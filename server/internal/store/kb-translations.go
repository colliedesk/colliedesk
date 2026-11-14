package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Translations are language versions of an article (docs/internals/knowledge-base.md, "Translations"):
// one per article and language, with their own slug, search fields, status, versions and trash. A
// translation in a language the help center does not list can be read, edited and deleted, never made
// or published; the help center shows only published translations of published articles in a
// language it shows.

// kbTranslationSelect reads translations t with their newest version; extra columns follow the change columns.
func kbTranslationSelect(extra, join string) string {
	return `SELECT t.article_id::text,t.locale,t.title,t.slug,t.status,t.published_at,t.outdated,t.outdated_at,t.created_at,t.updated_at,t.deleted_at IS NOT NULL,` +
		kbArticleChangeColumns + extra + `
	FROM kb_article_translations t
	LEFT JOIN LATERAL (SELECT * FROM kb_article_translation_versions x WHERE x.workspace_id=t.workspace_id AND x.article_id=t.article_id AND x.locale=t.locale
		ORDER BY x.version DESC LIMIT 1) v ON true
	LEFT JOIN agents p ON p.workspace_id=v.workspace_id AND p.id=v.changed_by_agent_id` + join
}

func scanKBTranslationSummary(row pgx.Row, extra ...any) (domain.KBTranslationSummary, error) {
	var item domain.KBTranslationSummary
	var change changeRow
	targets := append([]any{&item.ArticleID, &item.Locale, &item.Title, &item.Slug, &item.Status, &item.PublishedAt, &item.Outdated, &item.OutdatedAt,
		&item.CreatedAt, &item.UpdatedAt, &item.InTrash}, change.targets()...)
	if err := row.Scan(append(targets, extra...)...); err != nil {
		return item, err
	}
	item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
	item.PublishedAt, item.OutdatedAt = domain.UTCOptional(item.PublishedAt), domain.UTCOptional(item.OutdatedAt)
	item.LastChange = change.change()
	return item, nil
}

// listKBArticleTranslations reads the summaries of an article's translations, those in the trash too.
func listKBArticleTranslations(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) ([]domain.KBTranslationSummary, error) {
	rows, err := tx.Query(ctx, kbTranslationSelect(``, ``)+` WHERE t.workspace_id=$1 AND t.article_id=$2 ORDER BY t.locale`, workspaceID, articleID)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBTranslationSummary, error) {
		return scanKBTranslationSummary(row)
	})
	if items == nil {
		items = []domain.KBTranslationSummary{}
	}
	return items, err
}

// readKBTranslation reads one translation, in or out of the trash, with its text and the article as it is now.
func readKBTranslation(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string) (domain.KBArticleTranslation, error) {
	var item domain.KBArticleTranslation
	var original domain.KBTranslationOriginal
	summary, err := scanKBTranslationSummary(tx.QueryRow(ctx, kbTranslationSelect(`,t.body_md,t.seo_title,t.seo_description,t.source_html IS NOT NULL,t.deleted_at,
			COALESCE((SELECT l.visible FROM help_center_languages l WHERE l.workspace_id=t.workspace_id AND l.locale=t.locale), false),
			a.id::text,a.number,COALESCE(h.locale,'en'),a.title,a.body_md,a.seo_title,a.seo_description,a.status,a.collection_id::text,c.name,
			COALESCE((SELECT max(w.version) FROM kb_article_versions w WHERE w.workspace_id=a.workspace_id AND w.article_id=a.id), 0),a.updated_at`,
		` JOIN kb_articles a ON a.workspace_id=t.workspace_id AND a.id=t.article_id
			JOIN kb_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id
			LEFT JOIN help_centers h ON h.workspace_id=t.workspace_id`)+` WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3`, workspaceID, articleID, locale),
		&item.BodyMD, &item.SEOTitle, &item.SEODescription, &item.HasSourceHTML, &item.DeletedAt, &item.LanguageVisible,
		&original.ID, &original.Number, &original.Locale, &original.Title, &original.BodyMD, &original.SEOTitle, &original.SEODescription, &original.Status,
		&original.CollectionID, &original.CollectionName, &original.Version, &original.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrKBTranslationNotFound
	}
	if err != nil {
		return item, err
	}
	original.UpdatedAt = domain.UTC(original.UpdatedAt)
	item.KBTranslationSummary, item.DeletedAt, item.Original = summary, domain.UTCOptional(item.DeletedAt), original
	return item, nil
}

// translationKey refuses an id or locale that cannot name a translation.
func translationKey(articleID, locale string) error {
	if !savedReplyID.MatchString(articleID) {
		return domain.ErrKBArticleNotFound
	}
	if domain.CheckLocale(locale) != nil {
		return domain.ErrKBTranslationNotFound
	}
	return nil
}

