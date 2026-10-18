package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// diskReserve is kept free on the attachments volume during an import.
const diskReserve = 256 << 20

var errDiskFull = errors.New("not enough free disk space for attachments")

// importAttachment downloads a file right away (Freshdesk URLs are short-lived
// pre-signed links), streams it into storage with a size limit and records its
// SHA-256. An attachment marked failed after a lost or changed stored file is
// downloaded again into the same attachment row.
func (r *run) importAttachment(ctx context.Context, ticketKey, messageID string, file fdAttachment, refresh func(context.Context, int64) (fdAttachment, error)) error {
	key := strconv.FormatInt(file.ID, 10)
	entry, found, err := r.ledger.get(ctx, entityAttachment, key)
	if err != nil || (found && entry.Status == ledgerDone) {
		return err
	}
	if err := r.checkDiskSpace(file.reportedSize()); err != nil {
		return err
	}
	body, length, _, err := r.client.download(ctx, file.AttachmentURL)
	if errors.Is(err, errURLExpired) {
		fresh, refreshErr := refresh(ctx, file.ID)
		if refreshErr != nil {
			return fmt.Errorf("refresh expired attachment URL: %w", refreshErr)
		}
		body, length, _, err = r.client.download(ctx, fresh.AttachmentURL)
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if length >= 0 && length != file.reportedSize() {
		_ = body.Close()
		return fmt.Errorf("size mismatch: Freshdesk reported %d bytes, the download announces %d", file.reportedSize(), length)
	}
	object, err := r.opts.Storage.Put(ctx, r.workspaceID, body, file.reportedSize())
	_ = body.Close()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	name := strings.TrimSpace(file.Name)
	if name == "" {
		name = "attachment-" + key
	}
	input := store.NewAttachment{MessageID: messageID, Filename: name, ContentType: file.ContentType, Object: object}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin attachment import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := store.VerifyObjects(ctx, tx, r.opts.Storage, object.Key); err != nil {
		return err
	}
	targetID, err := r.saveAttachment(ctx, tx, entry, found, input)
	if err != nil {
		return err
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityAttachment, sourceID: key, parentID: ticketKey, targetID: &targetID, status: ledgerDone}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attachment import: %w", err)
	}
	return nil
}

func (r *run) checkDiskSpace(size int64) error {
	reporter, ok := r.opts.Storage.(attachments.SpaceReporter)
	if !ok {
		return nil
	}
	free, err := reporter.Available()
	if err != nil {
		return nil //nolint:nilerr // unknown free space must not block an import; Put still fails on a full disk
	}
	if free < size+diskReserve {
		return fmt.Errorf("%w: %d bytes free, %d needed plus a %d byte reserve", errDiskFull, free, size, diskReserve)
	}
	return nil
}

// saveAttachment writes the attachment row of a stored file: it reuses the row of a ledger entry
// whose stored file was lost or changed, and inserts a new one otherwise.
func (r *run) saveAttachment(ctx context.Context, tx pgx.Tx, entry ledgerEntry, found bool, input store.NewAttachment) (string, error) {
	if found && entry.TargetID != nil {
		replaced, err := store.ReplaceAttachmentBytes(ctx, tx, r.workspaceID, *entry.TargetID, input)
		if err != nil {
			return "", err
		}
		if replaced {
			return *entry.TargetID, nil
		}
	}
	item, err := store.InsertAttachment(ctx, tx, r.workspaceID, input)
	if err != nil {
		return "", err
	}
	return item.ID, nil
}
