package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The public help center reads only published articles outside the trash. Every query here filters
// on both, so a draft never reaches a page, a count, a search or an image.
const publishedKBArticle = `a.workspace_id=$1 AND a.status='published' AND a.deleted_at IS NULL`

// OpenHelpCenter resolves the workspace of a help center slug and reports whether its help center may
// be shown: it exists, it is on and, where plan limits apply, the workspace is not locked. The three
// refusals are one false, so a caller cannot answer them differently.
func (s *Store) OpenHelpCenter(ctx context.Context, slug string) (domain.HelpCenterSite, bool, error) {
	workspaceID, found, err := s.WorkspaceForMailSlug(ctx, slug)
	if err != nil || !found {
		return domain.HelpCenterSite{}, false, err
	}
	return s.OpenHelpCenterWorkspace(ctx, workspaceID)
}

// OpenHelpCenterWorkspace is OpenHelpCenter for a workspace already found, such as the one a custom
// domain belongs to. CustomHost is the workspace's active custom domain, read in the same query, and "" during
// the trial: the help center serves on its custom domain only once the workspace is paid.
func (s *Store) OpenHelpCenterWorkspace(ctx context.Context, workspaceID string) (domain.HelpCenterSite, bool, error) {
	type state struct {
		site    domain.HelpCenterSite
		enabled bool
		locked  bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (state, error) {
		got := state{site: domain.HelpCenterSite{WorkspaceID: workspaceID}}
		site, theme := &got.site, &got.site.Theme
		colors := &theme.Colors
		var logoWidth, logoHeight *int
		// The logo and favicon count only while they are images of the library outside its trash.
		err := tx.QueryRow(ctx, `SELECT w.slug, w.name, COALESCE(h.enabled, false), COALESCE(h.title, ''), COALESCE(h.locale, 'en'), COALESCE(h.content_version, 0), COALESCE(h.portal_enabled, false),
				logo.id::text, logo.width, logo.height, icon.id::text, COALESCE(h.logo_alt, ''), h.linkback_url, h.contact_url, h.contact_phone,
				h.color_primary, h.color_header, h.color_header_text, h.color_background, h.color_text, h.color_link, h.color_footer, h.body_font, h.heading_font,
				COALESCE(h.custom_css, ''), COALESCE(d.hostname, ''), COALESCE(h.google_site_verification, ''), COALESCE(h.bing_site_verification, '')
			FROM workspaces w LEFT JOIN help_centers h ON h.workspace_id=w.id
			LEFT JOIN help_center_domains d ON d.workspace_id=w.id AND d.status='active'
			LEFT JOIN kb_media logo ON logo.workspace_id=w.id AND logo.id=h.logo_media_id AND logo.kind='image' AND logo.deleted_at IS NULL
			LEFT JOIN kb_media icon ON icon.workspace_id=w.id AND icon.id=h.favicon_media_id AND icon.kind='image' AND icon.deleted_at IS NULL
			WHERE w.id=$1`, workspaceID).
			Scan(&site.Slug, &site.Name, &got.enabled, &site.Title, &site.Locale, &site.ContentVersion, &site.Portal,
				&theme.LogoMediaID, &logoWidth, &logoHeight, &theme.FaviconMediaID, &theme.LogoAlt, &theme.LinkbackURL, &theme.ContactURL, &theme.ContactPhone,
				&colors.Primary, &colors.Header, &colors.HeaderText, &colors.Background, &colors.Text, &colors.Link, &colors.Footer, &theme.BodyFont, &theme.HeadingFont,
				&theme.CustomCSS, &site.CustomHost, &site.SearchConsole.Google, &site.SearchConsole.Bing)
		if logoWidth != nil && logoHeight != nil {
			site.LogoWidth, site.LogoHeight = *logoWidth, *logoHeight
		}
		if err != nil || !got.enabled {
			return got, err
		}
		rows, err := tx.Query(ctx, `SELECT locale FROM help_center_languages WHERE workspace_id=$1 AND visible AND locale <> $2 ORDER BY position, locale`, workspaceID, site.Locale)
		if err != nil {
			return got, err
		}
		if site.Languages, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil || !s.limits {
			return got, err
		}
		access, err := workspaceAccess(ctx, tx, workspaceID, s.now())
		got.locked, got.site.Trial = access.Locked, access.Plan == domain.PlanTrial
		// A trial workspace may verify its custom domain, but its noindex pages never serve there: the
		// help center stays on its own address until the workspace is paid.
		if got.site.Trial {
			got.site.CustomHost = ""
		}
		return got, err
	})
	if err != nil {
		return domain.HelpCenterSite{}, false, fmt.Errorf("open help center: %w", err)
	}
	if !got.enabled || got.locked {
		return domain.HelpCenterSite{}, false, nil
	}
	return got.site, true, nil
}

