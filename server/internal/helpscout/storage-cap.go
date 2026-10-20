package helpscout

import (
	"context"
	"fmt"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/importkit"
)

// guardStorage runs before the first write. On a paid Colliedesk Cloud plan imported files count
// toward the storage cap, so it sizes every attachment and hosted image the run would bring in and
// refuses the whole import when they do not fit: a partial import is a failure. Trial and
// self-hosted runs skip it. An import that passes is never stopped by the cap afterwards.
func (r *run) guardStorage(ctx context.Context) error {
	if _, ok, err := importkit.CappedRoom(ctx, r.db, r.workspaceID, r.opts.Cloud, r.opts.PaidStorageBytes); err != nil || !ok {
		return err
	}
	needed, err := r.sizeIncoming(ctx)
	if err != nil {
		return err
	}
	// The sizing can take long on a large inbox: judge against the room as it is now.
	room, ok, err := importkit.CappedRoom(ctx, r.db, r.workspaceID, r.opts.Cloud, r.opts.PaidStorageBytes)
	if err != nil || !ok {
		return err
	}
	if room.Used+needed > room.Limit {
		return &importkit.StorageCapError{Plan: room.Plan, Needed: needed, Used: room.Used, Limit: room.Limit}
	}
	return nil
}

// checkDryRunStorage tells a dry run on a capped workspace whether the real run would be refused.
func (r *run) checkDryRunStorage(ctx context.Context, report *Report) error {
	room, ok, err := importkit.CappedRoom(ctx, r.db, r.workspaceID, r.opts.Cloud, r.opts.PaidStorageBytes)
	if err != nil || !ok {
		return err
	}
	needed, err := r.sizeIncoming(ctx)
	if err != nil {
		return err
	}
	free := max(room.Limit-room.Used, 0)
	report.StorageCap = &StorageCapCheck{Plan: room.Plan, LimitBytes: room.Limit, UsedBytes: room.Used, NeededBytes: needed, FreeBytes: free, WouldRefuse: needed > free}
	return nil
}

// sizeIncoming adds up the bytes the run would store: the reported size of every attachment the
// ledger does not hold as done, and every hosted image not yet stored (read once, since Help Scout
// reports no size for them). It writes nothing. Threads are read again by the import itself.
func (r *run) sizeIncoming(ctx context.Context) (int64, error) {
	r.progress.phase("sizing attachments")
	var needed int64
	for _, ref := range r.refs {
		entry, found, err := r.ledger.get(ctx, entityTicket, strconv.FormatInt(ref.ID, 10))
		if err != nil {
			return 0, err
		}
		if unchanged(entry, found, ref) {
			continue
		}
		threads, err := r.fetchThreads(ctx, ref.ID)
		if err != nil {
			return 0, fmt.Errorf("could not size the import: conversation %d could not be read (%w). Nothing was imported; run the same command again", ref.ID, err)
		}
		for _, thread := range threads {
			if thread.Type == "lineitem" || thread.State == "draft" {
				continue
			}
			for _, file := range thread.Embedded.Attachments {
				if held, err := r.isDone(ctx, entityAttachment, strconv.FormatInt(file.ID, 10)); err != nil {
					return 0, err
				} else if !held && file.State != "virus" {
					needed += file.Size
				}
			}
			for _, image := range r.inlineImages(thread.Body) {
				if held, err := r.isDone(ctx, entityInlineImage, inlineImageID(strconv.FormatInt(thread.ID, 10), image.key)); err != nil {
					return 0, err
				} else if !held {
					// An unreadable image counts as zero: the import then fails it and lists it.
					if data, _, err := r.fetchInlineImage(ctx, image.url); err == nil {
						needed += int64(len(data))
					} else if ctx.Err() != nil {
						return 0, ctx.Err()
					}
				}
			}
		}
	}
	return needed, nil
}

func (r *run) isDone(ctx context.Context, entity, id string) (bool, error) {
	entry, found, err := r.ledger.get(ctx, entity, id)
	return found && entry.Status == ledgerDone, err
}
