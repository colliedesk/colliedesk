package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/kbviews"
	"github.com/jackc/pgx/v5"
)

// WriteKBArticleViews adds counts to kb_article_views in one statement for every workspace. A count
// whose article no longer exists is skipped; the others still add up.
func (s *Store) WriteKBArticleViews(ctx context.Context, counts []kbviews.Count) error {
	if len(counts) == 0 {
		return nil
	}
	workspaces, articles, locales := make([]string, len(counts)), make([]string, len(counts)), make([]string, len(counts))
	days, views := make([]time.Time, len(counts)), make([]int64, len(counts))
	for index, count := range counts {
		workspaces[index], articles[index], locales[index] = count.WorkspaceID, count.ArticleID, count.Locale
		days[index], views[index] = count.Day, count.Views
	}
	err := s.db.Privileged(ctx, database.PrivilegeKBArticleViews, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO kb_article_views(workspace_id, kb_article_id, locale, day, views)
			SELECT c.workspace_id, c.kb_article_id, c.locale, c.day, sum(c.views)
			FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::date[], $5::bigint[]) AS c(workspace_id, kb_article_id, locale, day, views)
			JOIN kb_articles a ON a.workspace_id=c.workspace_id AND a.id=c.kb_article_id
			GROUP BY c.workspace_id, c.kb_article_id, c.locale, c.day
			ON CONFLICT (workspace_id, kb_article_id, locale, day) DO UPDATE SET views = kb_article_views.views + EXCLUDED.views`,
			workspaces, articles, locales, days, views)
		return err
	})
	if err != nil {
		return fmt.Errorf("write article views: %w", err)
	}
	return nil
}
