package store

import (
	"context"
	"errors"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// HelpCenterPreview is what a preview of the help center shows: the site with the candidate theme, its
// live collections and, for the home page, the first articles of each top collection; for an article
// page, the published article with the lowest number (nil when nothing is published) and the first
// articles of its collection.
type HelpCenterPreview struct {
	Site        domain.HelpCenterSite
	Collections []domain.PublicKBCollection
	Articles    []domain.PublicKBArticle
	Article     *domain.PublicKBArticle
}

// HelpCenterPreview reads everything a preview of the home page (article false) or of an article page
// needs, whether the help center is on or off, in one transaction whose statements go out together.
// homeArticles and neighbours bound the article lists as the public pages do. The logo and favicon
// are checked like a save (checkThemeMedia): one that is not an image of this workspace's library, or
// is in its trash without being the one the field already names, is a domain.ThemeError
// media_not_found; one accepted in the trash is left out, as on the public pages. It changes nothing.
func (s *Store) HelpCenterPreview(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme, article bool, homeArticles, neighbours int) (HelpCenterPreview, error) {
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (HelpCenterPreview, error) {
		got := HelpCenterPreview{Site: domain.HelpCenterSite{WorkspaceID: workspaceID, Theme: theme}}
		site := &got.Site
		var logo, icon previewImage
		var logoWidth, logoHeight *int
		var media error
		batch := &pgx.Batch{}
		batch.Queue(`SELECT w.slug, w.name, COALESCE(h.title, ''), COALESCE(h.locale, 'en'), COALESCE(h.content_version, 0),
				h.logo_media_id::text, logo.kind, logo.deleted_at IS NOT NULL, logo.width, logo.height,
				h.favicon_media_id::text, icon.kind, icon.deleted_at IS NOT NULL
			FROM workspaces w LEFT JOIN help_centers h ON h.workspace_id=w.id
			LEFT JOIN kb_media logo ON logo.workspace_id=w.id AND logo.id=$2::uuid
			LEFT JOIN kb_media icon ON icon.workspace_id=w.id AND icon.id=$3::uuid
			WHERE w.id=$1`, workspaceID, theme.LogoMediaID, theme.FaviconMediaID).QueryRow(func(row pgx.Row) error {
			err := row.Scan(&site.Slug, &site.Name, &site.Title, &site.Locale, &site.ContentVersion,
				&logo.stored, &logo.kind, &logo.trashed, &logoWidth, &logoHeight, &icon.stored, &icon.kind, &icon.trashed)
			if err != nil {
				return err
			}
			fields := map[string]string{}
			site.Theme.LogoMediaID = logo.shown("logo_media_id", theme.LogoMediaID, fields)
			site.Theme.FaviconMediaID = icon.shown("favicon_media_id", theme.FaviconMediaID, fields)
			if len(fields) > 0 {
				media = &domain.ThemeError{Code: "media_not_found", Fields: fields}
			}
			if site.Theme.LogoMediaID != nil && logoWidth != nil && logoHeight != nil {
				site.LogoWidth, site.LogoHeight = *logoWidth, *logoHeight
			}
			return nil
		})
		batch.Queue(helpCenterCollectionsSQL, workspaceID).Query(func(rows pgx.Rows) error {
			var err error
			got.Collections, err = pgx.CollectRows(rows, scanPublicKBCollection)
			return err
		})
		collectArticles := func(rows pgx.Rows) error {
			var err error
			got.Articles, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PublicKBArticle, error) { return scanPublicKBArticle(row) })
			return err
		}
		if !article {
			// The top collections' first articles; one with nothing published in or below it is hidden
			// by the caller, and has no published article of its own either.
			batch.Queue(helpCenterArticlesSQL(`ARRAY(SELECT c.id FROM kb_collections c WHERE c.workspace_id=$1 AND c.parent_id IS NULL AND c.deleted_at IS NULL)`),
				workspaceID, 0, homeArticles).Query(collectArticles)
		} else {
			const first = `(SELECT min(a.number) FROM kb_articles a WHERE ` + publishedKBArticle + `)`
			batch.Queue(`SELECT `+publicKBArticleColumns+`, a.body_md FROM kb_articles a WHERE `+publishedKBArticle+` AND a.number=`+first, workspaceID).
				QueryRow(func(row pgx.Row) error {
					var body string
					found, err := scanPublicKBArticle(row, &body)
					if errors.Is(err, pgx.ErrNoRows) {
						return nil
					}
					found.BodyMD = body
					got.Article = &found
					return err
				})
			batch.Queue(articleVideosSQL(`(SELECT a.id FROM kb_articles a WHERE `+publishedKBArticle+` AND a.number=`+first+`)`), workspaceID).
				Query(func(rows pgx.Rows) error {
					videos, err := pgx.CollectRows(rows, scanPublicKBVideo)
					if got.Article != nil {
						got.Article.Videos = videos
					}
					return err
				})
			batch.Queue(helpCenterArticlesSQL(`ARRAY(SELECT a.collection_id FROM kb_articles a WHERE `+publishedKBArticle+` AND a.number=`+first+`)`),
				workspaceID, 0, neighbours).Query(collectArticles)
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return HelpCenterPreview{}, err
		}
		return got, media
	})
	if err != nil {
		return HelpCenterPreview{}, themeStoreError("help center preview", err)
	}
	if got.Article != nil && !strings.Contains(got.Article.BodyMD, domain.KBMediaPath) {
		got.Article.Videos = nil
	}
	return got, nil
}

// previewImage is what the preview's query reads of the logo or favicon the candidate names: the id
// the help center stores for that field, and the named item's kind (nil when it is not in this
// workspace's library) and whether it is in the trash.
type previewImage struct {
	stored  *string
	kind    *string
	trashed *bool
}

// shown applies checkThemeMedia's rule to the item named by id and returns the id the page shows: nil
// for none, or for an item accepted in the trash. A refusal goes into fields under field.
func (p previewImage) shown(field string, id *string, fields map[string]string) *string {
	if id == nil {
		return nil
	}
	found := p.kind != nil
	if problem := themeMediaProblem(found, found && *p.kind == domain.KBMediaImage, found && *p.trashed, p.stored, *id); problem != "" {
		fields[field] = problem
		return nil
	}
	if *p.trashed {
		return nil
	}
	return id
}