// GetKBTranslation returns one translation of an article, also from the trash.
func (s *Store) GetKBTranslation(ctx context.Context, workspaceID, articleID, locale string) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		if err := kbArticleExists(ctx, tx, workspaceID, articleID); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	})
	return item, kbError("get knowledge base translation", err)
}

func kbArticleExists(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2)`, workspaceID, articleID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.ErrKBArticleNotFound
	}
	return nil
}

// KBTranslationPublished reports whether the translation is published now (a write to it then changes
// what the help center may show).
func (s *Store) KBTranslationPublished(ctx context.Context, workspaceID, articleID, locale string) (bool, error) {
	if translationKey(articleID, locale) != nil {
		return false, nil
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var published bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 AND locale=$3 AND status='published')`,
			workspaceID, articleID, locale).Scan(&published)
		return published, err
	})
}

// addKBTranslationVersion records the translation as it is now as its next version, links the media its
// text points at to the article and bumps the knowledge base version.
func addKBTranslationVersion(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string, actor Actor, kind string, restoredFrom *int) error {
	if err := recordKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, kind, restoredFrom); err != nil {
		return err
	}
	if _, err := linkKBMedia(ctx, tx, workspaceID, articleID); err != nil {
		return err
	}
	return bumpKB(ctx, tx, workspaceID)
}

// recordKBTranslationVersion records the translation as it is now as its next version.
func recordKBTranslationVersion(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string, actor Actor, kind string, restoredFrom *int) error {
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_translation_versions(workspace_id,article_id,locale,version,kind,restored_from_version,title,slug,body_md,seo_title,seo_description,status,
			changed_by_agent_id,token_id,token_label,agent_request_id,created_at)
		SELECT t.workspace_id,t.article_id,t.locale,COALESCE((SELECT max(version) FROM kb_article_translation_versions WHERE workspace_id=$1 AND article_id=$2 AND locale=$3),0)+1,
			$4,$5,t.title,t.slug,t.body_md,t.seo_title,t.seo_description,t.status,$6,s.id,s.label,$8,clock_timestamp()
		FROM kb_article_translations t LEFT JOIN sessions s ON s.workspace_id=t.workspace_id AND s.id=$7
		WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3`,
		workspaceID, articleID, locale, kind, restoredFrom, optionalID(actor.AgentID), optionalID(actor.TokenID), optionalID(actor.RequestID)); err != nil {
		return fmt.Errorf("record knowledge base translation version: %w", err)
	}
	return nil
}

// checkKBLanguage refuses a locale that is not a listed language of the help center (the primary one
// included: the article itself is in it).
func checkKBLanguage(ctx context.Context, tx pgx.Tx, workspaceID, locale string) error {
	var listed bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM help_center_languages l WHERE l.workspace_id=$1 AND l.locale=$2
			AND l.locale <> COALESCE((SELECT h.locale FROM help_centers h WHERE h.workspace_id=$1), 'en'))`, workspaceID, locale).Scan(&listed); err != nil {
		return err
	}
	if !listed {
		return domain.ErrKBLanguageNotListed
	}
	return nil
}

// KBTranslationInput is a new translation; an empty Slug is made from the title, an empty Status is a draft.
type KBTranslationInput struct {
	Locale, Title, Slug, BodyMD, Status string
	SEOTitle, SEODescription            string
}

func cleanKBTranslationFields(title, slug, body, status, seoTitle, seoDescription *string) error {
	if title != nil {
		cleaned, err := domain.CleanKBText("title", *title, domain.MaxKBArticleTitle)
		if err != nil {
			return err
		}
		*title = cleaned
	}
	if slug != nil {
		if err := domain.CheckKBSlug(*slug); err != nil {
			return err
		}
	}
	if body != nil {
		if err := domain.CheckKBBody(*body); err != nil {
			return err
		}
	}
	if status != nil {
		if err := domain.CheckKBStatus(*status); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		value *string
		what  string
		limit int
	}{{seoTitle, "search title", domain.MaxKBSEOTitle}, {seoDescription, "meta description", domain.MaxKBSEODescription}} {
		if field.value == nil {
			continue
		}
		cleaned, err := domain.CleanKBSEO(field.what, *field.value, field.limit)
		if err != nil {
			return err
		}
		*field.value = cleaned
	}
	return nil
}

