package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const themePath = "/api/v1/help-center/theme"

type themeCheckView struct {
	Pair       string  `json:"pair"`
	Field      string  `json:"field"`
	Ratio      float64 `json:"ratio"`
	Passes     bool    `json:"passes"`
	Suggestion *string `json:"suggestion"`
}

type themeView struct {
	LogoMediaID    *string            `json:"logo_media_id"`
	FaviconMediaID *string            `json:"favicon_media_id"`
	LogoAlt        string             `json:"logo_alt"`
	LinkbackURL    *string            `json:"linkback_url"`
	ContactURL     *string            `json:"contact_url"`
	ContactPhone   *string            `json:"contact_phone"`
	Colors         map[string]*string `json:"colors"`
	BodyFont       *string            `json:"body_font"`
	HeadingFont    *string            `json:"heading_font"`
	CustomCSS      string             `json:"custom_css"`
	ContentVersion int64              `json:"content_version"`
	UpdatedAt      *string            `json:"updated_at"`
	Checks         []themeCheckView   `json:"checks"`
}

type themeErrorView struct {
	Error struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
		Checks []themeCheckView  `json:"checks"`
	} `json:"error"`
}

func fullTheme(logoID string) map[string]any {
	return map[string]any{
		"logo_media_id": logoID, "favicon_media_id": logoID, "logo_alt": "Acme",
		"linkback_url": "https://acme.example/", "contact_url": "mailto:help@acme.example", "contact_phone": "+1 (555) 010-2030",
		"colors": map[string]any{"primary": "#0B5CAD", "header": "#ffffff", "header_text": "#16201c", "background": "#ffffff",
			"text": "#1f2328", "link": "#0b5cad", "footer": "#f6f8fa"},
		"body_font": "humanist-sans", "heading_font": "transitional-serif", "custom_css": "h2{letter-spacing:.01em}",
	}
}

func themeRefused(t *testing.T, response interface{ Bytes() []byte }, code string) themeErrorView {
	t.Helper()
	var refused themeErrorView
	if err := json.Unmarshal(response.Bytes(), &refused); err != nil || refused.Error.Code != code {
		t.Fatalf("body = %s, want error %s", response.Bytes(), code)
	}
	return refused
}

