package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// A published translation keeps its unpublished changes in a draft row (kb_article_drafts with its
// locale), as a published article does: the help center reads only kb_article_translations, so it
// shows the published text until someone publishes the draft.

// kbTranslationToken is the concurrency token of translation t that every read shows and every write
// checks: the later of its own updated_at and its draft's.
const kbTranslationToken = `GREATEST(t.updated_at,(SELECT td.updated_at FROM kb_article_drafts td WHERE td.workspace_id=t.workspace_id AND td.article_id=t.article_id AND td.locale=t.locale))`

// kbTranslationHasDraft is true for a translation t with unpublished changes.
const kbTranslationHasDraft = `EXISTS(SELECT 1 FROM kb_article_drafts td WHERE td.workspace_id=t.workspace_id AND td.article_id=t.article_id AND td.locale=t.locale)`

// changesKBTranslationText tells whether change writes the text or the search fields.
func changesKBTranslationText(change KBTranslationChange) bool {
	return change.Title != nil || change.Slug != nil || change.BodyMD != nil || change.SEOTitle != nil || change.SEODescription != nil
}

// goesToKBTranslationDraft tells whether change is saved in the draft: a change of the text of a
// published translation that stays published.
func goesToKBTranslationDraft(before lockedTranslation, change KBTranslationChange) bool {
	return before.Status == domain.KBPublished && (change.Status == nil || *change.Status == domain.KBPublished) && changesKBTranslationText(change)
}

// saveKBTranslationDraft writes the text of change into the draft of a published translation, made
// from the live translation when it has none. The live row and the help center stay as they are.
func saveKBTranslationDraft(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID, locale string, change KBTranslationChange) (domain.KBArticleTranslation, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,locale,title,slug,body_md,seo_title,seo_description,updated_by_agent_id,created_at,updated_at)
		SELECT t.workspace_id,t.article_id,t.locale,COALESCE($4,d.title,t.title),COALESCE($5,d.slug,t.slug),COALESCE($6,d.body_md,t.body_md),
			COALESCE($7,d.seo_title,t.seo_title),COALESCE($8,d.seo_description,t.seo_description),$9,clock_timestamp(),clock_timestamp()
		FROM kb_article_translations t LEFT JOIN kb_article_drafts d ON d.workspace_id=t.workspace_id AND d.article_id=t.article_id AND d.locale=t.locale
		WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3
		ON CONFLICT (workspace_id,article_id,locale) WHERE locale IS NOT NULL DO UPDATE SET title=EXCLUDED.title, slug=EXCLUDED.slug, body_md=EXCLUDED.body_md,
			seo_title=EXCLUDED.seo_title, seo_description=EXCLUDED.seo_description, updated_by_agent_id=EXCLUDED.updated_by_agent_id, updated_at=EXCLUDED.updated_at`,
		workspaceID, articleID, locale, change.Title, change.Slug, change.BodyMD, change.SEOTitle, change.SEODescription, optionalID(actor.AgentID)); err != nil {
		return domain.KBArticleTranslation{}, fmt.Errorf("save knowledge base translation draft: %w", err)
	}
	return recordKBTranslationDraft(ctx, tx, workspaceID, actor, articleID, locale, !change.Autosave)
}

// restoreKBTranslationDraft writes the text of version into the draft of a published translation, so
// the help center keeps the published text until someone publishes it.
func restoreKBTranslationDraft(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID, locale string, version int) (domain.KBArticleTranslation, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,locale,title,slug,body_md,seo_title,seo_description,updated_by_agent_id,created_at,updated_at)
		SELECT v.workspace_id,v.article_id,v.locale,v.title,v.slug,v.body_md,v.seo_title,v.seo_description,$5,clock_timestamp(),clock_timestamp()
		FROM kb_article_translation_versions v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.locale=$3 AND v.version=$4
		ON CONFLICT (workspace_id,article_id,locale) WHERE locale IS NOT NULL DO UPDATE SET title=EXCLUDED.title, slug=EXCLUDED.slug, body_md=EXCLUDED.body_md,
			seo_title=EXCLUDED.seo_title, seo_description=EXCLUDED.seo_description, updated_by_agent_id=EXCLUDED.updated_by_agent_id, updated_at=EXCLUDED.updated_at`,
		workspaceID, articleID, locale, version, optionalID(actor.AgentID))
	if err != nil {
		return domain.KBArticleTranslation{}, fmt.Errorf("restore knowledge base translation version into its draft: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.KBArticleTranslation{}, domain.ErrKBTranslationVersionNotFound
	}
	return recordKBTranslationDraft(ctx, tx, workspaceID, actor, articleID, locale, true)
}

