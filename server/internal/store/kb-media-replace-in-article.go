package store

import (
	"context"
	"errors"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// lockKBArticleForReplace locks one article with the media its texts name plus extra before its text
// is read, so a save that drops the item while the replacement waits is seen. It returns the locked
// media; an article that is not live in the workspace is not found.
func lockKBArticleForReplace(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, extra ...string) ([]string, error) {
	found, err := lockKBArticlesForMedia(ctx, tx, workspaceID, []string{articleID}, extra...)
	if err != nil {
		return nil, err
	}
	var live bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL)`, workspaceID, articleID).Scan(&live); err != nil {
		return nil, err
	}
	if !live {
		return nil, domain.ErrKBArticleNotFound
	}
	return found, nil
}

// replaceInOneKBArticle rewrites from to to in one locked article. A published article keeps its text
// and that of its published translations: the copy goes into their unpublished changes, made from the
// published text when there are none, so the help center shows it only once someone publishes them.
// An article that is not published is rewritten as replaceInKBArticle does. It returns the
// media_replace version (0 when the article's own text did not change) and every rewrite for the undo.
func replaceInOneKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, actor Actor, from, to string) (int, []replacedTranslation, error) {
	var status, body string
	var drafted bool
	err := tx.QueryRow(ctx, `SELECT a.status, a.body_md, `+kbHasDraft+` FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, articleID).Scan(&status, &body, &drafted)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, domain.ErrKBArticleNotFound
	}
	if err != nil {
		return 0, nil, err
	}
	if status != domain.KBPublished {
		version, rewritten, err := replaceInKBArticle(ctx, tx, workspaceID, articleID, actor, from, to)
		if err != nil {
			return 0, nil, err
		}
		drafts, err := replaceInKBDraft(ctx, tx, workspaceID, articleID, from, to)
		return version, append(rewritten, drafts...), err
	}
	// Unpublished changes already there are rewritten first, so one made below from the published text
	// is not counted twice.
	rewritten, err := replaceInKBDraft(ctx, tx, workspaceID, articleID, from, to)
	if err != nil {
		return 0, nil, err
	}
	if after := domain.ReplaceKBMediaIDs(body, from, to); after != body && !drafted {
		if _, err := saveKBArticleDraft(ctx, tx, workspaceID, actor, articleID, KBArticleChange{BodyMD: &after}); err != nil {
			return 0, nil, err
		}
		rewritten = append(rewritten, replacedTranslation{ArticleID: articleID, Draft: true, Before: body, After: after})
	}
	translations, err := replaceInKBArticleTranslations(ctx, tx, workspaceID, articleID, actor, from, to)
	return 0, append(rewritten, translations...), err
}

// replaceInKBArticleTranslations rewrites from to to in the translations of a published article: a
// published one without unpublished changes gets them, made from its text; one with them was rewritten
// by replaceInKBDraft; one the help center does not show (a draft, or in the trash) is rewritten in
// place as a new version.
func replaceInKBArticleTranslations(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, actor Actor, from, to string) ([]replacedTranslation, error) {
	type translation struct {
		item           replacedTranslation
		shown, drafted bool
	}
	rows, err := tx.Query(ctx, `SELECT t.locale, t.body_md, t.status='published' AND t.deleted_at IS NULL, `+kbTranslationHasDraft+`
		FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.article_id=$2 ORDER BY t.locale FOR UPDATE`, workspaceID, articleID)
	if err != nil {
		return nil, err
	}
	translations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (translation, error) {
		t := translation{item: replacedTranslation{ArticleID: articleID}}
		return t, row.Scan(&t.item.Locale, &t.item.Before, &t.shown, &t.drafted)
	})
	if err != nil {
		return nil, err
	}
	rewritten, live := []replacedTranslation{}, false
	for _, t := range translations {
		item := t.item
		if item.After = domain.ReplaceKBMediaIDs(item.Before, from, to); item.After == item.Before || (t.shown && t.drafted) {
			continue
		}
		if t.shown {
			if _, err := saveKBTranslationDraft(ctx, tx, workspaceID, actor, articleID, item.Locale, KBTranslationChange{BodyMD: &item.After}); err != nil {
				return nil, err
			}
			item.Draft = true
			rewritten = append(rewritten, item)
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_translations SET body_md=$4, updated_at=clock_timestamp() WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`,
			workspaceID, articleID, item.Locale, item.After); err != nil {
			return nil, err
		}
		if err := recordKBTranslationVersion(ctx, tx, workspaceID, articleID, item.Locale, actor, domain.KBArticleMediaReplaced, nil); err != nil {
			return nil, err
		}
		rewritten, live = append(rewritten, item), true
	}
	if live {
		if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
			return nil, err
		}
	}
	return rewritten, nil
}
