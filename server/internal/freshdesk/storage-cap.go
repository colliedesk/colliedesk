package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// sizingAttempts is how many times the sizing pass reads one ticket before it gives up. Each attempt
// already retries a 5xx or a network error with the client's backoff and honours rate limits; this
// adds a few more tries, with the same backoff, for a ticket that keeps failing.
const sizingAttempts = 3

// StorageCapError means a workspace cannot take this import; see importkit.StorageCapError.
type StorageCapError = importkit.StorageCapError

// cappedRoom returns the storage room of the workspace, or ok false when no cap that counts imported
// files applies to this run (self-hosted, trial).
func (r *run) cappedRoom(ctx context.Context) (store.StorageRoom, bool, error) {
	return importkit.CappedRoom(ctx, r.db, r.workspaceID, r.opts.Cloud, r.opts.PaidStorageBytes, r.opts.PlansOnSale)
}

// guardStorage runs before the first write. On a paid plan imported files count toward
// the storage cap, so it sizes every attachment the run would bring in and refuses the whole import
// when they do not fit: a partial import is a failure. This is the only cap check. An import that
// passes it is admitted and never stopped by the cap afterwards: customer mail that arrives while it
// runs, or a file added at the source after the sizing, may take usage a little past the cap, which is
// accepted.
//
// The size is not in the ticket list, so a read-only pass adds up the reported attachment bytes first:
// the dry-run listing and id probing, then one read of each ticket and of its conversations. It
// repeats the listing and probing of the real run and costs a second round of API calls, once, and
// only for a workspace on a paid plan, whose cap counts imports. Trial and self-hosted runs skip it.
func (r *run) guardStorage(ctx context.Context) error {
	if _, ok, err := r.cappedRoom(ctx); err != nil || !ok {
		return err
	}
	r.progress.phase("sizing attachments")
	needed, err := r.sizeIncomingAttachments(ctx)
	if err != nil {
		return err
	}
	// The sizing can take hours on a large account: judge against the room as it is now.
	room, ok, err := r.cappedRoom(ctx)
	if err != nil || !ok {
		return err
	}
	if room.Used+needed > room.Limit {
		return &StorageCapError{Plan: room.Plan, Needed: needed, Used: room.Used, Limit: room.Limit, Upgrade: room.Upgrade}
	}
	return nil
}

// checkDryRunStorage fills the report of a dry run on a capped workspace with what the real run
// would do, using the tickets the dry run already listed.
func (r *run) checkDryRunStorage(ctx context.Context, report *Report) error {
	room, ok, err := r.cappedRoom(ctx)
	if err != nil || !ok {
		return err
	}
	r.progress.phase("sizing attachments")
	needed, skipped, err := r.sizeRefs(ctx)
	if err != nil {
		return err
	}
	free := max(room.Limit-room.Used, 0)
	report.StorageCap = &StorageCapCheck{Plan: room.Plan, LimitBytes: room.Limit, UsedBytes: room.Used, NeededBytes: needed, FreeBytes: free, WouldRefuse: needed > free, SkippedTickets: skipped}
	return nil
}

// sizeIncomingAttachments sizes the tickets a real run would import, on a separate dry-run copy of
// the run so that nothing is written and the real run starts clean.
func (r *run) sizeIncomingAttachments(ctx context.Context) (int64, error) {
	options := r.opts
	options.DryRun, options.Progress = true, nil
	probe, err := newRun(ctx, r.db, options)
	if err != nil {
		return 0, err
	}
	probe.ledger.source = r.ledger.source
	if err := probe.listTickets(ctx); err != nil {
		return 0, err
	}
	if err := probe.classifyTickets(ctx); err != nil {
		return 0, err
	}
	needed, _, err := probe.sizeRefs(ctx)
	return needed, err
}

