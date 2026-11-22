package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportKBSearches writes the help center searches still kept, oldest first.
func exportKBSearches(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT query, results, opened_article_id::text, created_at
		FROM kb_searches WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export help center searches: %w", err)
	}
	for rows.Next() {
		var record export.KBSearch
		if err := rows.Scan(&record.Query, &record.Results, &record.OpenedArticleID, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export help center searches: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.KBSearch(record); err != nil {
			rows.Close()
			return err
		}
	}
	return rowsError("export help center searches", rows)
}
