package httpapi

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// themeFields are the fields a theme call takes; an agent request carrying any other is refused
// before it is stored (settingsFields).
var themeFields = []string{"logo_media_id", "favicon_media_id", "logo_alt", "linkback_url", "contact_url", "contact_phone", "colors", "body_font", "heading_font"}

// themeColorFields are the keys of the theme's colors object (domain.ThemeColors).
var themeColorFields = []string{"primary", "header", "header_text", "background", "text", "link", "footer"}

// themeInput is the body of PUT /help-center/theme and its check. A field left out is the default.
type themeInput struct {
	LogoMediaID    *string             `json:"logo_media_id"`
	FaviconMediaID *string             `json:"favicon_media_id"`
	LogoAlt        *string             `json:"logo_alt"`
	LinkbackURL    *string             `json:"linkback_url"`
	ContactURL     *string             `json:"contact_url"`
	ContactPhone   *string             `json:"contact_phone"`
	Colors         *domain.ThemeColors `json:"colors"`
	BodyFont       *string             `json:"body_font"`
	HeadingFont    *string             `json:"heading_font"`
}

func (input themeInput) theme() domain.HelpCenterTheme {
	theme := domain.HelpCenterTheme{LogoMediaID: input.LogoMediaID, FaviconMediaID: input.FaviconMediaID,
		LinkbackURL: input.LinkbackURL, ContactURL: input.ContactURL, ContactPhone: input.ContactPhone,
		BodyFont: input.BodyFont, HeadingFont: input.HeadingFont}
	if input.LogoAlt != nil {
		theme.LogoAlt = *input.LogoAlt
	}
	if input.Colors != nil {
		theme.Colors = *input.Colors
	}
	return theme
}

func decodeTheme(response http.ResponseWriter, request *http.Request) (domain.HelpCenterTheme, bool) {
	var input themeInput
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the theme as a JSON object with only these fields: "+strings.Join(themeFields, ", ")+".")
		return domain.HelpCenterTheme{}, false
	}
	return input.theme(), true
}

// writeThemeError answers a domain.ThemeError with 422, its fields and, on contrast_too_low, every check.
func writeThemeError(response http.ResponseWriter, err error) {
	var invalid *domain.ThemeError
	if !errors.As(err, &invalid) {
		writeStoreError(response, err)
		return
	}
	message := "Check the highlighted fields. Nothing was saved."
	switch invalid.Code {
	case "media_not_found":
		message = "The logo and favicon must be images of the media library, outside its trash. Nothing was saved."
	case "contrast_too_low":
		var lines []string
		for _, field := range slices.Sorted(maps.Keys(invalid.Fields)) {
			lines = append(lines, invalid.Fields[field])
		}
		message = strings.Join(lines, " ") + " Nothing was saved."
	}
	body := map[string]any{"code": invalid.Code, "message": message, "fields": invalid.Fields}
	if invalid.Checks != nil {
		body["checks"] = invalid.Checks
	}
	writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, body)})
}

func (a *API) getHelpCenterTheme(response http.ResponseWriter, request *http.Request) {
	view, err := a.store.GetHelpCenterTheme(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (a *API) putHelpCenterTheme(response http.ResponseWriter, request *http.Request) {
	theme, ok := decodeTheme(response, request)
	if !ok {
		return
	}
	view, err := a.store.PutHelpCenterTheme(request.Context(), principal(request).Workspace.ID, theme)
	if err != nil {
		writeThemeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

// checkHelpCenterTheme runs the checks of a save and stores nothing. Contrast is an answer here, not
// an error, so an editor can show every pair while a person picks colors.
func (a *API) checkHelpCenterTheme(response http.ResponseWriter, request *http.Request) {
	theme, ok := decodeTheme(response, request)
	if !ok {
		return
	}
	cleaned, err := domain.CleanTheme(theme)
	if err == nil {
		err = a.store.CheckHelpCenterThemeMedia(request.Context(), principal(request).Workspace.ID, cleaned)
	}
	if err != nil {
		writeThemeError(response, err)
		return
	}
	checks, fields := domain.ThemeContrast(cleaned.Colors)
	writeJSON(response, http.StatusOK, map[string]any{"ok": len(fields) == 0, "checks": checks, "fields": fields})
}