// sizeRefs adds up the bytes this run would store: every attachment of a listed ticket it would
// import that the ledger does not already hold as done, and every Freshdesk-hosted image in its
// message HTML (read once, since Freshdesk reports no size for them). It writes nothing. A ticket that
// cannot be read is skipped and listed, as the real import does: it fails that ticket and goes on.
func (r *run) sizeRefs(ctx context.Context) (needed int64, skipped []string, err error) {
	for _, ref := range r.refs {
		key := strconv.FormatInt(ref.id, 10)
		entry, found, err := r.ledger.get(ctx, entityTicket, key)
		if err != nil {
			return 0, nil, err
		}
		if found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil && !ref.updatedAt.After(*entry.SourceUpdatedAt) {
			continue
		}
		files, images, readable, err := r.ticketFilesWithRetry(ctx, ref, key)
		if err != nil {
			return 0, nil, err
		}
		if !readable {
			skipped = append(skipped, key)
			continue
		}
		for _, file := range files {
			held, err := r.attachmentDone(ctx, file)
			if err != nil {
				return 0, nil, err
			}
			if !held {
				needed += file.reportedSize()
			}
		}
		for _, image := range images {
			if entry, found, err := r.ledger.get(ctx, entityInlineImage, image.id); err != nil {
				return 0, nil, err
			} else if found && entry.Status == ledgerDone {
				continue
			}
			size, err := r.measureImage(ctx, image.ref)
			if err != nil {
				return 0, nil, err
			}
			needed += size
		}
	}
	return needed, skipped, nil
}

// sizedImage is a Freshdesk-hosted image of a message, with its ledger id.
type sizedImage struct {
	id  string
	ref imageRef
}

// ticketFilesWithRetry reads a ticket and its conversations and returns their attachments and
// inline images. A
// ticket or conversation list that is gone (404) or forbidden (403) cannot succeed on a retry: the
// ticket is reported unreadable, readable false, and the real import will fail it and go on. Any other
// failing read is tried sizingAttempts times; a ticket that still cannot be read stops the sizing with
// a reason, because an unknown size cannot be proven to fit.
func (r *run) ticketFilesWithRetry(ctx context.Context, ref ticketRef, key string) (files []fdAttachment, images []sizedImage, readable bool, err error) {
	var last error
	for attempt := 1; attempt <= sizingAttempts; attempt++ {
		if attempt > 1 {
			if err := r.client.wait(ctx, backoff(attempt-1)); err != nil {
				return nil, nil, false, err
			}
		}
		ticket, err := r.fetchTicket(ctx, ref)
		if errors.Is(err, errNotFound) {
			// Deleted at the source: the real import skips it as deleted, so it is neither sized nor unreadable.
			return nil, nil, true, nil
		}
		var conversations []fdConversation
		if err == nil {
			conversations, err = r.fetchConversations(ctx, ref)
		}
		switch {
		case err == nil:
			files := append([]fdAttachment{}, ticket.Attachments...)
			images := r.sizedImages(entityDescription, strconv.FormatInt(ticket.ID, 10), ticket.Description)
			for _, conversation := range conversations {
				files = append(files, conversation.Attachments...)
				images = append(images, r.sizedImages(entityMessage, strconv.FormatInt(conversation.ID, 10), conversation.Body)...)
			}
			return files, images, true, nil
		case errors.Is(err, errNotFound), errors.Is(err, errForbidden):
			return nil, nil, false, nil
		case ctx.Err() != nil:
			return nil, nil, false, ctx.Err()
		}
		last = err
	}
	return nil, nil, false, fmt.Errorf("could not size the import: ticket %s could not be read after %d attempts (%w). Nothing was imported; run the same command again", key, sizingAttempts, last)
}

func (r *run) attachmentDone(ctx context.Context, file fdAttachment) (bool, error) {
	entry, found, err := r.ledger.get(ctx, entityAttachment, strconv.FormatInt(file.ID, 10))
	return found && entry.Status == ledgerDone, err
}

func (r *run) sizedImages(entity, sourceID, markup string) []sizedImage {
	var images []sizedImage
	for _, ref := range r.inlineImages(markup) {
		images = append(images, sizedImage{id: inlineImageID(entity, sourceID, ref.key), ref: ref})
	}
	return images
}
