package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
)

// exportKBArticleDrafts writes the unpublished changes of the workspace's articles and translations,
// by article with its own text first.
func exportKBArticleDrafts(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT article_id::text,locale,title,slug,body_md,seo_title,seo_description,collection_id::text,extra_collection_ids::text[],
			show_contents,attachment_ids::text[],tags,updated_by_agent_id::text,created_at,updated_at
		FROM kb_article_drafts WHERE workspace_id=$1 ORDER BY article_id,locale NULLS FIRST`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base drafts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.KBArticleDraft
		if err := rows.Scan(&record.ArticleID, &record.Locale, &record.Title, &record.Slug, &record.BodyMD, &record.SEOTitle, &record.SEODescription,
			&record.CollectionID, &record.ExtraCollectionIDs, &record.ShowContents, &record.AttachmentIDs, &record.Tags, &record.UpdatedByAgentID, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return fmt.Errorf("export knowledge base drafts: %w", err)
		}
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		if err := sink.KBArticleDraft(record); err != nil {
			return err
		}
	}
	return rowsError("export knowledge base drafts", rows)
}
