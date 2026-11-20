package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// listedByShownArticle is the rule that makes a file public, on rows l of kb_article_attachments joined
// to their article a: the article is published and outside the trash, and the list is its own or the
// list of one of its published translations in a language customers see.
const listedByShownArticle = publishedKBArticle + ` AND (l.locale IS NULL OR EXISTS (SELECT 1 FROM kb_article_translations t
		JOIN help_center_languages hl ON hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible
		WHERE t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=l.locale AND t.status='published' AND t.deleted_at IS NULL))`

// HelpCenterFile returns a file the help center may hand out: a published article, or a published
// translation shown to customers, lists it, and it is not in the library's trash. A draft's list never
// counts. Every other id gives domain.ErrKBMediaNotFound.
func (s *Store) HelpCenterFile(ctx context.Context, workspaceID, id string) (StoredKBMedia, error) {
	if !savedReplyID.MatchString(id) || id != strings.ToLower(id) {
		return StoredKBMedia{}, domain.ErrKBMediaNotFound
	}
	file, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBMedia, error) {
		return scanKBMedia(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2
			AND m.kind='file' AND m.deleted_at IS NULL AND EXISTS (
			SELECT 1 FROM kb_article_attachments l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND `+listedByShownArticle+`)`, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBMedia{}, domain.ErrKBMediaNotFound
	}
	if err != nil {
		return StoredKBMedia{}, fmt.Errorf("get help center file: %w", err)
	}
	return file, nil
}

// publicKBAttachments are the files a published article's page lists in language locale ("" the
// primary one): the translation's own list, or the article's when the translation lists none. Files in
// the library's trash are left out.
func publicKBAttachments(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string) (domain.KBAttachments, error) {
	read := func(locale *string) (domain.KBAttachments, error) {
		rows, err := tx.Query(ctx, `SELECT m.id::text, m.name, m.content_type, m.size FROM kb_article_attachments l
			JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id AND m.kind='file' AND m.deleted_at IS NULL
			WHERE l.workspace_id=$1 AND l.article_id=$2 AND l.locale IS NOT DISTINCT FROM $3 ORDER BY l.position`, workspaceID, articleID, locale)
		if err != nil {
			return nil, fmt.Errorf("read help center attachments: %w", err)
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBAttachment, error) {
			var item domain.KBAttachment
			err := row.Scan(&item.ID, &item.Name, &item.ContentType, &item.Size)
			return item, err
		})
		return domain.KBAttachments(items), err
	}
	if locale != "" {
		if items, err := read(&locale); err != nil || len(items) > 0 {
			return items, err
		}
	}
	return read(nil)
}
