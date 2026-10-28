package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// HelpCenterIndex returns every published article of the workspace outside the trash, without its
// text, in the help center's order (collection, then position and title): what sitemap.xml and
// llms.txt list. The article cap bounds it to domain.MaxKBArticles rows.
func (s *Store) HelpCenterIndex(ctx context.Context, workspaceID string) ([]domain.PublicKBArticle, error) {
	articles, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBArticle, error) {
		rows, err := tx.Query(ctx, `SELECT `+publicKBArticleColumns+` FROM kb_articles a WHERE `+publishedKBArticle+`
			ORDER BY a.collection_id NULLS LAST, a.position, lower(a.title), a.number`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) { return scanPublicKBArticle(row) })
	})
	if err != nil {
		return nil, fmt.Errorf("list help center index: %w", err)
	}
	return articles, nil
}
