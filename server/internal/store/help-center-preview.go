package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// HelpCenterPreviewSite returns the help center of the workspace as its pages would show it with theme
// as the brand, whether the help center is on or off, and the number of the published article with the
// lowest number (0 when nothing is published). As on the public pages, a logo or favicon that is not an
// image of this workspace's library outside its trash is left out. It changes nothing.
func (s *Store) HelpCenterPreviewSite(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme) (domain.HelpCenterSite, int64, error) {
	type preview struct {
		site  domain.HelpCenterSite
		first int64
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (preview, error) {
		got := preview{site: domain.HelpCenterSite{WorkspaceID: workspaceID}}
		site := &got.site
		var logoID, iconID *string
		var logoWidth, logoHeight *int
		err := tx.QueryRow(ctx, `SELECT w.slug, w.name, COALESCE(h.title, ''), COALESCE(h.locale, 'en'), COALESCE(h.content_version, 0),
				logo.id::text, logo.width, logo.height, icon.id::text,
				COALESCE((SELECT min(a.number) FROM kb_articles a WHERE `+publishedKBArticle+`), 0)
			FROM workspaces w LEFT JOIN help_centers h ON h.workspace_id=w.id
			LEFT JOIN kb_media logo ON logo.workspace_id=w.id AND logo.id=$2::uuid AND logo.kind='image' AND logo.deleted_at IS NULL
			LEFT JOIN kb_media icon ON icon.workspace_id=w.id AND icon.id=$3::uuid AND icon.kind='image' AND icon.deleted_at IS NULL
			WHERE w.id=$1`, workspaceID, theme.LogoMediaID, theme.FaviconMediaID).
			Scan(&site.Slug, &site.Name, &site.Title, &site.Locale, &site.ContentVersion, &logoID, &logoWidth, &logoHeight, &iconID, &got.first)
		if logoWidth != nil && logoHeight != nil {
			site.LogoWidth, site.LogoHeight = *logoWidth, *logoHeight
		}
		site.Theme = theme
		site.Theme.LogoMediaID, site.Theme.FaviconMediaID = logoID, iconID
		return got, err
	})
	if err != nil {
		return domain.HelpCenterSite{}, 0, fmt.Errorf("help center preview: %w", err)
	}
	return got.site, got.first, nil
}