// CreateKBTranslation adds the translation of a live article in a listed language, up to date with
// the article as it is now. domain.ErrKBTranslationExists when it has one, in the trash too.
func (s *Store) CreateKBTranslation(ctx context.Context, workspaceID string, actor Actor, articleID string, input KBTranslationInput) (domain.KBArticleTranslation, error) {
	if !savedReplyID.MatchString(articleID) {
		return domain.KBArticleTranslation{}, domain.ErrKBArticleNotFound
	}
	if err := domain.CheckLocale(input.Locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	if input.Status == "" {
		input.Status = domain.KBDraft
	}
	slug := &input.Slug
	if input.Slug == "" {
		slug = nil
	}
	if err := cleanKBTranslationFields(&input.Title, slug, &input.BodyMD, &input.Status, &input.SEOTitle, &input.SEODescription); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	if input.Slug == "" {
		input.Slug = domain.Slugify(input.Title, "article")
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		var live bool
		err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM kb_articles WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, articleID).Scan(&live)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
			return domain.KBArticleTranslation{}, domain.ErrKBArticleNotFound
		}
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := checkKBLanguage(ctx, tx, workspaceID, input.Locale); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,slug,body_md,status,published_at,seo_title,seo_description,author_agent_id,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $7='published' THEN clock_timestamp() END,$8,$9,$10,clock_timestamp(),clock_timestamp())
			ON CONFLICT (workspace_id,article_id,locale) DO NOTHING`,
			workspaceID, articleID, input.Locale, input.Title, input.Slug, input.BodyMD, input.Status, input.SEOTitle, input.SEODescription, optionalID(actor.AgentID))
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if tag.RowsAffected() == 0 {
			return domain.KBArticleTranslation{}, domain.ErrKBTranslationExists
		}
		if err := addKBTranslationVersion(ctx, tx, workspaceID, articleID, input.Locale, actor, domain.KBArticleCreated, nil); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, input.Locale)
	})
	return item, kbError("create knowledge base translation", err)
}

// KBTranslationChange is a partial update of a translation; a nil field stays. Outdated false marks
// the translation up to date with the article as it is now, true marks it outdated.
type KBTranslationChange struct {
	Title, Slug, BodyMD, Status, SEOTitle, SEODescription *string
	Outdated                                              *bool
}

// UpdateKBTranslation changes a live translation and records the version. seen is the updated_at the
// client read. A change of the flag alone is recorded as mark_current or mark_outdated.
func (s *Store) UpdateKBTranslation(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, seen time.Time, change KBTranslationChange) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	if err := cleanKBTranslationFields(change.Title, change.Slug, change.BodyMD, change.Status, change.SEOTitle, change.SEODescription); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		before, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, seen)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if change.Status != nil && *change.Status == domain.KBPublished && before.Status != domain.KBPublished {
			// A translation in a language the help center no longer lists stays a draft.
			if err := checkKBLanguage(ctx, tx, workspaceID, locale); err != nil {
				return domain.KBArticleTranslation{}, err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_translations SET title=COALESCE($4,title), slug=COALESCE($5,slug), body_md=COALESCE($6,body_md),
				status=COALESCE($7,status), published_at=CASE WHEN COALESCE($7,status)='published' THEN COALESCE(published_at, clock_timestamp()) END,
				seo_title=COALESCE($8,seo_title), seo_description=COALESCE($9,seo_description),
				outdated=COALESCE($10,outdated), outdated_at=CASE WHEN COALESCE($10,outdated) THEN COALESCE(outdated_at, clock_timestamp()) END,
				updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`,
			workspaceID, articleID, locale, change.Title, change.Slug, change.BodyMD, change.Status, change.SEOTitle, change.SEODescription, change.Outdated); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		kind := domain.KBArticleEdited
		if change.Title == nil && change.Slug == nil && change.BodyMD == nil && change.Status == nil && change.SEOTitle == nil && change.SEODescription == nil && change.Outdated != nil {
			kind = domain.KBTranslationMarkedCurrent
			if *change.Outdated {
				kind = domain.KBTranslationMarkedOutdated
			}
		}
		if err := addKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, kind, nil); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	})
	return item, kbError("change knowledge base translation", err)
}

type lockedTranslation struct {
	Status string
}

// lockLiveKBTranslation locks a live translation the client read at seen; the article's row is locked
// first, as every write to an article and its translations does.
func lockLiveKBTranslation(ctx context.Context, tx pgx.Tx, workspaceID, articleID, locale string, seen time.Time) (lockedTranslation, error) {
	var found lockedTranslation
	if err := lockKBArticleRow(ctx, tx, workspaceID, articleID); err != nil {
		return found, err
	}
	var updatedAt time.Time
	var deleted bool
	err := tx.QueryRow(ctx, `SELECT updated_at, deleted_at IS NOT NULL, status FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 AND locale=$3 FOR UPDATE`,
		workspaceID, articleID, locale).Scan(&updatedAt, &deleted, &found.Status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && deleted) {
		return found, domain.ErrKBTranslationNotFound
	}
	if err != nil {
		return found, err
	}
	if !updatedAt.Equal(seen) {
		return found, domain.ErrKBChanged
	}
	return found, nil
}

