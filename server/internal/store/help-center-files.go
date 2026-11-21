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
const listedByShownArticle = publicKBArticle + ` AND (l.locale IS NULL OR EXISTS (SELECT 1 FROM kb_article_translations t
		JOIN help_center_languages hl ON hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible
		WHERE t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=l.locale AND ` + publicKBTranslation + `))`

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

// fileHits is the query files(article_id, rank) of the articles whose page lists a file outside the
// trash whose text matches the search $2 of workspace $1, the lists chosen by listed on rows l.
func fileHits(listed string) string {
	return `files AS (SELECT l.article_id, max(ts_rank(m.text_search, q.q)) AS rank
		FROM kb_media m CROSS JOIN LATERAL (SELECT ` + kbQuery + ` AS q) q
		JOIN kb_article_attachments l ON l.workspace_id=m.workspace_id AND l.media_id=m.id
		WHERE m.workspace_id=$1 AND m.kind='file' AND m.deleted_at IS NULL AND m.text IS NOT NULL AND m.text_search @@ q.q AND ` + listed + `
		GROUP BY l.article_id)`
}

// translationFiles picks, on rows l, the lists the page in language $6 shows (publicKBAttachments): the
// translation's own, or the article's while the translation lists no file outside the trash.
const translationFiles = `(l.locale=$6 OR l.locale IS NULL AND NOT EXISTS (SELECT 1 FROM kb_article_attachments o
		JOIN kb_media om ON om.workspace_id=o.workspace_id AND om.id=o.media_id AND om.kind='file' AND om.deleted_at IS NULL
		WHERE o.workspace_id=l.workspace_id AND o.article_id=l.article_id AND o.locale=$6))`

// indexedKBAttachments fills in the files each published article's own page lists, in one read.
func indexedKBAttachments(ctx context.Context, tx pgx.Tx, workspaceID string, articles []domain.PublicKBArticle) error {
	if len(articles) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT a.number, m.id::text, m.name, m.content_type, m.size FROM kb_article_attachments l
		JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id AND m.kind='file' AND m.deleted_at IS NULL
		WHERE l.workspace_id=$1 AND l.locale IS NULL AND `+publicKBArticle+` ORDER BY a.number, l.position`, workspaceID)
	if err != nil {
		return fmt.Errorf("read help center index attachments: %w", err)
	}
	byNumber := map[int64]domain.KBAttachments{}
	var number int64
	var item domain.KBAttachment
	_, err = pgx.ForEachRow(rows, []any{&number, &item.ID, &item.Name, &item.ContentType, &item.Size}, func() error {
		byNumber[number] = append(byNumber[number], item)
		return nil
	})
	if err != nil {
		return fmt.Errorf("read help center index attachments: %w", err)
	}
	for i := range articles {
		articles[i].Attachments = byNumber[articles[i].Number]
	}
	return nil
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