// recordKBTranslationDraft records the translation's draft as a version of kind draft, notes its
// writer, links the media it names, settles an open review of the translation and returns it. An
// autosave (versioned false) records no version: the next save does.
func recordKBTranslationDraft(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID, locale string, versioned bool) (domain.KBArticleTranslation, error) {
	if versioned {
		if _, err := insertKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, domain.KBArticleDrafted, nil, true); err != nil {
			return domain.KBArticleTranslation{}, err
		}
	}
	if err := noteKBTextWriter(ctx, tx, workspaceID, articleID, &locale, actor, domain.KBArticleDrafted, ""); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	if err := settleKBReviewAfterEdit(ctx, tx, workspaceID, actor, articleID, &locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
}

// foldKBTranslationDraft fills the fields change leaves out from the translation's draft, makes the
// draft's files the translation's, then deletes the draft, so writing change publishes it.
func foldKBTranslationDraft(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string, change KBTranslationChange) (KBTranslationChange, error) {
	var title, slug, body, seoTitle, seoDescription string
	var attachments []string
	err := tx.QueryRow(ctx, `DELETE FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale=$3
		RETURNING title,slug,body_md,seo_title,seo_description,attachment_ids::text[]`,
		workspaceID, articleID, locale).Scan(&title, &slug, &body, &seoTitle, &seoDescription, &attachments)
	if errors.Is(err, pgx.ErrNoRows) {
		return change, nil
	}
	if err != nil {
		return change, fmt.Errorf("read knowledge base translation draft: %w", err)
	}
	// A draft that never changed its files leaves the translation's as they are.
	if attachments != nil {
		if err := replaceKBAttachments(ctx, tx, workspaceID, articleID, &locale, attachments); err != nil {
			return change, err
		}
	}
	for _, field := range []struct {
		target **string
		value  string
	}{{&change.Title, title}, {&change.Slug, slug}, {&change.BodyMD, body}, {&change.SEOTitle, seoTitle}, {&change.SEODescription, seoDescription}} {
		if *field.target == nil {
			value := field.value
			*field.target = &value
		}
	}
	return change, nil
}

// readKBTranslationDraft fills item.Draft from the translation's draft; its files are the draft's own
// list, or the live one when the draft never changed it.
func readKBTranslationDraft(ctx context.Context, tx pgx.Tx, workspaceID string, item *domain.KBArticleTranslation) error {
	if !item.HasDraft {
		return nil
	}
	var draft domain.KBTranslationDraft
	var ids []string
	var agentID, agentName *string
	if err := tx.QueryRow(ctx, `SELECT d.title,d.slug,d.body_md,d.seo_title,d.seo_description,d.attachment_ids::text[],d.created_at,d.updated_at,
			d.updated_by_agent_id::text,COALESCE(NULLIF(p.display_name,''),p.email)
		FROM kb_article_drafts d LEFT JOIN agents p ON p.workspace_id=d.workspace_id AND p.id=d.updated_by_agent_id
		WHERE d.workspace_id=$1 AND d.article_id=$2 AND d.locale=$3`, workspaceID, item.ArticleID, item.Locale).Scan(&draft.Title, &draft.Slug, &draft.BodyMD,
		&draft.SEOTitle, &draft.SEODescription, &ids, &draft.CreatedAt, &draft.UpdatedAt, &agentID, &agentName); err != nil {
		return fmt.Errorf("read knowledge base translation draft: %w", err)
	}
	draft.CreatedAt, draft.UpdatedAt = domain.UTC(draft.CreatedAt), domain.UTC(draft.UpdatedAt)
	draft.UpdatedBy = savedReplyPerson(agentID, agentName)
	draft.Attachments = item.Attachments
	if ids != nil {
		var err error
		if draft.Attachments, err = readKBAttachmentsByID(ctx, tx, workspaceID, ids); err != nil {
			return err
		}
	}
	item.Draft = &draft
	return nil
}

// PublishKBTranslationDraft makes the draft of a published translation live as a version of kind
// edit; seen is the translation's concurrency token. domain.ErrKBDraftNotFound without a draft.
func (s *Store) PublishKBTranslationDraft(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, seen time.Time) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		before, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, seen)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if !before.HasDraft {
			return domain.KBArticleTranslation{}, domain.ErrKBDraftNotFound
		}
		was, err := translationPublishState(ctx, tx, workspaceID, articleID, locale)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		change, err := foldKBTranslationDraft(ctx, tx, workspaceID, articleID, locale, KBTranslationChange{})
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := writeKBTranslationRow(ctx, tx, workspaceID, articleID, locale, change); err != nil {
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
	return item, kbError("publish knowledge base translation draft", err)
}

// DiscardKBTranslationDraft deletes the draft of a translation; the live translation and its versions
// stay as they are. seen is the translation's concurrency token.
func (s *Store) DiscardKBTranslationDraft(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, seen time.Time) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		before, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, seen)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if !before.HasDraft {
			return domain.KBArticleTranslation{}, domain.ErrKBDraftNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`, workspaceID, articleID, locale); err != nil {
			return domain.KBArticleTranslation{}, fmt.Errorf("discard knowledge base translation draft: %w", err)
		}
		if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := settleKBReviewAfterEdit(ctx, tx, workspaceID, actor, articleID, &locale); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	})
	return item, kbError("discard knowledge base translation draft", err)
}
