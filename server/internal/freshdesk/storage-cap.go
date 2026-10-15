package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// StorageCapError means a Founding workspace cannot take this import: its attachments would pass the
// plan's storage cap. Nothing was written.
type StorageCapError struct {
	Needed, Used, Limit int64
}

func (e *StorageCapError) Error() string {
	return fmt.Sprintf("this import needs %d bytes of attachments, but only %d bytes are free under the %d byte attachment cap of the Founding plan. "+
		"Nothing was imported. Upgrade to the Team plan (Billing > Manage billing) to remove the cap, or import a smaller slice with --since.",
		e.Needed, max(e.Limit-e.Used, 0), e.Limit)
}

func (r *run) storeWithPlanLimits() *store.Store {
	return store.NewWithDB(r.db).WithPlanLimits(true).WithFoundingStorageBytes(r.opts.FoundingStorageBytes)
}

// guardFoundingStorage runs before the first write. On the Founding plan imported files count toward
// the 20 GiB cap, so it sizes every attachment the run would bring in and refuses the whole import
// when they do not fit: a partial import is a failure. The size is not known from the ticket list,
// so a read-only pass (the same listing and id probing as a dry run, then one read of each ticket and
// its conversations) adds up the reported attachment bytes first. It costs a second round of API
// calls, once, and only for a Founding workspace. Trial, Team and self-hosted runs skip it.
func (r *run) guardFoundingStorage(ctx context.Context) error {
	if !r.opts.Cloud {
		return nil
	}
	room, err := r.storeWithPlanLimits().GetStorageRoom(ctx, r.workspaceID)
	if err != nil {
		return err
	}
	if !room.Capped || !domain.StorageCountsImports(room.Plan) {
		return nil
	}
	r.founding = true
	r.progress.phase("sizing attachments")
	needed, err := r.sizeIncomingAttachments(ctx)
	if err != nil {
		return fmt.Errorf("size the import before writing anything: %w", err)
	}
	if room.Used+needed > room.Limit {
		return &StorageCapError{Needed: needed, Used: room.Used, Limit: room.Limit}
	}
	return nil
}

// sizeIncomingAttachments adds up the attachment bytes this run would download: every attachment of
// a ticket it would import that the ledger does not already hold as done. It writes nothing.
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
	var needed int64
	for _, ref := range probe.refs {
		key := strconv.FormatInt(ref.id, 10)
		entry, found, err := r.ledger.get(ctx, entityTicket, key)
		if err != nil {
			return 0, err
		}
		if found && entry.Status == ledgerDone && entry.SourceUpdatedAt != nil && !ref.updatedAt.After(*entry.SourceUpdatedAt) {
			continue
		}
		ticket, err := probe.fetchTicket(ctx, ref)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("read ticket %s: %w", key, err)
		}
		conversations, err := probe.fetchConversations(ctx, ref)
		if err != nil {
			return 0, fmt.Errorf("read conversations of ticket %s: %w", key, err)
		}
		files := append([]fdAttachment{}, ticket.Attachments...)
		for _, conversation := range conversations {
			files = append(files, conversation.Attachments...)
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

func (r *run) attachmentDone(ctx context.Context, file fdAttachment) (bool, error) {
	entry, found, err := r.ledger.get(ctx, entityAttachment, strconv.FormatInt(file.ID, 10))
	return found && entry.Status == ledgerDone, err
}

// checkFoundingRoom is the guard at the moment of writing: an attachment that the earlier sizing
// could not have seen (a file added at the source meanwhile) is refused when it would pass the cap.
func (r *run) checkFoundingRoom(ctx context.Context, size int64) error {
	if !r.founding {
		return nil
	}
	room, err := r.storeWithPlanLimits().GetStorageRoom(ctx, r.workspaceID)
	if err != nil {
		return err
	}
	if room.Capped && room.Used+size > room.Limit {
		return &StorageCapError{Needed: size, Used: room.Used, Limit: room.Limit}
	}
	return nil
}
