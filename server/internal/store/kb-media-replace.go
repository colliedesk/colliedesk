package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// replacedTranslation is a translation a replacement rewrote, with its text before and after, so an
// undo puts back the text before only while the translation still holds the text after.
type replacedTranslation struct {
	ArticleID string `json:"article_id"`
	Locale    string `json:"locale"`
	Before    string `json:"before"`
	After     string `json:"after"`
}

// lockKBArticlesForMedia locks the articles in id order, then the media every one of their texts and
// translations names plus extra, the way a save does (lockNamedKBMedia), before anything is written.
// A replacement touches many articles in one transaction; locking all of them and all their media up
// front, each in id order, means it never holds an item while it waits for an article a save holds.
func lockKBArticlesForMedia(ctx context.Context, tx pgx.Tx, workspaceID string, articles []string, extra ...string) ([]string, error) {
	if len(articles) == 0 {
		return lockNamedKBMedia(ctx, tx, workspaceID, extra)
	}
	var text string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(string_agg(t, ' '), '') FROM (
			SELECT a.body_md || ' ' || COALESCE((SELECT string_agg(r.body_md, ' ') FROM kb_article_translations r WHERE r.workspace_id=a.workspace_id AND r.article_id=a.id), '') AS t
			FROM (SELECT * FROM kb_articles WHERE workspace_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR UPDATE) a) texts`, workspaceID, articles).Scan(&text); err != nil {
		return nil, fmt.Errorf("lock knowledge base articles: %w", err)
	}
	named := domain.KBMediaIDs(text)
	for _, id := range extra {
		if !slices.Contains(named, id) {
			named = append(named, id)
		}
	}
	return lockNamedKBMedia(ctx, tx, workspaceID, named)
}

// ReplaceKBMedia puts the item with in the place of the item id in every live article whose current
// text (or a translation) shows it, as a new version of kind media_replace, and records the
// replacement so it can be undone. An image is replaced by an image, a video by a video; with is
// taken out of the library's trash if it is there.
func (s *Store) ReplaceKBMedia(ctx context.Context, workspaceID string, actor Actor, id, with string) (domain.KBMediaReplacement, error) {
	if !savedReplyID.MatchString(id) || !savedReplyID.MatchString(with) {
		return domain.KBMediaReplacement{}, domain.ErrKBMediaNotFound
	}
	if strings.EqualFold(id, with) {
		return domain.KBMediaReplacement{}, &domain.InvalidKBError{Message: "Replace an item with another one."}
	}
	from, to := strings.ToLower(id), strings.ToLower(with)
	replacement, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBMediaReplacement, error) {
		rows, err := tx.Query(ctx, `SELECT DISTINCT l.article_id::text FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND a.deleted_at IS NULL ORDER BY 1`, workspaceID, from)
		if err != nil {
			return domain.KBMediaReplacement{}, err
		}
		articles, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return domain.KBMediaReplacement{}, err
		}
		// Both items are locked with the rest, so neither is purged under the replacement.
		found, err := lockKBArticlesForMedia(ctx, tx, workspaceID, articles, from, to)
		if err != nil {
			return domain.KBMediaReplacement{}, err
		}
		if !slices.Contains(found, from) || !slices.Contains(found, to) {
			return domain.KBMediaReplacement{}, domain.ErrKBMediaNotFound
		}
		var kinds []string
		if err := tx.QueryRow(ctx, `SELECT array_agg(DISTINCT kind) FROM kb_media WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, []string{from, to}).Scan(&kinds); err != nil {
			return domain.KBMediaReplacement{}, err
		}
		if len(kinds) != 1 {
			return domain.KBMediaReplacement{}, domain.ErrKBMediaKindMismatch
		}
		changed, versions := []string{}, []int{}
		translations := []replacedTranslation{}
		for _, articleID := range articles {
			version, rewritten, err := replaceInKBArticle(ctx, tx, workspaceID, articleID, actor, from, to)
			if err != nil {
				return domain.KBMediaReplacement{}, err
			}
			if version > 0 {
				changed, versions = append(changed, articleID), append(versions, version)
			}
			translations = append(translations, rewritten...)
		}
		recorded, _ := json.Marshal(translations)
		return scanKBMediaReplacement(tx.QueryRow(ctx, `INSERT INTO kb_media_replacements(workspace_id,from_media_id,to_media_id,article_ids,article_versions,translations,replaced_by_agent_id)
			VALUES($1,$2,$3,$4::uuid[],$5::integer[],$6,$7) RETURNING `+kbMediaReplacementColumns, workspaceID, from, to, changed, versions, recorded, optionalID(actor.AgentID)))
	})
	if err != nil {
		return domain.KBMediaReplacement{}, kbMediaReplaceError("replace knowledge base media", err)
	}
	return replacement, nil
}

