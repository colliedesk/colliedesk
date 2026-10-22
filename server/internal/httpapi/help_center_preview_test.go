package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const previewPath = "/api/v1/help-center/theme/preview"

// previewCSP is the policy every preview document carries in a meta element: it is shown in a frame of
// the app, so it must hold its own rules whatever the app's page allows.
const previewCSP = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'">`

type previewView struct {
	HTML   string            `json:"html"`
	OK     bool              `json:"ok"`
	Checks []themeCheckView  `json:"checks"`
	Fields map[string]string `json:"fields"`
}

func preview(t *testing.T, api http.Handler, token, page string, theme map[string]any, want int) previewView {
	t.Helper()
	var view previewView
	response := kbCall(t, api, http.MethodPost, previewPath, map[string]any{"page": page, "theme": theme}, token, want, nil)
	if want == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
			t.Fatalf("preview body: %v", err)
		}
	}
	return view
}

func wantHTML(t *testing.T, label, html string, holds, lacks []string) {
	t.Helper()
	for _, want := range holds {
		if !strings.Contains(html, want) {
			t.Errorf("%s lacks %q:\n%s", label, want, html)
		}
	}
	for _, unwanted := range lacks {
		if strings.Contains(html, unwanted) {
			t.Errorf("%s holds %q", label, unwanted)
		}
	}
}

// The preview is the real page with the candidate brand, built from this workspace's published content
// only, while the help center is off, and it saves nothing. Another workspace's collections, articles and
// images never reach it, and its logo cannot be another workspace's image.
func TestHelpCenterThemePreviewShowsTheCandidateOnTheWorkspacesOwnPages(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Preview Acme", "admin@preview-acme.example.com")
	agent := agentSession(t, api, admin, "agent@preview-acme.example.com")
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	drafts := kbCollection(t, api, admin, map[string]any{"name": "Unreleased plans"})
	image := uploadKBImage(t, api, admin, pngImage(3), http.StatusCreated)
	kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Reset your password", "status": "published",
		"body_md": "Open **Settings**.\n\n![The form](" + "/api/v1/kb/images/" + image.ID + ")"})
	kbArticle(t, api, admin, map[string]any{"collection_id": drafts.ID, "title": "Secret roadmap", "status": "draft", "body_md": "not yet"})
	logo := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)

	other := createSession(t, api, service, "Preview Other", "admin@preview-other.example.com")
	otherCollection := kbCollection(t, api, other, map[string]any{"name": "Other team handbook"})
	kbArticle(t, api, other, map[string]any{"collection_id": otherCollection.ID, "title": "Other team payroll", "status": "published", "body_md": "private"})
	otherLogo := uploadKBImage(t, api, other, pngImage(5), http.StatusCreated)

	var before themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &before)
	theme := map[string]any{"logo_media_id": logo.ID, "logo_alt": "Acme", "colors": map[string]any{"primary": "#0b5cad"}, "body_font": "humanist-sans"}
	home := preview(t, api, admin, "home", theme, http.StatusOK)
	if !home.OK || len(home.Checks) != 6 || len(home.Fields) != 0 {
		t.Fatalf("home checks = %+v", home)
	}
	shared := []string{
		"<!doctype html>", previewCSP, "<style>", "--accent:#0b5cad;", "--font-body:Seravek", `<meta name="color-scheme" content="light">`,
		`<img class="brand-logo" src="/api/v1/kb/media/` + logo.ID + `" alt="Acme"`,
	}
	never := []string{"<script", "<link rel=\"stylesheet\"", "style=", "Secret roadmap", "Unreleased plans", "Other team", otherLogo.ID, "/_/brand-"}
	wantHTML(t, "home preview", home.HTML, append(shared, "Guides", "Reset your password"), never)
	if strings.Index(home.HTML, previewCSP) > strings.Index(home.HTML, "<style>") {
		t.Error("the policy must come before the style it governs")
	}

	article := preview(t, api, admin, "article", theme, http.StatusOK)
	wantHTML(t, "article preview", article.HTML, append(shared, "Reset your password", "<strong>Settings</strong>", `src="/api/v1/kb/images/`+image.ID+`"`), never)

	// An agent may look too; nothing is saved by either.
	preview(t, api, agent, "home", theme, http.StatusOK)
	var saved themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &saved)
	if saved.LogoMediaID != nil || saved.Colors["primary"] != nil || saved.ContentVersion != before.ContentVersion {
		t.Fatalf("a preview wrote the theme: %+v", saved)
	}

	// The other workspace sees its own pages, and cannot borrow this workspace's logo.
	otherHome := preview(t, api, other, "home", map[string]any{}, http.StatusOK)
	wantHTML(t, "other home preview", otherHome.HTML, []string{"Other team handbook"}, []string{"Guides", "Reset your password", logo.ID, image.ID})
	borrowed := themeRefused(t, kbCall(t, api, http.MethodPost, previewPath, map[string]any{"page": "home", "theme": map[string]any{"logo_media_id": logo.ID}},
		other, http.StatusUnprocessableEntity, nil).Body, "media_not_found")
	if _, named := borrowed.Error.Fields["logo_media_id"]; !named {
		t.Fatalf("borrowed logo refusal = %+v", borrowed)
	}
}

