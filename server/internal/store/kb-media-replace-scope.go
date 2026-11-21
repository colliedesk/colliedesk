package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// kbArticlesShowingMedia lists the live articles whose current text, a translation or unpublished
// changes show the media, in id order.
func kbArticlesShowingMedia(ctx context.Context, tx pgx.Tx, workspaceID, media string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT l.article_id::text FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND a.deleted_at IS NULL ORDER BY 1`, workspaceID, media)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
