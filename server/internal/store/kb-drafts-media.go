package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// replaceInKBDraft rewrites from to to in the unpublished changes of a locked article and of each of
// its published translations, so publishing them later never brings the replaced item back. It
// returns the rewrites for the undo, one per draft that named the item; the article's own has no
// locale. The ids keep their length, so the text never grows past its limit.
func replaceInKBDraft(ctx context.Context, tx pgx.Tx, workspaceID, articleID, from, to string) ([]replacedTranslation, error) {
	rows, err := tx.Query(ctx, `SELECT COALESCE(locale, ''), body_md FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2
		ORDER BY locale NULLS FIRST FOR UPDATE`, workspaceID, articleID)
	if err != nil {
		return nil, err
	}
	drafts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (replacedTranslation, error) {
		item := replacedTranslation{ArticleID: articleID, Draft: true}
		return item, row.Scan(&item.Locale, &item.Before)
	})
	if err != nil {
		return nil, err
	}
	rewritten := []replacedTranslation{}
	for _, item := range drafts {
		item.After = domain.ReplaceKBMediaIDs(item.Before, from, to)
		if item.After == item.Before {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_drafts SET body_md=$4, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM NULLIF($3, '')`, workspaceID, articleID, item.Locale, item.After); err != nil {
			return nil, err
		}
		rewritten = append(rewritten, item)
	}
	if len(rewritten) > 0 {
		if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
			return nil, err
		}
	}
	return rewritten, nil
}

// undoKBDraftReplacement puts back the text the unpublished changes had before the replacement, only
// while they still hold the text it wrote, and reports whether it did.
func undoKBDraftReplacement(ctx context.Context, tx pgx.Tx, workspaceID string, item replacedTranslation) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE kb_article_drafts SET body_md=$4, updated_at=clock_timestamp()
		WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM NULLIF($3, '') AND body_md=$5`,
		workspaceID, item.ArticleID, item.Locale, item.Before, item.After)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
