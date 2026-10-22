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

// CheckHelpCenterThemeMedia refuses a logo or favicon that is not an image of the workspace's library
// outside the trash, with a domain.ThemeError media_not_found. It changes nothing.
func (s *Store) CheckHelpCenterThemeMedia(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, checkThemeMedia(ctx, tx, workspaceID, theme, "")
	})
	return themeStoreError("check help center theme", err)
}

// checkThemeMedia reads each library item the theme names, with lock appended to the query, and
// refuses one that is missing (in this workspace), in the trash or not an image.
func checkThemeMedia(ctx context.Context, tx pgx.Tx, workspaceID string, theme domain.HelpCenterTheme, lock string) error {
	fields := map[string]string{}
	for _, item := range []struct {
		field string
		id    *string
	}{{"logo_media_id", theme.LogoMediaID}, {"favicon_media_id", theme.FaviconMediaID}} {
		if item.id == nil {
			continue
		}
		var kind string
		var trashed bool
		err := tx.QueryRow(ctx, `SELECT kind, deleted_at IS NOT NULL FROM kb_media WHERE workspace_id=$1 AND id=$2`+lock, workspaceID, *item.id).Scan(&kind, &trashed)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			fields[item.field] = "No image with this id in the media library."
		case err != nil:
			return err
		case trashed:
			fields[item.field] = "This image is in the trash of the media library; restore it first."
		case kind != domain.KBMediaImage:
			fields[item.field] = "This item is a video; choose an image."
		}
	}
	if len(fields) > 0 {
		return &domain.ThemeError{Code: "media_not_found", Fields: fields}
	}
	return nil
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
		if err := checkThemeMedia(ctx, tx, workspaceID, cleaned, " FOR SHARE"); err != nil {
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
		if _, err := tx.Exec(ctx, `UPDATE kb_media SET kept=true WHERE workspace_id=$1 AND id IN ($2::uuid,$3::uuid) AND NOT kept`,
			workspaceID, cleaned.LogoMediaID, cleaned.FaviconMediaID); err != nil {
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