// lockKBArticleRow locks an article's row (in or out of the trash); domain.ErrKBArticleNotFound when it has none.
func lockKBArticleRow(ctx context.Context, tx pgx.Tx, workspaceID, articleID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM kb_articles WHERE workspace_id=$1 AND id=$2 FOR NO KEY UPDATE`, workspaceID, articleID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrKBArticleNotFound
	}
	return err
}

// DeleteKBTranslation moves a translation to the trash and records the version.
func (s *Store) DeleteKBTranslation(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, seen time.Time) error {
	if err := translationKey(articleID, locale); err != nil {
		return err
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if _, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, seen); err != nil {
			return struct{}{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_translations SET deleted_at=$4, deleted_by_agent_id=$5, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`, workspaceID, articleID, locale, s.now(), optionalID(actor.AgentID)); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, addKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, domain.KBArticleDeleted, nil)
	})
	return kbError("delete knowledge base translation", err)
}

// UndeleteKBTranslation takes a translation out of the trash as it was.
func (s *Store) UndeleteKBTranslation(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, seen time.Time) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		if err := lockKBArticleRow(ctx, tx, workspaceID, articleID); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		var updatedAt time.Time
		err := tx.QueryRow(ctx, `SELECT updated_at FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 AND locale=$3 AND deleted_at IS NOT NULL FOR UPDATE`,
			workspaceID, articleID, locale).Scan(&updatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.KBArticleTranslation{}, domain.ErrKBTranslationNotFound
		}
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if !updatedAt.Equal(seen) {
			return domain.KBArticleTranslation{}, domain.ErrKBChanged
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_translations SET deleted_at=NULL, deleted_by_agent_id=NULL, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND article_id=$2 AND locale=$3`, workspaceID, articleID, locale); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if err := addKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, domain.KBArticleUndeleted, nil); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	})
	return item, kbError("restore knowledge base translation", err)
}

// ListTrashedKBTranslations returns the translations in the trash, most recently deleted first, with
// their article's title and number. A workspace's translations are bounded by its articles and
// languages, so one answer holds a page of at most limit.
func (s *Store) ListTrashedKBTranslations(ctx context.Context, workspaceID string, limit int, cursor string) ([]domain.TrashedKBTranslation, string, error) {
	query := kbTranslationSelect(`,a.title,a.number,t.deleted_at,t.deleted_by_agent_id::text,COALESCE(NULLIF(d.display_name,''),d.email)`,
		` JOIN kb_articles a ON a.workspace_id=t.workspace_id AND a.id=t.article_id
		LEFT JOIN agents d ON d.workspace_id=t.workspace_id AND d.id=t.deleted_by_agent_id`) + ` WHERE t.workspace_id=$1 AND t.deleted_at IS NOT NULL`
	args := []any{workspaceID, limit + 1}
	if cursor != "" {
		at, key, err := decodeTrashCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		articleID, locale, found := strings.Cut(key, "/")
		if !found || !savedReplyID.MatchString(articleID) {
			return nil, "", ErrBadCursor
		}
		query += ` AND (t.deleted_at, t.article_id, t.locale) < ($3, $4, $5)`
		args = append(args, at, articleID, locale)
	}
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TrashedKBTranslation, error) {
		rows, err := tx.Query(ctx, query+` ORDER BY t.deleted_at DESC, t.article_id DESC, t.locale DESC LIMIT $2`, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TrashedKBTranslation, error) {
			var item domain.TrashedKBTranslation
			var deleterID, deleterName *string
			summary, err := scanKBTranslationSummary(row, &item.ArticleTitle, &item.ArticleNumber, &item.DeletedAt, &deleterID, &deleterName)
			if err != nil {
				return item, err
			}
			item.KBTranslationSummary, item.DeletedAt = summary, domain.UTC(item.DeletedAt)
			item.DeletedBy = savedReplyPerson(deleterID, deleterName)
			item.PurgesAt = item.DeletedAt.Add(domain.KBTrashRetention)
			return item, nil
		})
	})
	if err != nil {
		return nil, "", fmt.Errorf("list knowledge base translation trash: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.DeletedAt.Format(time.RFC3339Nano) + " " + last.ArticleID + "/" + last.Locale))
	}
	if items == nil {
		items = []domain.TrashedKBTranslation{}
	}
	return items, next, nil
}

// KBTranslationVersions returns one page of the versions of a translation, newest first.
func (s *Store) KBTranslationVersions(ctx context.Context, workspaceID, articleID, locale string, limit, before int) ([]domain.KBTranslationVersion, int, error) {
	if err := translationKey(articleID, locale); err != nil {
		return nil, 0, err
	}
	if before <= 0 {
		before = math.MaxInt32
	}
	versions, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBTranslationVersion, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_article_translations WHERE workspace_id=$1 AND article_id=$2 AND locale=$3)`,
			workspaceID, articleID, locale).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			if err := kbArticleExists(ctx, tx, workspaceID, articleID); err != nil {
				return nil, err
			}
			return nil, domain.ErrKBTranslationNotFound
		}
		rows, err := tx.Query(ctx, `SELECT `+kbArticleChangeColumns+`,v.title,v.slug,v.body_md,v.seo_title,v.seo_description,v.status FROM kb_article_translation_versions v
			LEFT JOIN agents p ON p.workspace_id=v.workspace_id AND p.id=v.changed_by_agent_id
			WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.locale=$3 AND v.version<$4 ORDER BY v.version DESC LIMIT $5`, workspaceID, articleID, locale, before, limit+1)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBTranslationVersion, error) {
			var change changeRow
			var version domain.KBTranslationVersion
			if err := row.Scan(append(change.targets(), &version.Title, &version.Slug, &version.BodyMD, &version.SEOTitle, &version.SEODescription, &version.Status)...); err != nil {
				return version, err
			}
			version.KBArticleChange = *change.change()
			return version, nil
		})
	})
	if err != nil {
		return nil, 0, kbError("list knowledge base translation versions", err)
	}
	next := 0
	if len(versions) > limit {
		versions = versions[:limit]
		next = versions[limit-1].Version
	}
	if versions == nil {
		versions = []domain.KBTranslationVersion{}
	}
	return versions, next, nil
}