// replaceInKBArticle rewrites from to to in the current text of a locked article and its translations,
// and returns the media_replace version it recorded (0 when nothing changed) and the translations it
// rewrote. The ids keep their length, so no text grows past its limit.
func replaceInKBArticle(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, actor Actor, from, to string) (int, []replacedTranslation, error) {
	var body string
	if err := tx.QueryRow(ctx, `SELECT body_md FROM kb_articles WHERE workspace_id=$1 AND id=$2`, workspaceID, articleID).Scan(&body); err != nil {
		return 0, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT locale, body_md FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 ORDER BY locale FOR UPDATE`, workspaceID, articleID)
	if err != nil {
		return 0, nil, err
	}
	translations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (replacedTranslation, error) {
		item := replacedTranslation{ArticleID: articleID}
		return item, row.Scan(&item.Locale, &item.Before)
	})
	if err != nil {
		return 0, nil, err
	}
	var rewritten []replacedTranslation
	for _, item := range translations {
		item.After = domain.ReplaceKBMediaIDs(item.Before, from, to)
		if item.After == item.Before {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_translations SET body_md=$4, updated_at=clock_timestamp() WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`,
			workspaceID, articleID, item.Locale, item.After); err != nil {
			return 0, nil, err
		}
		rewritten = append(rewritten, item)
	}
	after := domain.ReplaceKBMediaIDs(body, from, to)
	if after == body && len(rewritten) == 0 {
		return 0, nil, nil
	}
	version, err := recordKBArticleMediaVersion(ctx, tx, workspaceID, articleID, actor, after)
	return version, rewritten, err
}

