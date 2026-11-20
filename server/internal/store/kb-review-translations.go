package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// A translation has no draft layer: its text waits to be published while its status is draft, and an
// edit of a published one goes live at once. So a translation in a collection that needs approval is
// reviewed as a draft, and published only with the text a review approved.

// translationContent is what a review of the translation approves: its text, listed where its article is.
func translationContent(item domain.KBArticleTranslation, collections []string) domain.KBContent {
	return domain.KBContent{Title: item.Title, Slug: item.Slug, BodyMD: item.BodyMD, SEOTitle: item.SEOTitle, SEODescription: item.SEODescription, CollectionIDs: collections}
}

// readKBTranslationText reads a translation with the collections of its article; domain.ErrKBTranslationNotFound when it has none.
func readKBTranslationText(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string) (domain.KBArticleTranslation, domain.KBContent, error) {
	item, err := readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	if err != nil {
		return item, domain.KBContent{}, err
	}
	article, err := readKBArticle(ctx, tx, workspaceID, articleID)
	if err != nil {
		return item, domain.KBContent{}, err
	}
	return item, translationContent(item, article.CollectionIDs), nil
}

// translationPublishState is what guardKBPublish compares for a translation; a missing one is not live.
func translationPublishState(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string) (kbPublishState, error) {
	item, content, err := readKBTranslationText(ctx, tx, workspaceID, articleID, locale)
	if errors.Is(err, domain.ErrKBTranslationNotFound) {
		return kbPublishState{}, nil
	}
	if err != nil {
		return kbPublishState{}, err
	}
	return kbPublishState{Live: item.Status == domain.KBPublished && item.DeletedAt == nil, Content: content}, nil
}

// afterKBTranslationWrite runs after every write of a translation, in its transaction: it refuses a
// published text no review approved (before is the translation as it was), then settles an open review
// of the translation whose text changed.
func afterKBTranslationWrite(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, articleID, locale string, before kbPublishState) error {
	after, err := translationPublishState(ctx, tx, workspaceID, articleID, locale)
	if err != nil {
		return err
	}
	if err := guardKBPublish(ctx, tx, workspaceID, articleID, &locale, before, after); err != nil {
		return err
	}
	return settleKBReviewAfterEdit(ctx, tx, workspaceID, actor, articleID, &locale)
}

// kbPendingText tells whether the article (locale nil) or its translation has text waiting to be
// published, and its hash: the article's draft over it, or a translation that is a draft. A text in
// the trash has none.
func kbPendingText(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string) (bool, string, error) {
	if locale == nil {
		article, err := readKBArticle(ctx, tx, workspaceID, articleID)
		if err != nil {
			return false, "", err
		}
		pending := article.DeletedAt == nil && (article.Status != domain.KBPublished || article.Draft != nil)
		return pending, article.AsDrafted().Content().Hash(), nil
	}
	item, content, err := readKBTranslationText(ctx, tx, workspaceID, articleID, *locale)
	if err != nil {
		return false, "", err
	}
	return item.DeletedAt == nil && item.Status != domain.KBPublished, content.Hash(), nil
}

// kbTranslationTextAuthors is kbTextAuthors for a translation's history.
func kbTranslationTextAuthors(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string, people []string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT v.changed_by_agent_id::text FROM kb_article_translation_versions v WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.locale=$3
		AND v.changed_by_agent_id=ANY($4::uuid[]) AND v.kind IN `+kbTextChangeKinds+` AND v.version > COALESCE((SELECT max(p.version) FROM kb_article_translation_versions p
			WHERE p.workspace_id=$1 AND p.article_id=$2 AND p.locale=$3 AND p.status='published' AND `+kbTextVersion("p.")+`),0)`,
		workspaceID, articleID, locale, people)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base translation authors: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// reviewedKBText is the text a review is about as it is now, and what the help center shows of it
// (nil while it is not published).
func reviewedKBText(ctx context.Context, tx pgx.Tx, workspaceID string, review domain.KBReview) (domain.KBContent, *domain.KBContent, error) {
	if review.Locale == nil {
		article, err := readKBArticle(ctx, tx, workspaceID, review.ArticleID)
		if err != nil {
			return domain.KBContent{}, nil, err
		}
		if article.Status != domain.KBPublished {
			return article.AsDrafted().Content(), nil, nil
		}
		live := article.Content()
		return article.AsDrafted().Content(), &live, nil
	}
	item, content, err := readKBTranslationText(ctx, tx, workspaceID, review.ArticleID, *review.Locale)
	if err != nil {
		return domain.KBContent{}, nil, err
	}
	if item.Status != domain.KBPublished || item.DeletedAt != nil {
		return content, nil, nil
	}
	return content, &content, nil
}