// listedKBArticles is a FROM item a over the articles of workspace $1, once for each collection it
// is listed in: listed is that collection, extra false for its home and true for another one. The extra
// branch reads kb_article_collections by its index, so it costs nothing while no article has one.
const listedKBArticles = `(SELECT a.workspace_id, a.id, a.number, a.slug, a.title, a.position, a.status, a.deleted_at, a.published_at, a.updated_at,
			a.collection_id AS listed, false AS extra FROM kb_articles a
		UNION ALL SELECT a.workspace_id, a.id, a.number, a.slug, a.title, a.position, a.status, a.deleted_at, a.published_at, a.updated_at,
			x.collection_id, true FROM kb_article_collections x JOIN kb_articles a ON a.workspace_id=x.workspace_id AND a.id=x.article_id
		WHERE x.workspace_id=$1 AND x.collection_id<>a.collection_id) a`

// helpCenterCollectionsSQL lists the live collections of workspace $1 with their name and description
// in language $2 ("" the primary one; a missing name falls back to it), the published articles each
// lists itself (its own and those listed in it too), and how many of those are published in each shown
// language, by position and name.
const helpCenterCollectionsSQL = `WITH shown AS (SELECT a.listed AS collection_id, t.locale, count(*) AS n
		FROM ` + listedKBArticles + ` JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.status='published' AND t.deleted_at IS NULL
		JOIN help_center_languages hl ON hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible
		WHERE ` + publishedKBArticle + ` GROUP BY a.listed, t.locale),
	byc AS (SELECT collection_id, jsonb_object_agg(locale, n) AS counts FROM shown GROUP BY collection_id)
	SELECT c.id::text, COALESCE(c.parent_id::text, ''), COALESCE(ct.name, c.name), CASE WHEN ct.collection_id IS NULL THEN c.description ELSE ct.description END,
		c.slug, COALESCE(n.published, 0), COALESCE(byc.counts, '{}'::jsonb)
	FROM kb_collections c
	LEFT JOIN (SELECT a.listed AS collection_id, count(*) AS published FROM ` + listedKBArticles + ` WHERE ` + publishedKBArticle + ` GROUP BY a.listed) n
		ON n.collection_id=c.id
	LEFT JOIN byc ON byc.collection_id=c.id
	LEFT JOIN kb_collection_translations ct ON ct.workspace_id=c.workspace_id AND ct.collection_id=c.id AND ct.locale=$2 AND $2 <> ''
	WHERE c.workspace_id=$1 AND c.deleted_at IS NULL
	ORDER BY c.position, lower(c.name), c.id`

func scanPublicKBCollection(locale string) func(row pgx.CollectableRow) (domain.PublicKBCollection, error) {
	return func(row pgx.CollectableRow) (domain.PublicKBCollection, error) {
		var collection domain.PublicKBCollection
		var primary int
		counts := map[string]int{}
		err := row.Scan(&collection.ID, &collection.ParentID, &collection.Name, &collection.Description, &collection.Slug, &primary, &counts)
		counts[""] = primary
		collection.PublishedIn, collection.Published = counts, counts[locale]
		return collection, err
	}
}