// recordKBArticleMediaVersion sets the article's text and records it as a media_replace version, and
// returns the version's number.
func recordKBArticleMediaVersion(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, actor Actor, body string) (int, error) {
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET body_md=$3, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`, workspaceID, articleID, body); err != nil {
		return 0, err
	}
	if err := addKBArticleVersion(ctx, tx, workspaceID, articleID, actor, domain.KBArticleMediaReplaced, nil); err != nil {
		return 0, err
	}
	var version int
	err := tx.QueryRow(ctx, `SELECT max(version) FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2`, workspaceID, articleID).Scan(&version)
	return version, err
}

// UndoKBMediaReplacement puts back the text each article and translation had before the replacement,
// where nothing changed since: an article whose newest version is still the replacement's gets the
// text of the version before it, a translation that still holds the replaced text gets its text
// before. Anything changed since keeps its text and is listed as skipped. Undoing twice changes
// nothing more.
func (s *Store) UndoKBMediaReplacement(ctx context.Context, workspaceID string, actor Actor, id string) (domain.KBMediaReplacement, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBMediaReplacement{}, domain.ErrKBMediaReplacementNotFound
	}
	replacement, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBMediaReplacement, error) {
		var versions []int
		var raw []byte
		replacement, err := scanKBMediaReplacement(tx.QueryRow(ctx, `SELECT `+kbMediaReplacementColumns+` FROM kb_media_replacements
			WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.KBMediaReplacement{}, domain.ErrKBMediaReplacementNotFound
		}
		if err != nil || replacement.UndoneAt != nil {
			return replacement, err
		}
		if err := tx.QueryRow(ctx, `SELECT article_versions, translations FROM kb_media_replacements WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&versions, &raw); err != nil {
			return domain.KBMediaReplacement{}, err
		}
		var translations []replacedTranslation
		if err := json.Unmarshal(raw, &translations); err != nil {
			return domain.KBMediaReplacement{}, fmt.Errorf("read replaced translations: %w", err)
		}
		articles := slices.Clone(replacement.ArticleIDs)
		for _, item := range translations {
			if !slices.Contains(articles, item.ArticleID) {
				articles = append(articles, item.ArticleID)
			}
		}
		slices.Sort(articles)
		found, err := lockKBArticlesForMedia(ctx, tx, workspaceID, articles, replacement.From)
		if err != nil {
			return domain.KBMediaReplacement{}, err
		}
		if !slices.Contains(found, replacement.From) {
			return domain.KBMediaReplacement{}, domain.ErrKBMediaGone
		}
		skipped := []string{}
		restored := map[string]bool{}
		for _, item := range translations {
			tag, err := tx.Exec(ctx, `UPDATE kb_article_translations SET body_md=$4, updated_at=clock_timestamp()
				WHERE workspace_id=$1 AND article_id=$2 AND locale=$3 AND body_md=$5`, workspaceID, item.ArticleID, item.Locale, item.Before, item.After)
			if err != nil {
				return domain.KBMediaReplacement{}, err
			}
			if tag.RowsAffected() == 0 && !slices.Contains(skipped, item.ArticleID) {
				skipped = append(skipped, item.ArticleID)
			}
			restored[item.ArticleID] = true
		}
		for i, articleID := range replacement.ArticleIDs {
			var newest int
			var before *string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(max(version),0), (SELECT body_md FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2 AND version=$3-1)
				FROM kb_article_versions WHERE workspace_id=$1 AND article_id=$2`, workspaceID, articleID, versions[i]).Scan(&newest, &before); err != nil {
				return domain.KBMediaReplacement{}, err
			}
			if newest != versions[i] || before == nil {
				if !slices.Contains(skipped, articleID) {
					skipped = append(skipped, articleID)
				}
				continue
			}
			if _, err := recordKBArticleMediaVersion(ctx, tx, workspaceID, articleID, actor, *before); err != nil {
				return domain.KBMediaReplacement{}, err
			}
			delete(restored, articleID)
		}
		// A translation put back on an article whose text did not change still needs its links read again.
		for articleID := range restored {
			if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
				return domain.KBMediaReplacement{}, err
			}
		}
		return scanKBMediaReplacement(tx.QueryRow(ctx, `UPDATE kb_media_replacements SET undone_at=clock_timestamp(), undone_by_agent_id=$3, skipped_article_ids=$4::uuid[]
			WHERE workspace_id=$1 AND id=$2 RETURNING `+kbMediaReplacementColumns, workspaceID, id, optionalID(actor.AgentID), skipped))
	})
	if err != nil {
		return domain.KBMediaReplacement{}, kbMediaReplaceError("undo knowledge base media replacement", err)
	}
	return replacement, nil
}

const kbMediaReplacementColumns = `id::text,from_media_id::text,to_media_id::text,article_ids::text[],translations,skipped_article_ids::text[],created_at,undone_at`

func scanKBMediaReplacement(row pgx.Row) (domain.KBMediaReplacement, error) {
	var replacement domain.KBMediaReplacement
	var translations []byte
	if err := row.Scan(&replacement.ID, &replacement.From, &replacement.To, &replacement.ArticleIDs, &translations, &replacement.SkippedArticleIDs,
		&replacement.CreatedAt, &replacement.UndoneAt); err != nil {
		return replacement, err
	}
	replacement.CreatedAt, replacement.UndoneAt = domain.UTC(replacement.CreatedAt), domain.UTCOptional(replacement.UndoneAt)
	if replacement.ArticleIDs == nil {
		replacement.ArticleIDs = []string{}
	}
	if replacement.SkippedArticleIDs == nil {
		replacement.SkippedArticleIDs = []string{}
	}
	var recorded []replacedTranslation
	if err := json.Unmarshal(translations, &recorded); err != nil {
		return replacement, fmt.Errorf("read replaced translations: %w", err)
	}
	replacement.Translations = make([]domain.KBMediaReplacedTranslation, len(recorded))
	for i, item := range recorded {
		replacement.Translations[i] = domain.KBMediaReplacedTranslation{ArticleID: item.ArticleID, Locale: item.Locale}
	}
	return replacement, nil
}

func kbMediaReplaceError(what string, err error) error {
	for _, known := range []error{domain.ErrKBMediaReplacementNotFound, domain.ErrKBMediaKindMismatch, domain.ErrKBMediaGone} {
		if errors.Is(err, known) {
			return known
		}
	}
	return kbError(what, err)
}
