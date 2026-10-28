package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbMediaNamedByHelpCenter is true for a library item, on rows aliased m, that the help center shows as
// its logo or favicon. The purge never deletes one and the library never calls it unused.
const kbMediaNamedByHelpCenter = `EXISTS(SELECT 1 FROM help_centers h WHERE h.workspace_id=m.workspace_id
	AND (h.logo_media_id=m.id OR h.favicon_media_id=m.id))`

// helpCenterThemeColumns are the theme's columns in the order of domain.HelpCenterTheme; the read
// casts the media ids to text.
const helpCenterThemeColumns = `logo_media_id,favicon_media_id,logo_alt,linkback_url,contact_url,contact_phone,
	color_primary,color_header,color_header_text,color_background,color_text,color_link,color_footer,body_font,heading_font`

// GetHelpCenterTheme returns the brand of the help center with its contrast checks; a workspace
// without a help center row has the default look.
func (s *Store) GetHelpCenterTheme(ctx context.Context, workspaceID string) (domain.HelpCenterThemeView, error) {
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterThemeView, error) {
		return readHelpCenterTheme(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.HelpCenterThemeView{}, fmt.Errorf("get help center theme: %w", err)
	}
	return view, nil
}

func readHelpCenterTheme(ctx context.Context, tx pgx.Tx, workspaceID string) (domain.HelpCenterThemeView, error) {
	var view domain.HelpCenterThemeView
	theme, colors := &view.HelpCenterTheme, &view.Colors
	err := tx.QueryRow(ctx, `SELECT logo_media_id::text,favicon_media_id::text,`+strings.TrimPrefix(helpCenterThemeColumns, "logo_media_id,favicon_media_id,")+`,content_version,updated_at FROM help_centers WHERE workspace_id=$1`, workspaceID).Scan(
		&theme.LogoMediaID, &theme.FaviconMediaID, &theme.LogoAlt, &theme.LinkbackURL, &theme.ContactURL, &theme.ContactPhone,
		&colors.Primary, &colors.Header, &colors.HeaderText, &colors.Background, &colors.Text, &colors.Link, &colors.Footer,
		&theme.BodyFont, &theme.HeadingFont, &view.ContentVersion, &view.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.HelpCenterThemeView{}, err
	}
	view.UpdatedAt = domain.UTCOptional(view.UpdatedAt)
	view.Checks, _ = domain.ThemeContrast(view.Colors)
	return view, nil
}

// CheckHelpCenterThemeMedia refuses a logo or favicon that is not an image of the workspace's library,
// or that is in its trash without being the one the same field already names, with a
// domain.ThemeError media_not_found. It changes nothing.
func (s *Store) CheckHelpCenterThemeMedia(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := checkThemeMedia(ctx, tx, workspaceID, theme, false)
		return struct{}{}, err
	})
	return themeStoreError("check help center theme", err)
}

// checkThemeMedia reads each library item the theme names and refuses one that is missing (in this
// workspace), not an image, or in the trash. An item in the trash is accepted in the field that
// already names it, so the theme read and sent back whole still saves after its logo was trashed; a
// trashed item named anew is refused. When saving, the help center row is locked first (FOR UPDATE)
// and each item FOR SHARE, so the purge, which skips locked items and checks again, never deletes
// one the save is about to name. It returns the ids the help center names before the save.
func checkThemeMedia(ctx context.Context, tx pgx.Tx, workspaceID string, theme domain.HelpCenterTheme, saving bool) ([]string, error) {
	itemLock, centerLock := "", ""
	if saving {
		itemLock, centerLock = " FOR SHARE", " FOR UPDATE"
	}
	var storedLogo, storedFavicon *string
	err := tx.QueryRow(ctx, `SELECT logo_media_id::text, favicon_media_id::text FROM help_centers WHERE workspace_id=$1`+centerLock,
		workspaceID).Scan(&storedLogo, &storedFavicon)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var previous []string
	for _, id := range []*string{storedLogo, storedFavicon} {
		if id != nil {
			previous = append(previous, *id)
		}
	}
	fields := map[string]string{}
	for _, item := range []struct {
		field  string
		id     *string
		stored *string
	}{{"logo_media_id", theme.LogoMediaID, storedLogo}, {"favicon_media_id", theme.FaviconMediaID, storedFavicon}} {
		if item.id == nil {
			continue
		}
		var kind string
		var trashed bool
		err := tx.QueryRow(ctx, `SELECT kind, deleted_at IS NOT NULL FROM kb_media WHERE workspace_id=$1 AND id=$2`+itemLock, workspaceID, *item.id).Scan(&kind, &trashed)
		found := !errors.Is(err, pgx.ErrNoRows)
		if found && err != nil {
			return nil, err
		}
		if problem := themeMediaProblem(found, kind == domain.KBMediaImage, trashed, item.stored, *item.id); problem != "" {
			fields[item.field] = problem
		}
	}
	if len(fields) > 0 {
		return nil, &domain.ThemeError{Code: "media_not_found", Fields: fields}
	}
	return previous, nil
}

