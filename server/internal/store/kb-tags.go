package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// replaceKBArticleTags makes tags, already cleaned (domain.CleanKBTags), the published tags of the
// article. A tag it keeps keeps its row, so only the tags that changed queue AI retrieval.
func replaceKBArticleTags(ctx context.Context, tx pgx.Tx, workspaceID, id string, tags []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM kb_article_tags WHERE workspace_id=$1 AND article_id=$2 AND tag <> ALL($3::text[])`,
		workspaceID, id, tags); err != nil {
		return fmt.Errorf("remove knowledge base article tags: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_tags(workspace_id,article_id,tag) SELECT $1,$2,tag FROM unnest($3::text[]) tag
		ON CONFLICT DO NOTHING`, workspaceID, id, tags); err != nil {
		return fmt.Errorf("add knowledge base article tags: %w", err)
	}
	return nil
}

// draftTags is the list a draft save writes: the new tags, never NULL, so a draft that clears them
// is told apart from one that never changed them.
func draftTags(tags *[]string) []string {
	if tags == nil || *tags == nil {
		return []string{}
	}
	return *tags
}

// fillKBTags sets the published Tags of articles read by scanKBSummary, sorted, empty without any.
// One query for the whole page.
func fillKBTags(ctx context.Context, tx pgx.Tx, workspaceID string, items ...*domain.KBArticleSummary) error {
	ids := make([]string, 0, len(items))
	byID := make(map[string]*domain.KBArticleSummary, len(items))
	for _, item := range items {
		item.Tags = []string{}
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT article_id::text, tag FROM kb_article_tags WHERE workspace_id=$1 AND article_id=ANY($2::uuid[])
		ORDER BY tag`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read knowledge base article tags: %w", err)
	}
	var articleID, tag string
	if _, err := pgx.ForEachRow(rows, []any{&articleID, &tag}, func() error {
		if item := byID[articleID]; item != nil {
			item.Tags = append(item.Tags, tag)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("read knowledge base article tags: %w", err)
	}
	return nil
}

// fillKBDraftTags sets the tags the draft of article will publish: its own list, or the published
// tags when the draft never changed them.
func fillKBDraftTags(ctx context.Context, tx pgx.Tx, workspaceID string, article *domain.KBArticle) error {
	if article.Draft == nil {
		return nil
	}
	var tags []string
	if err := tx.QueryRow(ctx, `SELECT tags FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL`,
		workspaceID, article.ID).Scan(&tags); err != nil {
		return fmt.Errorf("read knowledge base article draft tags: %w", err)
	}
	if tags == nil {
		tags = article.Tags
	}
	article.Draft.Tags = tags
	return nil
}
