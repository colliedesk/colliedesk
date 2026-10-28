package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// The documented way to change one thing is to read the theme and send it back whole. A logo moved to
// the library's trash since must not block that: the id already stored in the same field is accepted,
// while a trashed id named anew, or moved to the other field, is still refused.
func TestHelpCenterThemeSavesAgainWithTheLogoItAlreadyHasInTheTrash(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Trashed Logo", "admin@theme-trashed-logo.example.com")
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	icon := uploadKBImage(t, api, admin, pngImage(9), http.StatusCreated)
	other := uploadKBImage(t, api, admin, pngImage(11), http.StatusCreated)
	theme := fullTheme(logo.ID)
	theme["favicon_media_id"] = icon.ID
	kbCall(t, api, http.MethodPut, themePath, theme, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+logo.ID, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+other.ID, nil, admin, http.StatusOK, nil)

	var read themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &read)
	if read.LogoMediaID == nil || *read.LogoMediaID != logo.ID {
		t.Fatalf("theme with a trashed logo = %+v", read)
	}
	resent := map[string]any{"logo_media_id": *read.LogoMediaID, "favicon_media_id": *read.FaviconMediaID, "logo_alt": read.LogoAlt,
		"colors": map[string]any{"primary": "#7a3e00"}}
	kbCall(t, api, http.MethodPost, themePath+"/check", resent, admin, http.StatusOK, nil)
	var saved themeView
	kbCall(t, api, http.MethodPut, themePath, resent, admin, http.StatusOK, &saved)
	if *saved.Colors["primary"] != "#7a3e00" || *saved.LogoMediaID != logo.ID || saved.ContentVersion != 2 {
		t.Fatalf("saved = %+v", saved)
	}

	for field, body := range map[string]map[string]any{
		"favicon_media_id": {"logo_media_id": logo.ID, "favicon_media_id": logo.ID},
		"logo_media_id":    {"logo_media_id": other.ID, "favicon_media_id": icon.ID},
	} {
		refused := themeRefused(t, kbCall(t, api, http.MethodPut, themePath, body, admin, http.StatusUnprocessableEntity, nil).Body, "media_not_found")
		if !strings.Contains(refused.Error.Fields[field], "trash") || len(refused.Error.Fields) != 1 {
			t.Fatalf("%s refusal fields = %v", field, refused.Error.Fields)
		}
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+logo.ID+"/restore", nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &read)
	if read.LogoMediaID == nil || *read.LogoMediaID != logo.ID || *read.Colors["primary"] != "#7a3e00" {
		t.Fatalf("theme after restore = %+v", read)
	}
}

func TestHelpCenterThemeRefusesAVideoAsLogoOrFavicon(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Theme Video", "admin@theme-video.example.com")
	data := mediaVideo(t, "h264.mp4")
	video := uploadKBVideo(t, api, admin, data, len(data), nil)
	for _, field := range []string{"logo_media_id", "favicon_media_id"} {
		refused := themeRefused(t, kbCall(t, api, http.MethodPut, themePath, map[string]any{field: video.ID}, admin, http.StatusUnprocessableEntity, nil).Body, "media_not_found")
		if !strings.Contains(refused.Error.Fields[field], "video") {
			t.Fatalf("%s refusal fields = %v", field, refused.Error.Fields)
		}
	}
	var theme themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &theme)
	if theme.LogoMediaID != nil || theme.FaviconMediaID != nil || theme.ContentVersion != 0 {
		t.Fatalf("theme after video refusals = %+v", theme)
	}
}

// A send token's theme change waits as a request; an admin approving it changes the theme.
func TestApprovedThemeRequestChangesTheTheme(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Theme Approve", "admin@theme-approve.example.com")
	send := issueToken(t, api, admin, "Writer", "send").Token
	filed := accepted(t, api, http.MethodPut, themePath, map[string]any{"colors": map[string]any{"primary": "#0b5cad"}, "body_font": "mono"}, send)
	if response := approve(t, api, admin, filed.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", response.Code, response.Body.String())
	}
	var theme themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &theme)
	if theme.Colors["primary"] == nil || *theme.Colors["primary"] != "#0b5cad" || theme.BodyFont == nil || *theme.BodyFont != "mono" || theme.ContentVersion != 1 {
		t.Fatalf("theme after approval = %+v", theme)
	}
}

// A theme request is stored for days and shown to people, so a key hidden in a nested object, a
// color nobody has, or an object where a value goes, is refused before anything is filed.
func TestThemeRequestWithAnUnexpectedNestedFieldIsNotFiled(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Theme Filing", "admin@theme-filing.example.com")
	send := issueToken(t, api, admin, "Writer", "send").Token
	for _, body := range []map[string]any{
		{"colors": map[string]any{"primary": "#0b5cad", "api_key": "sk-hidden-1"}},
		{"colors": map[string]any{"primary": map[string]any{"secret": "sk-hidden-2"}}},
		{"logo_alt": map[string]any{"secret": "sk-hidden-3"}},
		{"colors": []any{map[string]any{"secret": "sk-hidden-4"}}},
	} {
		response := request(t, api, http.MethodPut, themePath, body, send, nil)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Nothing was filed") {
			t.Fatalf("%v: status=%d body=%s, want 400 and nothing filed", body, response.Code, response.Body.String())
		}
	}
	for _, item := range listRequests(t, api, admin, "") {
		if strings.Contains(string(item.Body), "sk-hidden") {
			t.Fatalf("a stored request keeps a nested field the theme does not take: %s", item.Body)
		}
	}
	accepted(t, api, http.MethodPut, themePath, map[string]any{"colors": map[string]any{"primary": "#0b5cad", "footer": nil}}, send)
}
