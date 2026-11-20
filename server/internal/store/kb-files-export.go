package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportKBFiles writes the library's files, with their bytes, and then the lists articles and
// translations keep of them, in order.
func exportKBFiles(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,content_type,size,sha256,storage_key,name,kept,created_by_agent_id::text,created_at,
			updated_at,updated_by_agent_id::text,deleted_at,deleted_by_agent_id::text
		FROM kb_media WHERE workspace_id=$1 AND kind='file' ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base files: %w", err)
	}
	for rows.Next() {
		var record export.KBFile
		if err := rows.Scan(&record.ID, &record.ContentType, &record.Size, &record.SHA256, &record.StorageKey, &record.Name, &record.Kept, &record.CreatedByAgentID,
			&record.CreatedAt, &record.UpdatedAt, &record.UpdatedByAgentID, &record.DeletedAt, &record.DeletedByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base files: %w", err)
		}
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.KBFile(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base files", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT article_id::text,locale,media_id::text,position,created_at
		FROM kb_article_attachments WHERE workspace_id=$1 ORDER BY article_id,locale NULLS FIRST,position`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base attachment lists: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.KBArticleAttachment
		if err := rows.Scan(&record.ArticleID, &record.Locale, &record.MediaID, &record.Position, &record.CreatedAt); err != nil {
			return fmt.Errorf("export knowledge base attachment lists: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.KBArticleAttachment(record); err != nil {
			return err
		}
	}
	return rowsError("export knowledge base attachment lists", rows)
}