// HelpCenterCollections returns the live collections of the workspace with the number of published
// articles each holds itself, by position and name. The caller hides the ones with nothing published
// below them, so a collection of drafts never shows its name.
func (s *Store) HelpCenterCollections(ctx context.Context, workspaceID string) ([]domain.PublicKBCollection, error) {
	return s.HelpCenterCollectionsIn(ctx, workspaceID, "")
}

// HelpCenterCollectionsIn is HelpCenterCollections as the pages of language locale show it: its names,
// and Published counting the articles published in it ("" is the primary language).
func (s *Store) HelpCenterCollectionsIn(ctx context.Context, workspaceID, locale string) ([]domain.PublicKBCollection, error) {
	collections, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBCollection, error) {
		rows, err := tx.Query(ctx, helpCenterCollectionsSQL, workspaceID, locale)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, scanPublicKBCollection(locale))
	})
	if err != nil {
		return nil, fmt.Errorf("list help center collections: %w", err)
	}
	return collections, nil
}

func scanPublicKBArticle(row pgx.Row, extra ...any) (domain.PublicKBArticle, error) {
	var article domain.PublicKBArticle
	err := row.Scan(append([]any{&article.Number, &article.Slug, &article.Title, &article.CollectionID, &article.PublishedAt, &article.UpdatedAt}, extra...)...)
	article.PublishedAt, article.UpdatedAt = domain.UTCOptional(article.PublishedAt), domain.UTC(article.UpdatedAt)
	return article, err
}

const publicKBArticleColumns = `a.number, a.slug, a.title, a.collection_id::text, a.published_at, a.updated_at`

// shownTranslation joins t, the published translation in language $L shown to customers, of articles a.
func shownTranslation(locale string) string {
	return ` JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=` + locale + ` AND t.status='published' AND t.deleted_at IS NULL
		JOIN help_center_languages hl ON hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible`
}

// publicKBTranslationColumns read a translation t of article a as publicKBArticleColumns read the article.
const publicKBTranslationColumns = `a.number, t.slug, t.title, a.collection_id::text, t.published_at, t.updated_at`

// helpCenterArticlesSQL lists, for each collection of workspace $1 that collections (an SQL array
// of ids) names, the published articles it lists in their order: its own by position, then title; then
// those listed in it from another home, by title; skipping the first $2 and keeping at most $3. The
// collection id read is the one listed in, so a list groups by it; the article page reads the home.
func helpCenterArticlesSQL(collections string) string {
	return `SELECT a.number, a.slug, a.title, a.listed::text, a.published_at, a.updated_at FROM (
			SELECT a.*, row_number() OVER (PARTITION BY a.listed ORDER BY a.extra, CASE WHEN a.extra THEN 0 ELSE a.position END, lower(a.title), a.number) AS place
			FROM ` + listedKBArticles + ` WHERE ` + publishedKBArticle + ` AND a.listed = ANY(` + collections + `)) a
		WHERE a.place > $2 AND a.place <= $2 + $3 ORDER BY a.listed, a.place`
}

// helpCenterTranslatedArticlesSQL is helpCenterArticlesSQL for the translations in language $5: only
// the articles published in it count, in the same order.
const helpCenterTranslatedArticlesSQL = `SELECT x.number, x.tslug, x.ttitle, x.listed::text, x.tpublished, x.tupdated FROM (
		SELECT a.number, a.listed, t.slug AS tslug, t.title AS ttitle, t.published_at AS tpublished, t.updated_at AS tupdated,
			row_number() OVER (PARTITION BY a.listed ORDER BY a.extra, CASE WHEN a.extra THEN 0 ELSE a.position END, lower(t.title), a.number) AS place
		FROM ` + listedKBArticles + `
		JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=$5 AND t.status='published' AND t.deleted_at IS NULL
		JOIN help_center_languages hl ON hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible
		WHERE ` + publishedKBArticle + ` AND a.listed = ANY($4::uuid[])) x
	WHERE x.place > $2 AND x.place <= $2 + $3 ORDER BY x.listed, x.place`

