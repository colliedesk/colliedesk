package helpscout

import (
	"bytes"
	"context"
	"encoding/base64"
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

// importThread writes one timeline message, its attachments and its hosted images, and returns the
// number of items that failed (already recorded in the ledger).
func (r *run) importThread(ctx context.Context, ref hsConversation, conversationID string, thread hsThread, files []hsAttachment) int {
	conversationKey, threadKey := strconv.FormatInt(ref.ID, 10), strconv.FormatInt(thread.ID, 10)
	message := r.threadMessage(thread)
	messageID, ok, err := r.ledger.done(ctx, entityThread, threadKey)
	if err == nil && !ok {
		messageID, err = r.insertMessage(ctx, conversationKey, conversationID, threadKey, message)
	}
	if err != nil {
		r.itemFailed(ctx, entityThread, threadKey, conversationKey, err)
		return 1 + len(files)
	}
	failed := 0
	for _, file := range files {
		if err := r.importAttachment(ctx, ref.ID, messageID, file); err != nil {
			if errors.Is(err, errDiskFull) {
				// Every later download would fail too: stop the run.
				if r.internalErr == nil {
					r.internalErr = err
				}
				return failed + 1
			}
			r.itemFailed(ctx, entityAttachment, strconv.FormatInt(file.ID, 10), conversationKey, err)
			failed++
		}
	}
	return failed + r.importInlineImages(ctx, conversationKey, threadKey, messageID, thread.Body)
}

func (r *run) insertMessage(ctx context.Context, conversationKey, conversationID, threadKey string, m newMessage) (string, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin message import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,author_name,kind,body,body_html,origin,created_at)
		SELECT $1,c.id,$3,$4,$5,$6,$7,$8,$9 FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
		r.workspaceID, conversationID, m.authorAgentID, m.authorName, m.kind, m.body, m.html, m.origin, m.createdAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("write message: %w", err)
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityThread, sourceID: threadKey, parentID: conversationKey, targetID: &id, status: ledgerDone}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit message import: %w", err)
	}
	return id, nil
}

// importAttachment reads a file through the API (base64 in JSON), checks its length against the
// size Help Scout reported, stores it and records its SHA-256. An attachment marked failed after a
// lost or changed stored file is read again into the same attachment row.
func (r *run) importAttachment(ctx context.Context, conversationID int64, messageID string, file hsAttachment) error {
	key := strconv.FormatInt(file.ID, 10)
	entry, found, err := r.ledger.get(ctx, entityAttachment, key)
	if err != nil || (found && entry.Status == ledgerDone) {
		return err
	}
	if err := r.checkDiskSpace(file.Size); err != nil {
		return err
	}
	var payload struct {
		Data string `json:"data"`
	}
	path := fmt.Sprintf("/v2/conversations/%d/attachments/%d/data", conversationID, file.ID)
	if err := r.client.getJSON(ctx, path, nil, &payload); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return fmt.Errorf("download: the data is not base64: %w", err)
	}
	if file.Size > 0 && int64(len(data)) != file.Size {
		return fmt.Errorf("size mismatch: Help Scout reported %d bytes, the data holds %d", file.Size, len(data))
	}
	object, err := r.opts.Storage.Put(ctx, r.workspaceID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	name := strings.TrimSpace(file.Filename)
	if name == "" {
		name = "attachment-" + key
	}
	input := store.NewAttachment{MessageID: messageID, Filename: name, ContentType: file.MimeType, Object: object}
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
	conversationKey := strconv.FormatInt(conversationID, 10)
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityAttachment, sourceID: key, parentID: conversationKey, targetID: &targetID, status: ledgerDone}); err != nil {
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

// saveAttachment reuses the row of a ledger entry whose stored file was lost or changed, and inserts
// a new one otherwise.
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
