package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportKBArticleViewsAndUses writes the views of each article per language and local day, then each
// sent reply that used an article, so the help center numbers on Reports can be worked out again from
// the archive.
func exportKBArticleViewsAndUses(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT kb_article_id::text, locale, to_char(day, 'YYYY-MM-DD'), views
		FROM kb_article_views WHERE workspace_id=$1 ORDER BY day, kb_article_id, locale`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base article views: %w", err)
	}
	for rows.Next() {
		var record export.KBArticleView
		if err := rows.Scan(&record.ArticleID, &record.Locale, &record.Day, &record.Views); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base article views: %w", err)
		}
		if err := sink.KBArticleView(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base article views", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT kb_article_id::text, conversation_id::text, message_id::text, kind, created_at
		FROM kb_article_uses WHERE workspace_id=$1 ORDER BY created_at, message_id, kb_article_id, kind`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base article uses: %w", err)
	}
	for rows.Next() {
		var record export.KBArticleUse
		if err := rows.Scan(&record.ArticleID, &record.ConversationID, &record.MessageID, &record.Kind, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base article uses: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.KBArticleUse(record); err != nil {
			rows.Close()
			return err
		}
	}
	return rowsError("export knowledge base article uses", rows)
}
