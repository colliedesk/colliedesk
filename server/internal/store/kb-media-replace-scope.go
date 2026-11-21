package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbArticlesShowingMedia lists the live articles whose current text, a translation or unpublished
// changes show the media, in id order; with article only that one, and none when it does not show it.
// An article that is not live in the workspace is not found.
func kbArticlesShowingMedia(ctx context.Context, tx pgx.Tx, workspaceID, media, article string) ([]string, error) {
	if article != "" {
		var live bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL)`, workspaceID, article).Scan(&live); err != nil {
			return nil, err
		}
		if !live {
			return nil, domain.ErrKBArticleNotFound
		}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT l.article_id::text FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND a.deleted_at IS NULL AND ($3 = '' OR a.id::text = $3) ORDER BY 1`, workspaceID, media, article)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