// Contrast does not block a preview: the person sees the page and why Save will refuse it.
func TestHelpCenterThemePreviewShowsAContrastFailureThatSaveRefuses(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Preview Contrast", "admin@preview-contrast.example.com")
	theme := map[string]any{"colors": map[string]any{"text": "#999999"}}
	got := preview(t, api, admin, "home", theme, http.StatusOK)
	if got.OK || !strings.Contains(got.Fields["colors.text"], "4.5:1") || !strings.Contains(got.HTML, "--ink:#999999;") {
		t.Fatalf("preview of a failing theme = ok %v fields %v", got.OK, got.Fields)
	}
	failing := 0
	for _, check := range got.Checks {
		if !check.Passes {
			failing++
			if check.Field != "colors.text" || check.Suggestion == nil {
				t.Errorf("failing check = %+v", check)
			}
		}
	}
	if failing == 0 {
		t.Fatal("no failing check")
	}
	themeRefused(t, kbCall(t, api, http.MethodPut, themePath, theme, admin, http.StatusUnprocessableEntity, nil).Body, "contrast_too_low")
}

// With nothing published the article page shows a sample text, so the look can be judged anyway.
func TestHelpCenterThemePreviewOfAnEmptyKnowledgeBase(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Preview Empty", "admin@preview-empty.example.com")
	home := preview(t, api, admin, "home", map[string]any{}, http.StatusOK)
	wantHTML(t, "empty home", home.HTML, []string{previewCSP, "Nothing is published here yet.", `<meta name="color-scheme" content="light dark">`}, []string{"--accent:#"})
	article := preview(t, api, admin, "article", map[string]any{}, http.StatusOK)
	wantHTML(t, "sample article", article.HTML, []string{previewCSP, "<h1>An example article</h1>"}, []string{"<script"})
}

func TestHelpCenterThemePreviewRefusesAWrongShape(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Preview Shape", "admin@preview-shape.example.com")
	for name, body := range map[string]map[string]any{
		"page":         {"page": "search", "theme": map[string]any{}},
		"colors.text":  {"page": "home", "theme": map[string]any{"colors": map[string]any{"text": "blue"}}},
		"linkback_url": {"page": "home", "theme": map[string]any{"linkback_url": "javascript:alert(1)"}},
	} {
		// Bodies the contract refuses, so they go around the contract check of kbCall.
		response := request(t, api, http.MethodPost, previewPath, body, admin, nil)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status=%d body=%s", name, response.Code, response.Body.String())
		}
		refused := themeRefused(t, response.Body, "validation_failed")
		if _, named := refused.Error.Fields[name]; !named {
			t.Errorf("%s: fields = %v", name, refused.Error.Fields)
		}
	}
	if response := request(t, api, http.MethodPost, previewPath, map[string]any{"page": "home", "theme": map[string]any{}, "extra": 1}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d", response.Code)
	}
}

// Each person's previews count against the help center's page budget, so a stuck editor cannot load the
// server; another person keeps theirs.
func TestHelpCenterThemePreviewIsLimitedPerPerson(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Preview Busy", "admin@preview-busy.example.com")
	agent := agentSession(t, api, admin, "agent@preview-busy.example.com")
	// A body the server cannot read still counts, and costs no rendering.
	for index := range 600 {
		if response := request(t, api, http.MethodPost, previewPath, map[string]any{"page": 1}, admin, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("preview %d status=%d", index, response.Code)
		}
	}
	busy := request(t, api, http.MethodPost, previewPath, map[string]any{"page": "home", "theme": map[string]any{}}, admin, nil)
	if busy.Code != http.StatusTooManyRequests || busy.Header().Get("Retry-After") == "" {
		t.Fatalf("601st preview status=%d headers=%v, want 429 with Retry-After", busy.Code, busy.Header())
	}
	preview(t, api, agent, "home", map[string]any{}, http.StatusOK)
}