// RestoreKBTranslationVersion makes the title, slug, text and search fields of version current as a
// new version; the status and the outdated flag stay.
func (s *Store) RestoreKBTranslationVersion(ctx context.Context, workspaceID string, actor Actor, articleID, locale string, version int, seen time.Time) (domain.KBArticleTranslation, error) {
	if err := translationKey(articleID, locale); err != nil {
		return domain.KBArticleTranslation{}, err
	}
	item, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleTranslation, error) {
		if _, err := lockLiveKBTranslation(ctx, tx, workspaceID, articleID, locale, seen); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		tag, err := tx.Exec(ctx, `UPDATE kb_article_translations t SET title=v.title, slug=v.slug, body_md=v.body_md, seo_title=v.seo_title, seo_description=v.seo_description,
				updated_at=clock_timestamp() FROM kb_article_translation_versions v
			WHERE t.workspace_id=$1 AND t.article_id=$2 AND t.locale=$3 AND v.workspace_id=t.workspace_id AND v.article_id=t.article_id AND v.locale=t.locale AND v.version=$4`,
			workspaceID, articleID, locale, version)
		if err != nil {
			return domain.KBArticleTranslation{}, err
		}
		if tag.RowsAffected() == 0 {
			return domain.KBArticleTranslation{}, domain.ErrKBTranslationVersionNotFound
		}
		if err := addKBTranslationVersion(ctx, tx, workspaceID, articleID, locale, actor, domain.KBArticleRestored, &version); err != nil {
			return domain.KBArticleTranslation{}, err
		}
		return readKBTranslation(ctx, tx, workspaceID, articleID, locale)
	})
	return item, kbError("restore knowledge base translation version", err)
}

// markKBTranslationsOutdated marks the live translations of an article outdated when its title or text
// is no longer was (what it was before this transaction's write). Only edits and restores of the
// article, and an import that rewrites it, call it: a media replacement or a link rewrite changes the
// translations the same way, so they stay up to date.
func markKBTranslationsOutdated(ctx context.Context, tx pgx.Tx, workspaceID, articleID, wasTitle, wasBody string) error {
	_, err := tx.Exec(ctx, `UPDATE kb_article_translations t SET outdated=true, outdated_at=clock_timestamp()
		FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2 AND t.workspace_id=a.workspace_id AND t.article_id=a.id
			AND t.deleted_at IS NULL AND NOT t.outdated AND (a.title, a.body_md) IS DISTINCT FROM ($3::text, $4::text)`, workspaceID, articleID, wasTitle, wasBody)
	if err != nil {
		return fmt.Errorf("mark knowledge base translations outdated: %w", err)
	}
	return nil
}

// KBLanguageFilter selects the articles of one language list.
type KBLanguageFilter struct {
	Locale, State string
	Limit         int
	Cursor        string
}

