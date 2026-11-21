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

// ListKBTags returns every published tag of the workspace's articles outside the trash, by name, with
// how many of them have it.
func (s *Store) ListKBTags(ctx context.Context, workspaceID string) ([]domain.KBTagCount, error) {
	tags, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBTagCount, error) {
		rows, err := tx.Query(ctx, `SELECT t.tag, count(*) FROM kb_article_tags t
			JOIN kb_articles a ON a.workspace_id=t.workspace_id AND a.id=t.article_id AND a.deleted_at IS NULL
			WHERE t.workspace_id=$1 GROUP BY t.tag ORDER BY t.tag`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowToStructByPos[domain.KBTagCount])
	})
	if err != nil {
		return nil, fmt.Errorf("list knowledge base tags: %w", err)
	}
	return tags, nil
}

// kbTagWords is the search vector of the published tags of the article aliased a, empty without any:
// the help center and help chat find an article by a tag as by a word of its title.
const kbTagWords = `COALESCE((SELECT kb_search_vector(string_agg(kt.tag, ' ')) FROM kb_article_tags kt
	WHERE kt.workspace_id=a.workspace_id AND kt.article_id=a.id), ''::tsvector)`

// shownKBTags returns the published tags of the article, sorted, when the help center shows tags, and
// none when it does not.
func shownKBTags(ctx context.Context, tx pgx.Tx, workspaceID, id string) ([]string, error) {
	var tags []string
	err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(t.tag ORDER BY t.tag), '{}') FROM kb_article_tags t
		JOIN help_centers h ON h.workspace_id=t.workspace_id AND h.show_tags
		WHERE t.workspace_id=$1 AND t.article_id=$2`, workspaceID, id).Scan(&tags)
	return tags, err
}