// themeMediaProblem is why a theme field may not name the library item id ("" when it may): found in
// this workspace's library, an image, in the trash; stored is the id the field names before the save.
func themeMediaProblem(found, image, trashed bool, stored *string, id string) string {
	switch {
	case !found:
		return "No image with this id in the media library."
	case !image:
		return "This item is a video; choose an image."
	case trashed && (stored == nil || !strings.EqualFold(*stored, id)):
		return "This image is in the trash of the media library; restore it first."
	}
	return ""
}

// PutHelpCenterTheme replaces the brand of the help center after domain.CheckTheme and the media
// check. The logo and favicon are marked kept and locked FOR SHARE until the save commits, so the
// purge, which skips locked rows and checks again, never deletes one the theme is about to name. A
// save bumps content_version, since every page shows the theme.
func (s *Store) PutHelpCenterTheme(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme) (domain.HelpCenterThemeView, error) {
	cleaned, _, err := domain.CheckTheme(theme)
	if err != nil {
		return domain.HelpCenterThemeView{}, err
	}
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterThemeView, error) {
		previous, err := checkThemeMedia(ctx, tx, workspaceID, cleaned, true)
		if err != nil {
			return domain.HelpCenterThemeView{}, err
		}
		colors := cleaned.Colors
		if _, err := tx.Exec(ctx, `INSERT INTO help_centers(workspace_id,content_version,updated_at,`+helpCenterThemeColumns+`)
			VALUES($1,1,clock_timestamp(),$2::uuid,$3::uuid,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (workspace_id) DO UPDATE SET logo_media_id=EXCLUDED.logo_media_id, favicon_media_id=EXCLUDED.favicon_media_id,
				logo_alt=EXCLUDED.logo_alt, linkback_url=EXCLUDED.linkback_url, contact_url=EXCLUDED.contact_url, contact_phone=EXCLUDED.contact_phone,
				color_primary=EXCLUDED.color_primary, color_header=EXCLUDED.color_header, color_header_text=EXCLUDED.color_header_text,
				color_background=EXCLUDED.color_background, color_text=EXCLUDED.color_text, color_link=EXCLUDED.color_link,
				color_footer=EXCLUDED.color_footer, body_font=EXCLUDED.body_font, heading_font=EXCLUDED.heading_font,
				content_version=help_centers.content_version+1, updated_at=clock_timestamp()`,
			workspaceID, cleaned.LogoMediaID, cleaned.FaviconMediaID, cleaned.LogoAlt, cleaned.LinkbackURL, cleaned.ContactURL, cleaned.ContactPhone,
			colors.Primary, colors.Header, colors.HeaderText, colors.Background, colors.Text, colors.Link, colors.Footer,
			cleaned.BodyFont, cleaned.HeadingFont); err != nil {
			return domain.HelpCenterThemeView{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_media SET kept=true, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id IN ($2::uuid,$3::uuid) AND NOT kept`,
			workspaceID, cleaned.LogoMediaID, cleaned.FaviconMediaID); err != nil {
			return domain.HelpCenterThemeView{}, err
		}
		// An image the help center no longer shows in either field is kept no more, so "Remove unused"
		// and the purge treat it like any other image no article uses.
		if _, err := tx.Exec(ctx, `UPDATE kb_media m SET kept=false, updated_at=clock_timestamp()
			WHERE m.workspace_id=$1 AND m.id=ANY($2::uuid[]) AND m.kept AND NOT `+kbMediaNamedByHelpCenter, workspaceID, previous); err != nil {
			return domain.HelpCenterThemeView{}, err
		}
		return readHelpCenterTheme(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.HelpCenterThemeView{}, themeStoreError("put help center theme", err)
	}
	return view, nil
}

// themeStoreError passes a ThemeError through as it is and wraps anything else.
func themeStoreError(what string, err error) error {
	var invalid *domain.ThemeError
	if err == nil || errors.As(err, &invalid) {
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}
