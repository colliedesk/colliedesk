package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbAttachmentSelect reads the files of the media library named by $2 (uuid[]) in that order.
const kbAttachmentSelect = `SELECT m.id::text,m.name,m.content_type,m.size FROM unnest($2::uuid[]) WITH ORDINALITY AS l(media_id,position)
	JOIN kb_media m ON m.workspace_id=$1 AND m.id=l.media_id AND m.kind='file' ORDER BY l.position`

// readKBAttachmentsByID reads the files ids name, in order; an id that names no file is left out.
func readKBAttachmentsByID(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (domain.KBAttachments, error) {
	rows, err := tx.Query(ctx, kbAttachmentSelect, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base attachments: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBAttachment, error) {
		var item domain.KBAttachment
		err := row.Scan(&item.ID, &item.Name, &item.ContentType, &item.Size)
		item.URL = domain.KBMediaPath + item.ID
		return item, err
	})
	return domain.KBAttachments(items), err
}

// listedKBAttachmentIDs is the live list of the article (locale nil) or of one translation.
func listedKBAttachmentIDs(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT media_id::text FROM kb_article_attachments
		WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM $3 ORDER BY position`, workspaceID, articleID, locale)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// readKBAttachments reads the live files of the article (locale nil) or of one translation.
func readKBAttachments(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string) (domain.KBAttachments, error) {
	ids, err := listedKBAttachmentIDs(ctx, tx, workspaceID, articleID, locale)
	if err != nil {
		return nil, err
	}
	return readKBAttachmentsByID(ctx, tx, workspaceID, ids)
}

// fillKBArticleAttachments sets the live files of the article and those of its draft: the draft's own
// list, or the live one when the draft never changed it.
func fillKBArticleAttachments(ctx context.Context, tx pgx.Tx, workspaceID string, article *domain.KBArticle) error {
	var err error
	if article.Attachments, err = readKBAttachments(ctx, tx, workspaceID, article.ID, nil); err != nil {
		return err
	}
	if article.Draft == nil {
		return nil
	}
	var ids []string
	if err := tx.QueryRow(ctx, `SELECT attachment_ids::text[] FROM kb_article_drafts WHERE workspace_id=$1 AND article_id=$2 AND locale IS NULL`,
		workspaceID, article.ID).Scan(&ids); err != nil {
		return fmt.Errorf("read knowledge base draft attachments: %w", err)
	}
	if ids == nil {
		article.Draft.Attachments = article.Attachments
		return nil
	}
	article.Draft.Attachments, err = readKBAttachmentsByID(ctx, tx, workspaceID, ids)
	return err
}

// checkKBAttachments refuses a list that names an id twice, an id that is not a file of the library
// outside the trash, a file over the size limit, or files that come to more than an article lists.
func checkKBAttachments(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) error {
	if len(ids) > domain.MaxKBAttachments {
		return &domain.InvalidKBError{Message: fmt.Sprintf("An article lists at most %d files.", domain.MaxKBAttachments)}
	}
	for index, id := range ids {
		if slices.Contains(ids[:index], id) {
			return &domain.InvalidKBError{Message: "The file " + id + " is listed twice; list each file once."}
		}
		if !savedReplyID.MatchString(id) {
			return &domain.KBAttachmentMissingError{ID: id}
		}
	}
	rows, err := tx.Query(ctx, `SELECT id::text,size FROM kb_media WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND kind='file' AND deleted_at IS NULL`,
		workspaceID, ids)
	if err != nil {
		return err
	}
	sizes := map[string]int64{}
	var total int64
	for rows.Next() {
		var id string
		var size int64
		if err := rows.Scan(&id, &size); err != nil {
			rows.Close()
			return err
		}
		sizes[id] = size
		total += size
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		size, found := sizes[id]
		if !found {
			return &domain.KBAttachmentMissingError{ID: id}
		}
		if size > domain.MaxKBFileBytes {
			return domain.ErrKBAttachmentsTooLarge
		}
	}
	if total > domain.MaxKBArticleFileBytes {
		return domain.ErrKBAttachmentsTooLarge
	}
	return nil
}

// replaceKBAttachments makes ids the live list of the article (locale nil) or of one translation.
func replaceKBAttachments(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, locale *string, ids []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM kb_article_attachments WHERE workspace_id=$1 AND article_id=$2 AND locale IS NOT DISTINCT FROM $3`,
		workspaceID, articleID, locale); err != nil {
		return fmt.Errorf("clear knowledge base attachments: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_attachments(workspace_id,article_id,locale,media_id,position,created_at)
		SELECT $1,$2,$3,l.media_id,l.position-1,clock_timestamp() FROM unnest($4::uuid[]) WITH ORDINALITY AS l(media_id,position)`,
		workspaceID, articleID, locale, ids); err != nil {
		return fmt.Errorf("list knowledge base attachments: %w", err)
	}
	return nil
}

// SetKBArticleAttachments sets the files the article lists, in order. A published article keeps the
// list in its draft until the draft is published. seen, when set, is the token the client read.
func (s *Store) SetKBArticleAttachments(ctx context.Context, workspaceID string, actor Actor, id string, ids []string, seen *time.Time) (domain.KBArticle, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticle{}, domain.ErrKBArticleNotFound
	}
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticle, error) {
		status, err := lockKBArticleSeen(ctx, tx, workspaceID, id, seen)
		if err != nil {
			return domain.KBArticle{}, err
		}
		if err := checkKBAttachments(ctx, tx, workspaceID, ids); err != nil {
			return domain.KBArticle{}, err
		}
		// The files are part of the text a review approves: a change is an edit of the article by its
		// author, and a change to a published article's draft is a draft save.
		if status != domain.KBPublished {
			return changeKBArticleLocked(ctx, tx, workspaceID, actor, id, domain.KBArticleEdited, nil, func(tx pgx.Tx) error {
				if err := replaceKBAttachments(ctx, tx, workspaceID, id, nil, ids); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `UPDATE kb_articles SET updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
				return err
			})
		}
		if _, err := tx.Exec(ctx, `INSERT INTO kb_article_drafts(workspace_id,article_id,title,slug,body_md,seo_title,seo_description,attachment_ids,
				updated_by_agent_id,created_at,updated_at)
			SELECT a.workspace_id,a.id,a.title,a.slug,a.body_md,a.seo_title,a.seo_description,$3::uuid[],$4,clock_timestamp(),clock_timestamp()
			FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2
			ON CONFLICT (workspace_id,article_id) WHERE locale IS NULL DO UPDATE SET attachment_ids=EXCLUDED.attachment_ids,
				updated_by_agent_id=EXCLUDED.updated_by_agent_id, updated_at=EXCLUDED.updated_at`,
			workspaceID, id, ids, optionalID(actor.AgentID)); err != nil {
			return domain.KBArticle{}, fmt.Errorf("save knowledge base draft attachments: %w", err)
		}
		return recordKBArticleDraft(ctx, tx, workspaceID, actor, id, true)
	})
	return article, kbError("set knowledge base article attachments", err)
}