// ListKBLanguage returns one page of the live articles as one language sees them, in the help center's
// order: missing (no live translation in it), outdated, or translated (any live translation).
func (s *Store) ListKBLanguage(ctx context.Context, workspaceID string, filter KBLanguageFilter) ([]domain.KBLanguageItem, string, error) {
	if err := domain.CheckLocale(filter.Locale); err != nil {
		return nil, "", err
	}
	offset := 0
	if filter.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Cursor)
		value, found := strings.CutPrefix(string(raw), "o:")
		parsed, parseErr := strconv.Atoi(value)
		if err != nil || !found || parseErr != nil || parsed < 0 {
			return nil, "", ErrBadCursor
		}
		offset = parsed
	}
	where := ``
	switch filter.State {
	case domain.KBLanguageMissing:
		where = ` AND t.article_id IS NULL`
	case domain.KBLanguageOutdated:
		where = ` AND t.outdated`
	case domain.KBLanguageTranslated:
		where = ` AND t.article_id IS NOT NULL`
	default:
		return nil, "", &domain.InvalidKBError{Message: "state is missing, outdated or translated."}
	}
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBLanguageItem, error) {
		rows, err := tx.Query(ctx, kbTreeOrder+`SELECT a.id::text,a.number,a.title,a.status,a.collection_id::text,c.name,a.updated_at,t.article_id IS NOT NULL,
				t.locale,t.title,t.slug,t.status,t.published_at,t.outdated,t.outdated_at,t.created_at,t.updated_at
			FROM kb_articles a JOIN kb_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id
			LEFT JOIN tree k ON k.id=a.collection_id
			LEFT JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=$2 AND t.deleted_at IS NULL
			WHERE a.workspace_id=$1 AND a.deleted_at IS NULL`+where+`
			ORDER BY k.path NULLS LAST, a.position, lower(a.title), a.number LIMIT $3 OFFSET $4`, workspaceID, filter.Locale, filter.Limit+1, offset)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBLanguageItem, error) {
			var item domain.KBLanguageItem
			var has bool
			var locale, title, slug, status *string
			var published, outdatedAt, created, updated *time.Time
			var outdated *bool
			if err := row.Scan(&item.ArticleID, &item.Number, &item.Title, &item.Status, &item.CollectionID, &item.CollectionName, &item.UpdatedAt, &has,
				&locale, &title, &slug, &status, &published, &outdated, &outdatedAt, &created, &updated); err != nil {
				return item, err
			}
			item.UpdatedAt = domain.UTC(item.UpdatedAt)
			if has {
				item.Translation = &domain.KBTranslationSummary{ArticleID: item.ArticleID, Locale: *locale, Title: *title, Slug: *slug, Status: *status,
					PublishedAt: domain.UTCOptional(published), Outdated: *outdated, OutdatedAt: domain.UTCOptional(outdatedAt), CreatedAt: domain.UTC(*created), UpdatedAt: domain.UTC(*updated)}
			}
			return item, nil
		})
	})
	if err != nil {
		return nil, "", kbError("list knowledge base language", err)
	}
	next := ""
	if len(items) > filter.Limit {
		items = items[:filter.Limit]
		next = base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset+filter.Limit)))
	}
	if items == nil {
		items = []domain.KBLanguageItem{}
	}
	return items, next, nil
}

