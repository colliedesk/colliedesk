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
	type state struct {
		site    domain.HelpCenterSite
		enabled bool
		locked  bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (state, error) {
		got := state{site: domain.HelpCenterSite{WorkspaceID: workspaceID}}
		err := tx.QueryRow(ctx, `SELECT w.slug, w.name, COALESCE(h.enabled, false), COALESCE(h.title, ''), COALESCE(h.locale, 'en'), COALESCE(h.content_version, 0)
			FROM workspaces w LEFT JOIN help_centers h ON h.workspace_id=w.id WHERE w.id=$1`, workspaceID).
			Scan(&got.site.Slug, &got.site.Name, &got.enabled, &got.site.Title, &got.site.Locale, &got.site.ContentVersion)
		if err != nil || !got.enabled || !s.limits {
			return got, err
		}
		access, err := workspaceAccess(ctx, tx, workspaceID, s.now())
		got.locked, got.site.Trial = access.Locked, access.Plan == domain.PlanTrial
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

// HelpCenterCollections returns the live collections of the workspace with the number of published
// articles each holds itself, by position and name. The caller hides the ones with nothing published
// below them, so a collection of drafts never shows its name.
func (s *Store) HelpCenterCollections(ctx context.Context, workspaceID string) ([]domain.PublicKBCollection, error) {
	collections, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBCollection, error) {
		rows, err := tx.Query(ctx, `SELECT c.id::text, COALESCE(c.parent_id::text, ''), c.name, c.slug, c.description, COALESCE(n.published, 0)
			FROM kb_collections c
			LEFT JOIN (SELECT a.collection_id, count(*) AS published FROM kb_articles a WHERE `+publishedKBArticle+` GROUP BY a.collection_id) n
				ON n.collection_id=c.id
			WHERE c.workspace_id=$1 AND c.deleted_at IS NULL
			ORDER BY c.position, lower(c.name), c.id`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBCollection, error) {
			var collection domain.PublicKBCollection
			err := row.Scan(&collection.ID, &collection.ParentID, &collection.Name, &collection.Slug, &collection.Description, &collection.Published)
			return collection, err
		})
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

// HelpCenterArticles returns, for each of the collections, its published articles by title, skipping
// the first offset and keeping at most limit; the text is left out.
func (s *Store) HelpCenterArticles(ctx context.Context, workspaceID string, collectionIDs []string, offset, limit int) ([]domain.PublicKBArticle, error) {
	for _, id := range collectionIDs {
		if !savedReplyID.MatchString(id) {
			return nil, domain.ErrKBCollectionNotFound
		}
	}
	articles, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBArticle, error) {
		rows, err := tx.Query(ctx, `SELECT `+publicKBArticleColumns+` FROM (
				SELECT a.*, row_number() OVER (PARTITION BY a.collection_id ORDER BY lower(a.title), a.number) AS place
				FROM kb_articles a WHERE `+publishedKBArticle+` AND a.collection_id = ANY($2::uuid[])) a
			WHERE a.place > $3 AND a.place <= $3 + $4 ORDER BY a.collection_id, a.place`, workspaceID, collectionIDs, offset, limit)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) { return scanPublicKBArticle(row) })
	})
	if err != nil {
		return nil, fmt.Errorf("list help center articles: %w", err)
	}
	return articles, nil
}

// HelpCenterArticle returns the published article with number and its text, or
// domain.ErrKBArticleNotFound for a draft, an article in the trash or no article at all.
func (s *Store) HelpCenterArticle(ctx context.Context, workspaceID string, number int64) (domain.PublicKBArticle, error) {
	article, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.PublicKBArticle, error) {
		var body string
		var id string
		article, err := scanPublicKBArticle(tx.QueryRow(ctx, `SELECT `+publicKBArticleColumns+`, a.body_md, a.id::text FROM kb_articles a
			WHERE `+publishedKBArticle+` AND a.number=$2`, workspaceID, number), &body, &id)
		article.BodyMD = body
		if err != nil || !strings.Contains(body, domain.KBMediaPath) {
			return article, err
		}
		// The videos its current text links, in the same transaction: the page shows their poster and size.
		rows, err := tx.Query(ctx, `SELECT m.id::text, COALESCE(m.width,0), COALESCE(m.height,0), COALESCE(right(m.poster_storage_key,8),'')
			FROM kb_article_media l JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id
			WHERE l.workspace_id=$1 AND l.article_id=$2 AND l.current AND m.kind='video' AND m.deleted_at IS NULL
			ORDER BY m.id`, workspaceID, id)
		if err != nil {
			return article, err
		}
		article.Videos, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBVideo, error) {
			var video domain.PublicKBVideo
			err := row.Scan(&video.ID, &video.Width, &video.Height, &video.PosterVersion)
			return video, err
		})
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
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND `+publishedKBArticle+` AND strpos(a.body_md, $3) > 0)`,
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
	articles, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.PublicKBArticle, error) {
		rows, err := tx.Query(ctx, `WITH hits AS (
				SELECT a.id, COALESCE(ts_rank(a.search, t.q), 0) AS rank, a.updated_at
				FROM kb_articles a CROSS JOIN LATERAL (SELECT `+kbQuery+` AS q) t
				WHERE `+publishedKBArticle+` AND (a.search @@ t.q OR a.title ILIKE $3)
				ORDER BY rank DESC, a.updated_at DESC, a.id DESC LIMIT $4)
			SELECT `+publicKBArticleColumns+`, left(a.body_md, $5) FROM hits h JOIN kb_articles a ON a.workspace_id=$1 AND a.id=h.id
			ORDER BY h.rank DESC, h.updated_at DESC, h.id DESC`,
			workspaceID, query, "%"+likePattern.Replace(query)+"%", limit, helpCenterExcerptChars)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) {
			var excerpt string
			article, err := scanPublicKBArticle(row, &excerpt)
			article.Excerpt = excerpt
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
// library's trash is never served, and the text is checked for the image path itself, since a link
// marks the article's text or a translation and translations are not published yet.
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
			WHERE l.workspace_id=$1 AND l.media_id=$2 AND l.current AND `+publishedKBArticle+` AND strpos(a.body_md, $3) > 0)`,
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