// HelpCenterArticles returns, for each of the collections, its published articles in order, skipping
// the first offset and keeping at most limit; the text is left out.
func (s *Store) HelpCenterArticles(ctx context.Context, workspaceID string, collectionIDs []string, offset, limit int) ([]domain.PublicKBArticle, error) {
	return s.HelpCenterArticlesIn(ctx, workspaceID, "", collectionIDs, offset, limit)
}

// HelpCenterArticlesIn is HelpCenterArticles in language locale: the articles published in it, with
// their translated title and slug ("" is the primary language).
func (s *Store) HelpCenterArticlesIn(ctx context.Context, workspaceID, locale string, collectionIDs []string, offset, limit int) ([]domain.PublicKBArticle, error) {
	for _, id := range collectionIDs {
		if !savedReplyID.MatchString(id) {
			return nil, domain.ErrKBCollectionNotFound
		}
	}
	articles, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBArticle, error) {
		var rows pgx.Rows
		var err error
		if locale == "" {
			rows, err = tx.Query(ctx, helpCenterArticlesSQL(`$4::uuid[]`), workspaceID, offset, limit, collectionIDs)
		} else {
			rows, err = tx.Query(ctx, helpCenterTranslatedArticlesSQL, workspaceID, offset, limit, collectionIDs, locale)
		}
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) {
			article, err := scanPublicKBArticle(row)
			article.Locale = locale
			return article, err
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list help center articles: %w", err)
	}
	return articles, nil
}

// articleVideosSQL lists the live library videos the current text of an article of workspace $1 links;
// article is an SQL expression of the article's id.
func articleVideosSQL(article string) string {
	return `SELECT m.id::text, COALESCE(m.width,0), COALESCE(m.height,0), COALESCE(right(m.poster_storage_key,8),'')
		FROM kb_article_media l JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id
		WHERE l.workspace_id=$1 AND l.article_id=` + article + ` AND l.current AND m.kind='video' AND m.deleted_at IS NULL
		ORDER BY m.id`
}

func scanPublicKBVideo(row pgx.CollectableRow) (domain.PublicKBVideo, error) {
	var video domain.PublicKBVideo
	err := row.Scan(&video.ID, &video.Width, &video.Height, &video.PosterVersion)
	return video, err
}

// HelpCenterArticle returns the published article with number and its text, or
// domain.ErrKBArticleNotFound for a draft, an article in the trash or no article at all.
func (s *Store) HelpCenterArticle(ctx context.Context, workspaceID string, number int64) (domain.PublicKBArticle, error) {
	return s.HelpCenterArticleIn(ctx, workspaceID, number, "")
}

