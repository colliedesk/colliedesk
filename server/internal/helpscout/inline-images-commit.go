package helpscout

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// imageBudget is what is left of the message's embedded-image budget after earlier runs.
func (r *run) imageBudget(ctx context.Context, messageID string) (int64, error) {
	var used int64
	err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT COALESCE(SUM(a.size),0) FROM attachments a JOIN import_items i
			ON i.workspace_id=a.workspace_id AND i.target_id=a.id AND i.entity='inline_image' AND i.status='done'
			WHERE a.workspace_id=$1 AND a.message_id=$2`, r.workspaceID, messageID).Scan(&used)
	})
	if err != nil {
		return 0, fmt.Errorf("read inline image budget: %w", err)
	}
	return max(maxImageBytes-used, 0), nil
}

// commitInlineImages records the attachment rows and ledger rows of a thread's images and points its
// stored HTML at them as data: URIs, in one transaction, so a resumed run never double-stores and
// never leaves HTML and ledger disagreeing.
func (r *run) commitInlineImages(ctx context.Context, conversationKey, messageID string, images []storedImage) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("begin inline image import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	keys := make([]string, len(images))
	for i, image := range images {
		keys[i] = image.object.Key
	}
	if err := store.VerifyObjects(ctx, tx, r.opts.Storage, keys...); err != nil {
		return err
	}
	embeds := map[string]string{}
	for _, image := range images {
		input := store.NewAttachment{MessageID: messageID, Filename: "inline-image-" + image.ref.key + importkit.ImageExtension(image.contentType), ContentType: image.contentType, Object: image.object, Inline: image.dataURI != ""}
		targetID, err := r.saveAttachment(ctx, tx, image.entry, image.found, input)
		if err != nil {
			return err
		}
		if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityInlineImage, sourceID: image.id, parentID: conversationKey, targetID: &targetID, status: ledgerDone}); err != nil {
			return err
		}
		if image.dataURI != "" {
			embeds[image.ref.key] = image.dataURI
		}
	}
	if err := r.embedImages(ctx, tx, messageID, embeds); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit inline image import: %w", err)
	}
	return nil
}

func (r *run) embedImages(ctx context.Context, tx pgx.Tx, messageID string, embeds map[string]string) error {
	var current *string
	if err := tx.QueryRow(ctx, `SELECT body_html FROM messages WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, r.workspaceID, messageID).Scan(&current); err != nil {
		return fmt.Errorf("read message HTML: %w", err)
	}
	if current == nil || len(embeds) == 0 {
		return nil
	}
	updated := mailimage.MapSources(*current, func(src string) (string, bool) {
		u, ok := r.hostedImageURL(src)
		if !ok {
			return "", false
		}
		uri, found := embeds[imageKey(u)]
		return uri, found
	})
	if updated == *current {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET body_html=$3 WHERE workspace_id=$1 AND id=$2`, r.workspaceID, messageID, updated); err != nil {
		return fmt.Errorf("write message HTML: %w", err)
	}
	return nil
}