func TestHelpCenterThemeRoundTripBumpsTheContentVersion(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Round Trip", "admin@theme-trip.example.com")
	agent := agentSession(t, api, admin, "agent@theme-trip.example.com")
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)

	var before themeView
	kbCall(t, api, http.MethodGet, themePath, nil, agent, http.StatusOK, &before)
	if before.LogoMediaID != nil || before.Colors["primary"] != nil || before.ContentVersion != 0 || len(before.Checks) != 6 {
		t.Fatalf("default theme = %+v", before)
	}
	if response := request(t, api, http.MethodPut, themePath, fullTheme(logo.ID), agent, nil); response.Code != http.StatusForbidden {
		t.Fatalf("an agent changed the theme: status=%d", response.Code)
	}
	var saved themeView
	kbCall(t, api, http.MethodPut, themePath, fullTheme(logo.ID), admin, http.StatusOK, &saved)
	if saved.LogoMediaID == nil || *saved.LogoMediaID != logo.ID || *saved.Colors["primary"] != "#0b5cad" || *saved.ContactPhone != "+1 (555) 010-2030" ||
		*saved.HeadingFont != "transitional-serif" || saved.CustomCSS != "h2{letter-spacing:.01em}" || saved.ContentVersion != 1 || saved.UpdatedAt == nil {
		t.Fatalf("saved theme = %+v", saved)
	}
	var read themeView
	kbCall(t, api, http.MethodGet, themePath, nil, agent, http.StatusOK, &read)
	if read.LogoAlt != "Acme" || *read.FaviconMediaID != logo.ID || *read.LinkbackURL != "https://acme.example/" || read.ContentVersion != 1 {
		t.Fatalf("read theme = %+v", read)
	}
	var reset themeView
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": ""}, admin, http.StatusOK, &reset)
	if reset.LogoMediaID != nil || reset.Colors["primary"] != nil || reset.BodyFont != nil || reset.CustomCSS != "" || reset.ContentVersion != 2 {
		t.Fatalf("reset theme = %+v", reset)
	}
	var center struct {
		ContentVersion int64 `json:"content_version"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, agent, http.StatusOK, &center)
	if center.ContentVersion != 2 {
		t.Fatalf("help center content_version = %d, want 2 after two theme saves", center.ContentVersion)
	}
}

func TestHelpCenterThemeRefusesLowContrastAndSavesNothing(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Theme Contrast", "admin@theme-contrast.example.com")
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": "", "colors": map[string]any{"link": "#0b5cad"}}, admin, http.StatusOK, nil)

	low := map[string]any{"colors": map[string]any{"text": "#999999", "background": "#ffffff"}, "custom_css": ""}
	refused := themeRefused(t, kbCall(t, api, http.MethodPut, themePath, low, admin, http.StatusUnprocessableEntity, nil).Body, "contrast_too_low")
	if !strings.HasPrefix(refused.Error.Fields["colors.text"], "Text on the background is 2.85:1; it needs 4.5:1. #") || len(refused.Error.Checks) != 6 {
		t.Fatalf("refusal = %+v", refused.Error)
	}
	shape := map[string]any{"linkback_url": "javascript:alert(1)", "colors": map[string]any{"text": "red"}, "custom_css": ""}
	// A color of the wrong shape breaks the contract's pattern, so the request is sent unvalidated.
	shaped := request(t, api, http.MethodPut, themePath, shape, admin, nil)
	if shaped.Code != http.StatusUnprocessableEntity {
		t.Fatalf("shape status=%d body=%s", shaped.Code, shaped.Body.String())
	}
	refused = themeRefused(t, shaped.Body, "validation_failed")
	if refused.Error.Fields["linkback_url"] == "" || refused.Error.Fields["colors.text"] == "" {
		t.Fatalf("shape refusal fields = %v", refused.Error.Fields)
	}
	if response := request(t, api, http.MethodPut, themePath, map[string]any{"colours": map[string]any{}}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d", response.Code)
	}

	type checked struct {
		OK     bool              `json:"ok"`
		Checks []themeCheckView  `json:"checks"`
		Fields map[string]string `json:"fields"`
	}
	var lowCheck, passCheck checked
	kbCall(t, api, http.MethodPost, themePath+"/check", low, admin, http.StatusOK, &lowCheck)
	if lowCheck.OK || lowCheck.Fields["colors.text"] == "" || len(lowCheck.Checks) != 6 {
		t.Fatalf("check of a low contrast theme = %+v", lowCheck)
	}
	kbCall(t, api, http.MethodPost, themePath+"/check", map[string]any{"colors": map[string]any{"text": "#767676", "background": "#ffffff"}}, admin, http.StatusOK, &passCheck)
	if !passCheck.OK || len(passCheck.Fields) != 0 {
		t.Fatalf("check of #767676 on white (4.54:1) = %+v", passCheck)
	}
	var after themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &after)
	if after.Colors["text"] != nil || after.Colors["link"] == nil || after.ContentVersion != 1 {
		t.Fatalf("theme after refusals and checks = %+v", after)
	}
}

func TestHelpCenterThemeStaysInItsWorkspace(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "Theme Alpha", "admin@theme-alpha.example.com")
	beta := createSession(t, api, service, "Theme Beta", "admin@theme-beta.example.com")
	logo := uploadKBImage(t, api, alpha, kbPNG, http.StatusCreated)
	kbCall(t, api, http.MethodPut, themePath, fullTheme(logo.ID), alpha, http.StatusOK, nil)

	for _, path := range []string{themePath, themePath + "/check"} {
		method := http.MethodPut
		if strings.HasSuffix(path, "/check") {
			method = http.MethodPost
		}
		refused := themeRefused(t, kbCall(t, api, method, path, map[string]any{"logo_media_id": logo.ID, "custom_css": ""}, beta, http.StatusUnprocessableEntity, nil).Body, "media_not_found")
		if refused.Error.Fields["logo_media_id"] == "" {
			t.Fatalf("%s fields = %v", path, refused.Error.Fields)
		}
	}
	var seen themeView
	kbCall(t, api, http.MethodGet, themePath, nil, beta, http.StatusOK, &seen)
	if seen.LogoMediaID != nil || seen.Colors["primary"] != nil || seen.ContentVersion != 0 {
		t.Fatalf("beta sees %+v", seen)
	}
	// An image in the library's trash is refused as a new logo until it is restored.
	trashed := uploadKBImage(t, api, alpha, pngImage(21), http.StatusCreated)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+trashed.ID, nil, alpha, http.StatusOK, nil)
	themeRefused(t, kbCall(t, api, http.MethodPut, themePath, fullTheme(trashed.ID), alpha, http.StatusUnprocessableEntity, nil).Body, "media_not_found")
}

func TestHelpCenterThemeTokenTiers(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Theme Tokens", "admin@theme-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	adminToken := issueToken(t, api, admin, "Admin", "admin").Token
	body := map[string]any{"colors": map[string]any{"primary": "#0b5cad"}, "custom_css": ""}

	if response := request(t, api, http.MethodPut, themePath, body, send, nil); response.Code != http.StatusAccepted {
		t.Fatalf("send token put status=%d body=%s, want 202", response.Code, response.Body.String())
	}
	kbCall(t, api, http.MethodPost, themePath+"/check", body, read, http.StatusOK, nil)
	var theme themeView
	kbCall(t, api, http.MethodGet, themePath, nil, read, http.StatusOK, &theme)
	if theme.Colors["primary"] != nil {
		t.Fatalf("a request changed the theme before approval: %+v", theme)
	}
	kbCall(t, api, http.MethodPut, themePath, body, adminToken, http.StatusOK, &theme)
	if theme.Colors["primary"] == nil {
		t.Fatalf("admin token put = %+v", theme)
	}
}