// HelpCenterArticleIn is HelpCenterArticle in language locale ("" the primary one): its published
// translation shown to customers, or domain.ErrKBArticleNotFound. Alternates name the article's slug
// in every language it is published and shown in.
func (s *Store) HelpCenterArticleIn(ctx context.Context, workspaceID string, number int64, locale string) (domain.PublicKBArticle, error) {
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.PublicKBArticle, error) {
		var body string
		var id string
		var seoTitle, seoDescription string
		var article domain.PublicKBArticle
		var err error
		if locale == "" {
			article, err = scanPublicKBArticle(tx.QueryRow(ctx, `SELECT `+publicKBArticleColumns+`, a.body_md, a.id::text, a.seo_title, a.seo_description FROM kb_articles a
				WHERE `+publishedKBArticle+` AND a.number=$2`, workspaceID, number), &body, &id, &seoTitle, &seoDescription)
		} else {
			article, err = scanPublicKBArticle(tx.QueryRow(ctx, `SELECT `+publicKBTranslationColumns+`, t.body_md, a.id::text, t.seo_title, t.seo_description FROM kb_articles a`+
				shownTranslation(`$3`)+` WHERE `+publishedKBArticle+` AND a.number=$2`, workspaceID, number, locale), &body, &id, &seoTitle, &seoDescription)
		}
		article.SEOTitle, article.SEODescription, article.Locale = seoTitle, seoDescription, locale
		article.BodyMD = body
		if err != nil {
			return article, err
		}
		article.Alternates = map[string]string{}
		rows, err := tx.Query(ctx, `SELECT '', a.slug FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2
			UNION ALL SELECT t.locale, t.slug FROM kb_articles a`+shownTranslation(`t.locale`)+` WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, id)
		if err != nil {
			return article, err
		}
		var alternate, slug string
		if _, err := pgx.ForEachRow(rows, []any{&alternate, &slug}, func() error {
			article.Alternates[alternate] = slug
			return nil
		}); err != nil {
			return article, err
		}
		if !strings.Contains(body, domain.KBMediaPath) {
			return article, nil
		}
		// The videos its current text links, in the same transaction: the page shows their poster and size.
		rows, err = tx.Query(ctx, articleVideosSQL(`$2::uuid`), workspaceID, id)
		if err != nil {
			return article, err
		}
		article.Videos, err = pgx.CollectRows(rows, scanPublicKBVideo)
		return article, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PublicKBArticle{}, domain.ErrKBArticleNotFound
	}
	if err != nil {
		return domain.PublicKBArticle{}, fmt.Errorf("get help center article: %w", err)
	}
	return article, nil
}

// shownTextNames is the media rule of the help center on rows a (articles) for path $3: the current
// text of a published article outside the trash names it, or the text of one of its published
// translations in a language customers see.
const shownTextNames = publishedKBArticle + ` AND (strpos(a.body_md, $3) > 0 OR EXISTS (SELECT 1 FROM kb_article_translations t
		JOIN help_center_languages hl ON hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible
		WHERE t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.status='published' AND t.deleted_at IS NULL AND strpos(t.body_md, $3) > 0))`

// HelpCenterVideo returns a video the help center may play, by the rule of HelpCenterImage: the
// current text of a published article outside the trash links it by its media path, and it is not in
// the library's trash.
func (s *Store) HelpCenterVideo(ctx context.Context, workspaceID, id string) (StoredKBMedia, error) {
	if !savedReplyID.MatchString(id) || id != strings.ToLower(id) {
		return StoredKBMedia{}, domain.ErrKBMediaNotFound
	}
	video, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBMedia, error) {
		return scanKBMedia(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2
			AND m.kind='video' AND m.deleted_at IS NULL AND EXISTS (
			SELECT 1 FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND `+shownTextNames+`)`,
			workspaceID, id, domain.KBMediaPath+id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBMedia{}, domain.ErrKBMediaNotFound
	}
	if err != nil {
		return StoredKBMedia{}, fmt.Errorf("get help center video: %w", err)
	}
	return video, nil
}

// helpCenterExcerptChars is how much of an article's text a search result reads for its excerpt.
const helpCenterExcerptChars = 600

// SearchHelpCenter finds published articles the way ListKBArticles searches (every word as a word or
// the start of one, or the text inside the title), best match first, at most limit. Excerpt holds the
// start of each text; only the articles returned are read for it, never the whole knowledge base.
func (s *Store) SearchHelpCenter(ctx context.Context, workspaceID, query string, limit int) ([]domain.PublicKBArticle, error) {
	return s.SearchHelpCenterIn(ctx, workspaceID, "", query, limit)
}