// readHelpCenterLanguages reads the primary language, the listed ones with their counts, and the
// locales translations exist in that are neither.
func readHelpCenterLanguages(ctx context.Context, tx pgx.Tx, workspaceID string) (domain.HelpCenterLanguages, error) {
	result := domain.HelpCenterLanguages{Languages: []domain.HelpCenterLanguage{}, Unlisted: []string{}}
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1), 'en')`, workspaceID).Scan(&result.Primary); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `WITH live AS (SELECT count(*) AS n FROM kb_articles WHERE workspace_id=$1 AND deleted_at IS NULL),
			counts AS (SELECT t.locale, count(*) FILTER (WHERE t.status='published') AS published, count(*) FILTER (WHERE t.status='draft') AS drafts,
				count(*) FILTER (WHERE t.outdated) AS outdated, count(*) AS translated
				FROM kb_article_translations t JOIN kb_articles a ON a.workspace_id=t.workspace_id AND a.id=t.article_id AND a.deleted_at IS NULL
				WHERE t.workspace_id=$1 AND t.deleted_at IS NULL GROUP BY t.locale)
		SELECT l.locale, l.visible, l.position, COALESCE(c.published,0), COALESCE(c.drafts,0), COALESCE(c.outdated,0), (SELECT n FROM live) - COALESCE(c.translated,0)
		FROM help_center_languages l LEFT JOIN counts c ON c.locale=l.locale
		WHERE l.workspace_id=$1 AND l.locale <> $2 ORDER BY l.position, l.locale`, workspaceID, result.Primary)
	if err != nil {
		return result, err
	}
	if result.Languages, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.HelpCenterLanguage, error) {
		var item domain.HelpCenterLanguage
		err := row.Scan(&item.Locale, &item.Visible, &item.Position, &item.Published, &item.Drafts, &item.Outdated, &item.Missing)
		return item, err
	}); err != nil {
		return result, err
	}
	if result.Languages == nil {
		result.Languages = []domain.HelpCenterLanguage{}
	}
	rows, err = tx.Query(ctx, `SELECT locale FROM kb_article_translations WHERE workspace_id=$1
			UNION SELECT locale FROM kb_collection_translations WHERE workspace_id=$1
		EXCEPT SELECT locale FROM help_center_languages WHERE workspace_id=$1 AND locale <> $2 ORDER BY 1`, workspaceID, result.Primary)
	if err != nil {
		return result, err
	}
	unlisted, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return result, err
	}
	if unlisted != nil {
		result.Unlisted = unlisted
	}
	return result, nil
}

// GetHelpCenterLanguages returns the help center's languages.
func (s *Store) GetHelpCenterLanguages(ctx context.Context, workspaceID string) (domain.HelpCenterLanguages, error) {
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterLanguages, error) {
		return readHelpCenterLanguages(ctx, tx, workspaceID)
	})
	if err != nil {
		return result, fmt.Errorf("read help center languages: %w", err)
	}
	return result, nil
}

// HelpCenterLanguagesInUseError names the languages a write would drop that still hold translations.
type HelpCenterLanguagesInUseError struct{ Locales []string }

func (e *HelpCenterLanguagesInUseError) Error() string {
	return "languages still hold translations: " + strings.Join(e.Locales, ", ")
}

// SetHelpCenterLanguages makes languages, in their order, the help center's languages besides the
// primary one. A language left out that still holds a translation or a collection name (in the trash
// too) is refused, so no translation is ever left without its language by this call.
func (s *Store) SetHelpCenterLanguages(ctx context.Context, workspaceID string, languages []domain.HelpCenterLanguageInput) (domain.HelpCenterLanguages, error) {
	if len(languages) > domain.MaxHelpCenterLanguages {
		return domain.HelpCenterLanguages{}, &domain.InvalidKBError{Message: fmt.Sprintf("A help center has at most %d languages besides its primary one.", domain.MaxHelpCenterLanguages)}
	}
	locales := make([]string, 0, len(languages))
	visible := make([]bool, 0, len(languages))
	for _, language := range languages {
		if err := domain.CheckLocale(language.Locale); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		for _, seen := range locales {
			if strings.EqualFold(seen, language.Locale) {
				return domain.HelpCenterLanguages{}, &domain.InvalidKBError{Message: "Each language is listed once; " + language.Locale + " is listed twice."}
			}
		}
		locales, visible = append(locales, language.Locale), append(visible, language.Visible)
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterLanguages, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		var primary string
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1), 'en')`, workspaceID).Scan(&primary); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if slices.ContainsFunc(locales, func(locale string) bool { return strings.EqualFold(locale, primary) }) {
			return domain.HelpCenterLanguages{}, &domain.InvalidKBError{Message: primary + " is the primary language of the help center (the articles' own language); list only the other languages."}
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT l.locale FROM help_center_languages l WHERE l.workspace_id=$1 AND l.locale <> ALL($2::text[]) AND l.locale <> $3
			AND (EXISTS (SELECT 1 FROM kb_article_translations t WHERE t.workspace_id=l.workspace_id AND t.locale=l.locale)
				OR EXISTS (SELECT 1 FROM kb_collection_translations t WHERE t.workspace_id=l.workspace_id AND t.locale=l.locale)) ORDER BY 1`, workspaceID, locales, primary)
		if err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		inUse, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if len(inUse) > 0 {
			return domain.HelpCenterLanguages{}, &HelpCenterLanguagesInUseError{Locales: inUse}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM help_center_languages WHERE workspace_id=$1 AND locale <> ALL($2::text[])`, workspaceID, locales); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO help_center_languages(workspace_id,locale,visible,position)
			SELECT $1, x.locale, x.visible, x.position FROM unnest($2::text[], $3::boolean[]) WITH ORDINALITY AS x(locale, visible, position)
			ON CONFLICT (workspace_id, locale) DO UPDATE SET visible=EXCLUDED.visible, position=EXCLUDED.position,
				updated_at=CASE WHEN (help_center_languages.visible, help_center_languages.position) IS DISTINCT FROM (EXCLUDED.visible, EXCLUDED.position)
					THEN clock_timestamp() ELSE help_center_languages.updated_at END`, workspaceID, locales, visible); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return domain.HelpCenterLanguages{}, err
		}
		return readHelpCenterLanguages(ctx, tx, workspaceID)
	})
	if err != nil {
		var inUse *HelpCenterLanguagesInUseError
		if errors.As(err, &inUse) {
			return result, err
		}
		return result, kbError("set help center languages", err)
	}
	return result, nil
}

// HelpCenterLanguagesShownChange tells whether setting languages would show a language customers do
// not see now, or hide one they do (a change to the public help center).
func (s *Store) HelpCenterLanguagesShownChange(ctx context.Context, workspaceID string, languages []domain.HelpCenterLanguageInput) (bool, error) {
	shown := []string{}
	for _, language := range languages {
		if language.Visible {
			shown = append(shown, language.Locale)
		}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		rows, err := tx.Query(ctx, `SELECT locale FROM help_center_languages WHERE workspace_id=$1 AND visible ORDER BY 1`, workspaceID)
		if err != nil {
			return false, err
		}
		now, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return false, err
		}
		slices.Sort(shown)
		return !slices.Equal(now, shown), nil
	})
}

// PutKBCollectionTranslation sets a live collection's name and description in a listed language.
func (s *Store) PutKBCollectionTranslation(ctx context.Context, workspaceID, collectionID, locale, name, description string) (domain.KBCollection, error) {
	if !savedReplyID.MatchString(collectionID) {
		return domain.KBCollection{}, domain.ErrKBCollectionNotFound
	}
	if err := domain.CheckLocale(locale); err != nil {
		return domain.KBCollection{}, err
	}
	name, err := domain.CleanKBText("name", name, domain.MaxKBCollectionName)
	if err != nil {
		return domain.KBCollection{}, err
	}
	if err := domain.CheckKBDescription(description); err != nil {
		return domain.KBCollection{}, err
	}
	collection, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBCollection, error) {
		if err := lockLiveKBCollection(ctx, tx, workspaceID, collectionID); err != nil {
			return domain.KBCollection{}, err
		}
		if err := checkKBLanguage(ctx, tx, workspaceID, locale); err != nil {
			return domain.KBCollection{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO kb_collection_translations(workspace_id,collection_id,locale,name,description,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,clock_timestamp(),clock_timestamp())
			ON CONFLICT (workspace_id,collection_id,locale) DO UPDATE SET name=EXCLUDED.name, description=EXCLUDED.description, source_meta=NULL, updated_at=clock_timestamp()`,
			workspaceID, collectionID, locale, name, description); err != nil {
			return domain.KBCollection{}, err
		}
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return domain.KBCollection{}, err
		}
		return readKBCollection(ctx, tx, workspaceID, collectionID)
	})
	return collection, kbCollectionError("translate", err)
}

