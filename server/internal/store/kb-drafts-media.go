package store

import (
	"context"
	"errors"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// replaceInKBDraft rewrites from to to in the unpublished changes of a locked article, so publishing
// them later never brings the replaced item back. It returns the rewrite for the undo, or nil when the
// article has no unpublished changes or they do not name the item. The ids keep their length, so the
// text never grows past its limit.
func replaceInKBDraft(ctx context.Context, tx pgx.Tx, workspaceID, articleID, from, to string) (*replacedTranslation, error) {
	var before string
	err := tx.QueryRow(ctx, `SELECT body_md FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL FOR UPDATE`,
		workspaceID, articleID).Scan(&before)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	after := domain.ReplaceKBMediaIDs(before, from, to)
	if after == before {
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_article_drafts SET body_md=$3, updated_at=clock_timestamp() WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL`,
		workspaceID, articleID, after); err != nil {
		return nil, err
	}
	if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
		return nil, err
	}
	return &replacedTranslation{ArticleID: articleID, Draft: true, Before: before, After: after}, nil
}

// undoKBDraftReplacement puts back the text the unpublished changes had before the replacement, only
// while they still hold the text it wrote, and reports whether it did.
func undoKBDraftReplacement(ctx context.Context, tx pgx.Tx, workspaceID string, item replacedTranslation) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE kb_article_drafts SET body_md=$3, updated_at=clock_timestamp()
		WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL AND body_md=$4`, workspaceID, item.ArticleID, item.Before, item.After)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
