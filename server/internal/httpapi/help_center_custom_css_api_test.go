package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// pageCSP is the policy of every public help center page; custom CSS must leave it as it is.
const pageCSP = "default-src 'none'; img-src 'self'; style-src 'self'; frame-src https://www.youtube-nocookie.com https://player.vimeo.com; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

func TestHelpCenterThemeRefusesCustomCSSAtItsLineAndSavesNothing(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Custom CSS Refused", "admin@css-refused.example.com")
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": ".masthead{border-bottom:2px solid #0b5cad}"}, admin, http.StatusOK, nil)

	hostile := map[string]any{"custom_css": "a{\n  color:red;\n  background:url(https://tracker.example/p.gif)\n}"}
	want := "line 3, column 14: url() is not allowed"
	for _, call := range []struct{ method, path string }{{http.MethodPut, themePath}, {http.MethodPost, themePath + "/check"}} {
		refused := themeRefused(t, kbCall(t, api, call.method, call.path, hostile, admin, http.StatusUnprocessableEntity, nil).Body, "custom_css_refused")
		if refused.Error.Fields["custom_css"] != want {
			t.Fatalf("%s %s fields = %v, want custom_css %q", call.method, call.path, refused.Error.Fields, want)
		}
	}
	// The preview puts custom CSS inside a <style> element, so a refused text never reaches it either.
	escape := map[string]any{"custom_css": `a::after{content:"</style><img src=x>"}`}
	refused := themeRefused(t, kbCall(t, api, http.MethodPost, previewPath, map[string]any{"page": "home", "theme": escape}, admin, http.StatusUnprocessableEntity, nil).Body, "custom_css_refused")
	if !strings.HasPrefix(refused.Error.Fields["custom_css"], "line 1, column 19: </ is not allowed") {
		t.Fatalf("preview fields = %v", refused.Error.Fields)
	}

	var after themeView
	kbCall(t, api, http.MethodGet, themePath, nil, admin, http.StatusOK, &after)
	if after.CustomCSS != ".masthead{border-bottom:2px solid #0b5cad}" || after.ContentVersion != 1 {
		t.Fatalf("theme after refusals = %+v", after)
	}
}

func TestHelpCenterCustomCSSIsServedAfterTheBrandWithTheSamePolicyAndClears(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Custom CSS Pages", "admin@css-pages.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Styled help", true)
	base := "/help/" + slug

	css := "/* our header */\n@media (max-width:600px){.brand{font-size:1rem}}\n.masthead{border-bottom:2px solid #0b5cad}\n"
	theme := map[string]any{"colors": map[string]any{"primary": "#0b5cad"}, "custom_css": css}
	var saved themeView
	kbCall(t, api, http.MethodPut, themePath, theme, admin, http.StatusOK, &saved)
	if saved.CustomCSS != css {
		t.Fatalf("saved custom_css = %q", saved.CustomCSS)
	}
	page := visit(site, base, "192.0.2.70")
	if got := page.Header().Get("Content-Security-Policy"); got != pageCSP {
		t.Fatalf("CSP with custom CSS = %q", got)
	}
	wantPage(t, page, http.StatusOK, nil, []string{"<style", "style=", "border-bottom"})
	sheet := visit(site, brandSheet(t, page), "192.0.2.70")
	body := sheet.Body.String()
	// The variables first, then the custom CSS byte for byte, so it can override them.
	if sheet.Code != http.StatusOK || !strings.HasPrefix(body, ":root{--paper:") || !strings.HasSuffix(body, "}\n"+css) ||
		sheet.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || sheet.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("brand sheet status=%d headers=%v body:\n%s", sheet.Code, sheet.Header(), body)
	}

	// Custom CSS alone is a brand sheet of that text only; the visitor's dark setting stays.
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": css}, admin, http.StatusOK, nil)
	alone := visit(site, base, "192.0.2.70")
	wantPage(t, alone, http.StatusOK, []string{`<meta name="color-scheme" content="light dark">`}, nil)
	if got := visit(site, brandSheet(t, alone), "192.0.2.70").Body.String(); got != css {
		t.Fatalf("brand sheet of custom CSS alone:\n%q", got)
	}
	oldSheet := brandSheet(t, alone)

	// The preview carries it too, in its inline block, before the rule that keeps links inert.
	shown := preview(t, api, admin, "home", map[string]any{"custom_css": css}, http.StatusOK)
	if style := previewStyle(t, shown.HTML); !strings.Contains(style, css) || strings.Index(style, css) > strings.Index(style, "pointer-events:none!important") {
		t.Fatalf("preview style block:\n%s", style)
	}

	// Clearing it (whitespace counts as none) removes the sheet, and the old name is the help center's 404.
	var cleared themeView
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": " \n "}, admin, http.StatusOK, &cleared)
	if cleared.CustomCSS != "" {
		t.Fatalf("cleared custom_css = %q", cleared.CustomCSS)
	}
	plain := visit(site, base, "192.0.2.70")
	if brandSheet(t, plain) != "" || plain.Header().Get("Content-Security-Policy") != pageCSP {
		t.Fatalf("page after clearing: sheets %v CSP %q", stylesheets(plain.Body.String()), plain.Header().Get("Content-Security-Policy"))
	}
	if response := visit(site, oldSheet, "192.0.2.70"); response.Code != http.StatusNotFound {
		t.Fatalf("old brand sheet status=%d", response.Code)
	}
}

