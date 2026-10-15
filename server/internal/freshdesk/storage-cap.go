package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// sizingAttempts is how many times the sizing pass reads one ticket before it gives up. Each attempt
// already retries a 5xx or a network error with the client's backoff and honours rate limits; this
// adds a few more tries, with the same backoff, for a ticket that keeps failing.
const sizingAttempts = 3

// StorageCapError means a Founding workspace cannot take this import: its attachments would pass the
// plan's storage cap. Nothing was written.
type StorageCapError struct {
	Needed, Used, Limit int64
}

func (e *StorageCapError) Error() string {
	return fmt.Sprintf("this import needs %s of attachments, but only %s is free under the %s attachment cap of the Founding plan. "+
		"Nothing was imported. Upgrade to the Team plan (Billing > Manage billing) to remove the cap, or import a smaller slice with --since.",
		domain.FormatBytes(e.Needed), domain.FormatBytes(max(e.Limit-e.Used, 0)), domain.FormatBytes(e.Limit))
}

func (r *run) storeWithPlanLimits() *store.Store {
	return store.NewWithDB(r.db).WithPlanLimits(true).WithFoundingStorageBytes(r.opts.FoundingStorageBytes)
}

// foundingRoom returns the Founding room of the workspace, or ok false when the storage cap does not
// apply to this run (self-hosted, trial, Team).
func (r *run) foundingRoom(ctx context.Context) (room store.StorageRoom, ok bool, err error) {
	if !r.opts.Cloud {
		return store.StorageRoom{}, false, nil
	}
	room, err = r.storeWithPlanLimits().GetStorageRoom(ctx, r.workspaceID)
	if err != nil {
		return store.StorageRoom{}, false, err
	}
	return room, room.Capped && domain.StorageCountsImports(room.Plan), nil
}

// guardFoundingStorage runs before the first write. On the Founding plan imported files count toward
// the 20 GiB cap, so it sizes every attachment the run would bring in and refuses the whole import
// when they do not fit: a partial import is a failure. This is the only cap check. An import that
// passes it is admitted and never stopped by the cap afterwards: customer mail that arrives while it
// runs, or a file added at the source after the sizing, may take usage a little past the cap, which is
// accepted.
//
// The size is not in the ticket list, so a read-only pass adds up the reported attachment bytes first:
// the dry-run listing and id probing, then one read of each ticket and of its conversations. It
// repeats the listing and probing of the real run and costs a second round of API calls, once, and
// only for a Founding workspace. Trial, Team and self-hosted runs skip it.
func (r *run) guardFoundingStorage(ctx context.Context) error {
	if _, ok, err := r.foundingRoom(ctx); err != nil || !ok {
		return err
	}
	r.progress.phase("sizing attachments")
	needed, err := r.sizeIncomingAttachments(ctx)
	if err != nil {
		return err
	}
	// The sizing can take hours on a large account: judge against the room as it is now.
	room, ok, err := r.foundingRoom(ctx)
	if err != nil || !ok {
		return err
	}
	if room.Used+needed > room.Limit {
		return &StorageCapError{Needed: needed, Used: room.Used, Limit: room.Limit}
	}
	return nil
}

// checkDryRunStorage fills the report of a dry run on a Founding workspace with what the real run
// would do, using the tickets the dry run already listed.
func (r *run) checkDryRunStorage(ctx context.Context, report *Report) error {
	room, ok, err := r.foundingRoom(ctx)
	if err != nil || !ok {
		return err
	}
	r.progress.phase("sizing attachments")
	needed, err := r.sizeRefs(ctx)
	if err != nil {
		return err
	}
	free := max(room.Limit-room.Used, 0)
	report.StorageCap = &StorageCapCheck{LimitBytes: room.Limit, UsedBytes: room.Used, NeededBytes: needed, FreeBytes: free, WouldRefuse: needed > free}
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
	return probe.sizeRefs(ctx)
}

// sizeRefs adds up the attachment bytes this run would download: every attachment of a listed ticket
// it would import that the ledger does not already hold as done. It writes nothing.
func (r *run) sizeRefs(ctx context.Context) (int64, error) {
	var needed int64
	for _, ref := range r.refs {
		key := strconv.FormatInt(ref.id, 10)
		entry, found, err := r.ledger.get(ctx, entityTicket, key)
		if err != nil {
			return 0, err
		}
		if found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil && !ref.updatedAt.After(*entry.SourceUpdatedAt) {
			continue
		}
		files, err := r.ticketFilesWithRetry(ctx, ref, key)
		if err != nil {
			return 0, err
		}
		for _, file := range files {
			held, err := r.attachmentDone(ctx, file)
			if err != nil {
				return 0, err
			}
			if !held {
				needed += file.reportedSize()
			}
		}
	}
	return needed, nil
}

// ticketFilesWithRetry reads a ticket and its conversations and returns their attachments. A
// ticket deleted at the source has none. A failing read is tried sizingAttempts times; a ticket that
// still cannot be read stops the sizing with a reason, because an unknown size cannot be proven to fit.
func (r *run) ticketFilesWithRetry(ctx context.Context, ref ticketRef, key string) ([]fdAttachment, error) {
	var last error
	for attempt := 1; attempt <= sizingAttempts; attempt++ {
		if attempt > 1 {
			if err := r.client.wait(ctx, backoff(attempt-1)); err != nil {
				return nil, err
			}
		}
		ticket, err := r.fetchTicket(ctx, ref)
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		if err == nil {
			var conversations []fdConversation
			if conversations, err = r.fetchConversations(ctx, ref); err == nil {
				files := append([]fdAttachment{}, ticket.Attachments...)
				for _, conversation := range conversations {
					files = append(files, conversation.Attachments...)
				}
				return files, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		last = err
	}
	return nil, fmt.Errorf("could not size the import: ticket %s could not be read after %d attempts (%w). Nothing was imported; run the same command again", key, sizingAttempts, last)
}

func (r *run) attachmentDone(ctx context.Context, file fdAttachment) (bool, error) {
	entry, found, err := r.ledger.get(ctx, entityAttachment, strconv.FormatInt(file.ID, 10))
	return found && entry.Status == ledgerDone, err
}
