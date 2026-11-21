package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// SetKBTranslationAttachments sets the files a translation lists, in order. A published translation
// keeps the list in its draft until the draft is published, so the help center never changes here. The
// list is part of the text a review approves. seen, when set, is the token the client read.
func (s *Store) SetKBTranslationAttachments(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, ids []string, seen *time.Time) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		if seen == nil {
			var current time.Time
			err := tx.QueryRow(ctx, `SELECT `+kbTranslationToken+` FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3`,
				workspaceID, articleID, locale).Scan(&current)
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.KBArticleTranslation{}, kbTranslationOrArticleMissing(ctx, tx, workspaceID, articleID)
			}
			if err != nil {
				return domain.KBArticleTranslation{}, err
			}
			seen = &current
		}
		before, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, *seen)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := checkKBAttachments(ctx, tx, workspaceID, ids); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if before.Status == domain.KBPublished {
			return saveKBTranslationDraftFiles(ctx, tx, workspaceID, actor, articleID, locale, ids)
		}
		was, err := translationPublishState(ctx, tx, workspaceID, articleID, locale)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := replaceKBAttachments(ctx, tx, workspaceID, articleID, &locale, ids); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_translations SET updated_at=clock_timestamp() WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`,
			workspaceID, articleID, locale); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := addKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, domain.KBArticleEdited, nil); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := afterKBTranslationWrite(ctx, tx, workspaceID, actor, articleID, locale, was); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	})
	return item, kbError("set knowledge base translation attachments", err)
}

// saveKBTranslationDraftFiles makes ids the file list of a published translation's draft, made from the
// live translation when it has none, and records the draft as a draft save.
func saveKBTranslationDraftFiles(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID, locale string, ids []string) (domain.KBArticleTranslation, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,locale,title,slug,body_md,seo_title,seo_description,attachment_ids,
			updated_by_agent_id,created_at,updated_at)
		SELECT t.workspace_id,t.article_id,t.locale,t.title,t.slug,t.body_md,t.seo_title,t.seo_description,$4::uuid[],$5,clock_timestamp(),clock_timestamp()
		FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3
		ON CONFLICT (workspace_id,article_id,locale) WHERE locale IS NOT NULL DO UPDATE SET attachment_ids=EXCLUDED.attachment_ids,
			updated_by_agent_id=EXCLUDED.updated_by_agent_id, updated_at=EXCLUDED.updated_at`,
		workspaceID, articleID, locale, ids, optionalID(actor.AgentID)); err != nil {
		return domain.KBArticleTranslation{}, fmt.Errorf("save knowledge base translation draft attachments: %w", err)
	}
	return recordKBTranslationDraft(ctx, tx, workspaceID, actor, articleID, locale, true)
}
