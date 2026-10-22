package httpapi

import (
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
)

// themeFields are the fields a theme call takes; an agent request carrying any other is refused
// before it is stored (settingsFields).
var themeFields = []string{"logo_media_id", "favicon_media_id", "logo_alt", "linkback_url", "contact_url", "contact_phone", "colors", "body_font", "heading_font", "custom_css"}

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
	CustomCSS      *string             `json:"custom_css"`
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
	if input.CustomCSS != nil {
		theme.CustomCSS = *input.CustomCSS
	}
	return theme
}

// cleanTheme is domain.CleanTheme and the custom CSS check, which every call taking a theme runs: a
// refused text never reaches the store or a preview's <style> element.
func cleanTheme(theme domain.HelpCenterTheme) (domain.HelpCenterTheme, error) {
	cleaned, err := domain.CleanTheme(theme)
	if err != nil {
		return domain.HelpCenterTheme{}, err
	}
	if err := helpcenter.CheckCustomCSS(cleaned.CustomCSS); err != nil {
		return domain.HelpCenterTheme{}, &domain.ThemeError{Code: "custom_css_refused", Fields: map[string]string{"custom_css": err.Error()}}
	}
	return cleaned, nil
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
	case "custom_css_refused":
		message = "The custom CSS was refused: " + invalid.Fields["custom_css"] + ". Nothing was saved."
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
	cleaned, err := cleanTheme(theme)
	if err != nil {
		writeThemeError(response, err)
		return
	}
	view, err := a.store.PutHelpCenterTheme(request.Context(), principal(request).Workspace.ID, cleaned)
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
	cleaned, err := cleanTheme(theme)
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

// previewHelpCenterTheme renders a page of the help center with a candidate theme and saves nothing.
// Each person's previews count against the help center's page budget, so an editor left typing cannot
// load the server; a body the server cannot read counts too.
func (a *API) previewHelpCenterTheme(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if allowed, wait := a.helpCenterPages.allow("preview "+who.Workspace.ID+" "+who.Agent.ID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "too_many_requests", "Too many previews in the last minute; try again in a moment.")
		return
	}
	var input struct {
		Page  string     `json:"page"`
		Theme themeInput `json:"theme"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send page (home or article) and theme, with only the theme's fields: "+strings.Join(themeFields, ", ")+".")
		return
	}
	if input.Page != helpcenter.PreviewHome && input.Page != helpcenter.PreviewArticle {
		writeThemeError(response, &domain.ThemeError{Code: "validation_failed", Fields: map[string]string{"page": "Choose home or article."}})
		return
	}
	cleaned, err := cleanTheme(input.Theme.theme())
	if err != nil {
		writeThemeError(response, err)
		return
	}
	// The logo and favicon are checked in the preview's own transaction.
	html, err := a.helpCenter.Preview(request.Context(), who.Workspace.ID, cleaned, input.Page)
	if err != nil {
		writeThemeError(response, err)
		return
	}
	checks, fields := domain.ThemeContrast(cleaned.Colors)
	writeJSON(response, http.StatusOK, map[string]any{"html": string(html), "ok": len(fields) == 0, "checks": checks, "fields": fields})
}
