package store

import (
	"context"
	"fmt"
	"github.com/colliedesk/colliedesk/server/internal/attachments"

	"github.com/jackc/pgx/v5"
)

// linkFiles records stored files as attachments of the message and points the email metadata at the
// stored original.
func linkFiles(ctx context.Context, tx pgx.Tx, storage attachments.Storage, workspaceID, messageID string, files []EmailFile) error {
	keys := make([]string, 0, len(files))
	for _, file := range files {
		keys = append(keys, file.Object.Key)
	}
	if err := VerifyObjects(ctx, tx, storage, keys...); err != nil {
		return err
	}
	for _, file := range files {
		item, err := InsertAttachment(ctx, tx, workspaceID, NewAttachment{MessageID: messageID, Filename: file.Filename,
			ContentType: file.ContentType, Object: file.Object, Inline: file.Inline})
		if err != nil {
			return err
		}
		if file.Original {
			if _, err := tx.Exec(ctx, `UPDATE messages SET email_meta=email_meta||jsonb_build_object('original_attachment_id',$3::text)
				WHERE workspace_id=$1 AND id=$2`, workspaceID, messageID, item.ID); err != nil {
				return fmt.Errorf("link original message: %w", err)
			}
		}
	}
	return nil
}