// SearchHelpCenterIn is SearchHelpCenter in language locale: it searches the published translations
// in it ("" is the primary language), their own words, never the primary text.
func (s *Store) SearchHelpCenterIn(ctx context.Context, workspaceID, locale, query string, limit int) ([]domain.PublicKBArticle, error) {
	sql := `WITH hits AS (
				SELECT a.id, COALESCE(ts_rank(a.search, t.q), 0) AS rank, a.updated_at
				FROM kb_articles a CROSS JOIN LATERAL (SELECT ` + kbQuery + ` AS q) t
				WHERE ` + publishedKBArticle + ` AND (a.search @@ t.q OR a.title ILIKE $3)
				ORDER BY rank DESC, a.updated_at DESC, a.id DESC LIMIT $4)
			SELECT ` + publicKBArticleColumns + `, left(a.body_md, $5) FROM hits h JOIN kb_articles a ON a.workspace_id=$1 AND a.id=h.id
			ORDER BY h.rank DESC, h.updated_at DESC, h.id DESC`
	args := []any{workspaceID, query, "%" + likePattern.Replace(query) + "%", limit, helpCenterExcerptChars}
	if locale != "" {
		sql = `WITH hits AS (
				SELECT a.id, COALESCE(ts_rank(t.search, w.q), 0) AS rank, t.updated_at
				FROM kb_articles a` + shownTranslation(`$6`) + ` CROSS JOIN LATERAL (SELECT ` + kbQuery + ` AS q) w
				WHERE ` + publishedKBArticle + ` AND (t.search @@ w.q OR t.title ILIKE $3)
				ORDER BY rank DESC, t.updated_at DESC, a.id DESC LIMIT $4)
			SELECT ` + publicKBTranslationColumns + `, left(t.body_md, $5) FROM hits h JOIN kb_articles a ON a.workspace_id=$1 AND a.id=h.id
				JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=$6
			ORDER BY h.rank DESC, h.updated_at DESC, h.id DESC`
		args = append(args, locale)
	}
	articles, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBArticle, error) {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) {
			var excerpt string
			article, err := scanPublicKBArticle(row, &excerpt)
			article.Excerpt, article.Locale = excerpt, locale
			return article, err
		})
	})
	if err != nil {
		return nil, fmt.Errorf("search help center: %w", err)
	}
	return articles, nil
}

