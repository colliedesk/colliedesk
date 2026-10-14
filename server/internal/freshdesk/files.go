package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// importAttachment downloads a file right away (Freshdesk URLs are short-lived
// pre-signed links), streams it into storage and records its SHA-256.
func (r *run) importAttachment(ctx context.Context, ticketKey, messageID string, file fdAttachment, refresh func(context.Context, int64) (fdAttachment, error)) error {
	key := strconv.FormatInt(file.ID, 10)
	if _, ok, err := r.ledger.done(ctx, entityAttachment, key); err != nil || ok {
		return err
	}
	body, length, err := r.client.download(ctx, file.AttachmentURL)
	if errors.Is(err, errURLExpired) {
		fresh, refreshErr := refresh(ctx, file.ID)
		if refreshErr != nil {
			return fmt.Errorf("refresh expired attachment URL: %w", refreshErr)
		}
		body, length, err = r.client.download(ctx, fresh.AttachmentURL)
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	object, err := r.opts.Storage.Put(ctx, r.workspaceID, body)
	_ = body.Close()
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if length >= 0 && object.Size != length {
		return fmt.Errorf("download truncated: got %d of %d bytes", object.Size, length)
	}
	if object.Size != file.reportedSize() {
		return fmt.Errorf("size mismatch: Freshdesk reported %d bytes, downloaded %d", file.reportedSize(), object.Size)
	}
	name := strings.TrimSpace(file.Name)
	if name == "" {
		name = "attachment-" + key
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin attachment import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	item, err := store.InsertAttachment(ctx, tx, r.workspaceID, store.NewAttachment{MessageID: messageID, Filename: name, ContentType: file.ContentType, Object: object})
	if err != nil {
		return err
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityAttachment, sourceID: key, parentID: ticketKey, targetID: &item.ID, status: ledgerDone}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit attachment import: %w", err)
	}
	return nil
}