// lockKBArticleSeen locks a live article and returns its status; seen, when set, must be its token.
func lockKBArticleSeen(ctx context.Context, tx pgx.Tx, workspaceID, id string, seen *time.Time) (string, error) {
	var status string
	var token time.Time
	err := tx.QueryRow(ctx, `SELECT a.status,`+kbArticleToken+` FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2 AND a.deleted_at IS NULL FOR UPDATE OF a`,
		workspaceID, id).Scan(&status, &token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrKBArticleNotFound
	}
	if err != nil {
		return "", err
	}
	if seen != nil && !token.Equal(*seen) {
		return "", domain.ErrKBChanged
	}
	return status, nil
}

// SetKBTranslationAttachments sets the files a translation lists, in order; a translation has no
// draft, so the list changes at once. It is an edit of the translation: in a collection that needs
// approval, a published translation's list changes only to one a review approved.
func (s *Store) SetKBTranslationAttachments(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, ids []string, seen *time.Time) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		if seen == nil {
			var current time.Time
			err := tx.QueryRow(ctx, `SELECT updated_at FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`,
				workspaceID, articleID, locale).Scan(&current)
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.KBArticleTranslation{}, kbTranslationOrArticleMissing(ctx, tx, workspaceID, articleID)
			}
			if err != nil {
				return domain.KBArticleTranslation{}, err
			}
			seen = &current
		}
		if _, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, *seen); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := checkKBAttachments(ctx, tx, workspaceID, ids); err != nil {
			return domain.KBArticleTranslation{}, err
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

// kbTranslationOrArticleMissing tells a missing article from a missing translation.
func kbTranslationOrArticleMissing(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) error {
	if err := kbArticleExists(ctx, tx, workspaceID, articleID); err != nil {
		return err
	}
	return domain.ErrKBTranslationNotFound
}