func TestHelpCenterCustomCSSStaysInItsWorkspace(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	alpha := createSession(t, api, service, "CSS Alpha", "admin@css-alpha.example.com")
	beta := createSession(t, api, service, "CSS Beta", "admin@css-beta.example.com")
	enableHelpCenter(t, api, alpha, "Alpha help", true)
	enableHelpCenter(t, api, beta, "Beta help", true)
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": ".alpha-only{color:#0b5cad}"}, alpha, http.StatusOK, nil)
	kbCall(t, api, http.MethodPut, themePath, map[string]any{"custom_css": ".beta-only{color:#8a1c1c}"}, beta, http.StatusOK, nil)

	alphaBase, betaBase := "/help/"+workspaceSlug(t, api, alpha), "/help/"+workspaceSlug(t, api, beta)
	alphaSheet, betaSheet := brandSheet(t, visit(site, alphaBase, "192.0.2.71")), brandSheet(t, visit(site, betaBase, "192.0.2.71"))
	alphaName, betaName := alphaSheet[strings.LastIndex(alphaSheet, "/")+1:], betaSheet[strings.LastIndex(betaSheet, "/")+1:]
	if alphaName == betaName {
		t.Fatalf("two workspaces share the sheet name %s", alphaName)
	}
	if body := visit(site, alphaSheet, "192.0.2.71").Body.String(); body != ".alpha-only{color:#0b5cad}" {
		t.Fatalf("alpha's sheet = %q", body)
	}
	// Beta's sheet name under alpha's help center is alpha's 404, never beta's text.
	if response := visit(site, alphaBase+"/_/"+betaName, "192.0.2.71"); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "beta-only") {
		t.Fatalf("beta's sheet under alpha status=%d body=%s", response.Code, response.Body.String())
	}
	var seen themeView
	kbCall(t, api, http.MethodGet, themePath, nil, alpha, http.StatusOK, &seen)
	if seen.CustomCSS != ".alpha-only{color:#0b5cad}" {
		t.Fatalf("alpha reads custom_css %q", seen.CustomCSS)
	}
}

func TestHelpCenterCustomCSSTokenTiers(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "CSS Tokens", "admin@css-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	adminToken := issueToken(t, api, admin, "Admin", "admin").Token
	body := map[string]any{"custom_css": ".masthead{border-bottom:2px solid #0b5cad}"}

	// A send token may only ask a person to approve it; a read token may check it.
	if response := request(t, api, http.MethodPut, themePath, body, send, nil); response.Code != http.StatusAccepted {
		t.Fatalf("send token put status=%d body=%s, want 202", response.Code, response.Body.String())
	}
	kbCall(t, api, http.MethodPost, themePath+"/check", body, read, http.StatusOK, nil)
	var theme themeView
	kbCall(t, api, http.MethodPut, themePath, body, adminToken, http.StatusOK, &theme)
	if theme.CustomCSS != body["custom_css"] {
		t.Fatalf("admin token put = %+v", theme)
	}
}