// DeleteKBCollectionTranslation removes a collection's name in one language: the help center shows
// the primary name there again. Setting it again is the way back; an import rerun writes the name it
// brought in again.
func (s *Store) DeleteKBCollectionTranslation(ctx context.Context, workspaceID, collectionID, locale string) (domain.KBCollection, error) {
	if !savedReplyID.MatchString(collectionID) {
		return domain.KBCollection{}, domain.ErrKBCollectionNotFound
	}
	if domain.CheckLocale(locale) != nil {
		return domain.KBCollection{}, domain.ErrKBTranslationNotFound
	}
	collection, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBCollection, error) {
		if err := lockLiveKBCollection(ctx, tx, workspaceID, collectionID); err != nil {
			return domain.KBCollection{}, err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM kb_collection_translations WHERE workspace_id=$1 AND collection_id=$2 AND locale=$3`, workspaceID, collectionID, locale)
		if err != nil {
			return domain.KBCollection{}, err
		}
		if tag.RowsAffected() == 0 {
			return domain.KBCollection{}, domain.ErrKBTranslationNotFound
		}
		if err := bumpKB(ctx, tx, workspaceID); err != nil {
			return domain.KBCollection{}, err
		}
		return readKBCollection(ctx, tx, workspaceID, collectionID)
	})
	return collection, kbCollectionError("remove the translation of", err)
}

func lockLiveKBCollection(ctx context.Context, tx pgx.Tx, workspaceID, collectionID string) error {
	var live bool
	err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM kb_collections WHERE workspace_id=$1 AND id=$2 FOR NO KEY UPDATE`, workspaceID, collectionID).Scan(&live)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
		return domain.ErrKBCollectionNotFound
	}
	return err
}

// ShownHelpCenterLanguages returns the languages besides the primary one that customers see.
func (s *Store) ShownHelpCenterLanguages(ctx context.Context, workspaceID string) (map[string]bool, error) {
	shown, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		rows, err := tx.Query(ctx, `SELECT locale FROM help_center_languages WHERE workspace_id=$1 AND visible
			AND locale <> COALESCE((SELECT locale FROM help_centers WHERE workspace_id=$1), 'en')`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	})
	if err != nil {
		return nil, fmt.Errorf("read shown help center languages: %w", err)
	}
	out := make(map[string]bool, len(shown))
	for _, locale := range shown {
		out[locale] = true
	}
	return out, nil
}