// HelpCenterImage returns an image the help center may serve: one that the current text of a
// published article outside the trash links. kb_article_media alone is not enough: an item in the
// library's trash is never served, and the texts are checked for the image path itself, since a link
// marks any version of the article's text or a translation: a published translation in a language
// customers see counts (shownTextNames).
func (s *Store) HelpCenterImage(ctx context.Context, workspaceID, id string) (StoredKBImage, error) {
	if !savedReplyID.MatchString(id) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	// Texts link the id as the database prints it, in lowercase.
	id = strings.ToLower(id)
	image, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (StoredKBImage, error) {
		return scanKBImage(tx.QueryRow(ctx, `SELECT `+kbMediaColumns+` FROM kb_media m WHERE m.workspace_id=$1 AND m.id=$2
			AND m.kind='image' AND m.deleted_at IS NULL AND EXISTS (
			SELECT 1 FROM kb_article_media l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND `+shownTextNames+`)`,
			workspaceID, id, domain.KBImagePath+id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredKBImage{}, domain.ErrKBImageNotFound
	}
	if err != nil {
		return StoredKBImage{}, fmt.Errorf("get help center image: %w", err)
	}
	return image, nil
}

// ChatKBArticle is an article of the help center as its AI chat reads it: in its own language, with
// the number and version it is cited by and its collection's name.
type ChatKBArticle struct {
	ID      string
	Number  int64
	Version int
	Title   string
	Folder  string
	// Size is the body's length in bytes; Body is read only when asked for.
	Size int
	Body string
	// Locale is the language of Title and Body when they are a published translation shown to
	// customers ("" the article's own text); TextKey changes with that translation.
	Locale, TextKey string
}

// ChatKBArticles lists the articles the help center shows (publishedKBArticle) that are not kept away
// from AI, by number, in their own language. withBodies reads the texts too.
func ChatKBArticles(ctx context.Context, tx pgx.Tx, workspaceID string, withBodies bool) ([]ChatKBArticle, error) {
	return ChatKBArticlesIn(ctx, tx, workspaceID, withBodies, "")
}

// chatKBSelect reads articles a for the help chat in language $2: an article with a published
// translation in it that customers see is read in that language, with its collection's name in it;
// the others in their own ("" is the primary language).
func chatKBSelect(body string) string {
	return `SELECT a.id::text,a.number,COALESCE(v.version,1),COALESCE(t.title,a.title),COALESCE(ct.name,c.name),octet_length(COALESCE(t.body_md,a.body_md)),` + body + `,
		COALESCE(t.locale,''),COALESCE(t.updated_at::text,'')
		FROM kb_articles a JOIN kb_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id
		LEFT JOIN LATERAL (SELECT max(version) AS version FROM kb_article_versions WHERE workspace_id=a.workspace_id AND article_id=a.id) v ON true
		LEFT JOIN kb_article_translations t ON t.workspace_id=a.workspace_id AND t.article_id=a.id AND t.locale=$2 AND $2 <> '' AND t.status='published' AND t.deleted_at IS NULL
			AND EXISTS (SELECT 1 FROM help_center_languages hl WHERE hl.workspace_id=t.workspace_id AND hl.locale=t.locale AND hl.visible)
		LEFT JOIN kb_collection_translations ct ON ct.workspace_id=c.workspace_id AND ct.collection_id=c.id AND ct.locale=t.locale`
}

// ChatKBArticlesIn is ChatKBArticles as the help chat of language locale reads them (chatKBSelect).
func ChatKBArticlesIn(ctx context.Context, tx pgx.Tx, workspaceID string, withBodies bool, locale string) ([]ChatKBArticle, error) {
	body := `''`
	if withBodies {
		body = `COALESCE(t.body_md,a.body_md)`
	}
	rows, err := tx.Query(ctx, chatKBSelect(body)+` WHERE `+publishedKBArticle+` AND NOT a.ai_excluded ORDER BY a.number`, workspaceID, locale)
	if err != nil {
		return nil, fmt.Errorf("read the help center for AI chat: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChatKBArticle, error) {
		var a ChatKBArticle
		err := row.Scan(&a.ID, &a.Number, &a.Version, &a.Title, &a.Folder, &a.Size, &a.Body, &a.Locale, &a.TextKey)
		return a, err
	})
}

// ChatKBArticleIDOf is the id of the article the help center shows under a number, when ChatKBArticles
// holds it; "" otherwise.
func ChatKBArticleIDOf(ctx context.Context, tx pgx.Tx, workspaceID string, number int64) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT a.id::text FROM kb_articles a WHERE `+publishedKBArticle+` AND NOT a.ai_excluded AND a.number=$2`, workspaceID, number).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// ChatKBArticleByID reads one article of ChatKBArticles, with its body; found is false for one the
// help center does not show or that is kept away from AI.
func ChatKBArticleByID(ctx context.Context, tx pgx.Tx, workspaceID, id string) (ChatKBArticle, bool, error) {
	return ChatKBArticleByIDIn(ctx, tx, workspaceID, id, "")
}

// ChatKBArticlesByIDIn reads the articles ids in language locale (chatKBSelect) in one query, by
// id; an id the help center does not show is left out.
func ChatKBArticlesByIDIn(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, locale string) (map[string]ChatKBArticle, error) {
	found := make(map[string]ChatKBArticle, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	rows, err := tx.Query(ctx, chatKBSelect(`COALESCE(t.body_md,a.body_md)`)+` WHERE `+publishedKBArticle+` AND NOT a.ai_excluded AND a.id = ANY($3::uuid[])`, workspaceID, locale, ids)
	if err != nil {
		return nil, fmt.Errorf("read help center articles for AI chat: %w", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ChatKBArticle, error) {
		var a ChatKBArticle
		err := row.Scan(&a.ID, &a.Number, &a.Version, &a.Title, &a.Folder, &a.Size, &a.Body, &a.Locale, &a.TextKey)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("read help center articles for AI chat: %w", err)
	}
	for _, a := range list {
		found[a.ID] = a
	}
	return found, nil
}

// ChatKBArticleByIDIn is ChatKBArticleByID in language locale (chatKBSelect).
func ChatKBArticleByIDIn(ctx context.Context, tx pgx.Tx, workspaceID, id, locale string) (ChatKBArticle, bool, error) {
	var a ChatKBArticle
	err := tx.QueryRow(ctx, chatKBSelect(`COALESCE(t.body_md,a.body_md)`)+` WHERE `+publishedKBArticle+` AND NOT a.ai_excluded AND a.id::text=$3`, workspaceID, locale, id).
		Scan(&a.ID, &a.Number, &a.Version, &a.Title, &a.Folder, &a.Size, &a.Body, &a.Locale, &a.TextKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChatKBArticle{}, false, nil
	}
	return a, err == nil, err
}
