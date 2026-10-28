package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postForm posts a submission to a public form path, from origin when not empty.
func postForm(api http.Handler, path, body, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return serve(api, req)
}

type formChannel struct {
	ID     string `json:"id"`
	Config struct {
		SubmitPath string `json:"submit_path"`
		LegacySlug bool   `json:"legacy_slug"`
	} `json:"config"`
}

func addWebForm(t *testing.T, api http.Handler, admin, name string, origins []string) formChannel {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "web_form", "name": name, "config": map[string]any{"allowed_origins": origins}}, admin, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("add %s: %d %s", name, response.Code, response.Body.String())
	}
	var form formChannel
	decode(t, response, &form)
	return form
}

// Each form has its own address and origins, two forms may share an origin, and a submission opens
// the conversation on the form it was posted to. The workspace's older address stays with the form
// that had it, whichever form is the default.
func TestEachWebFormHasItsOwnAddress(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Two Forms", "admin@two-forms.example.com")
	slugPath := "/api/v1/public/forms/" + workspaceSlug(t, api, admin)
	contact := addWebForm(t, api, admin, "Contact form", []string{"https://shop.example.com"})
	careers := addWebForm(t, api, admin, "Careers form", []string{"https://shop.example.com"})
	if !contact.Config.LegacySlug || contact.Config.SubmitPath != slugPath {
		t.Fatalf("the first form: %+v, want the older address %s", contact.Config, slugPath)
	}
	if careers.Config.LegacySlug || careers.Config.SubmitPath != "/api/v1/public/web-forms/"+careers.ID {
		t.Fatalf("the second form: %+v, want its own address", careers.Config)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+careers.ID, map[string]any{"is_default": true}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("make careers the default: %d %s", response.Code, response.Body.String())
	}
	opened := func(path, origin string) string {
		t.Helper()
		before := conversationChannels(t, api, admin)
		response := postForm(api, path, `{"email":"visitor@example.com","message":"Hello"}`, origin)
		if response.Code != http.StatusAccepted || response.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatalf("post %s from %s: %d %s (CORS %q)", path, origin, response.Code, response.Body.String(), response.Header().Get("Access-Control-Allow-Origin"))
		}
		for id, channel := range conversationChannels(t, api, admin) {
			if _, seen := before[id]; !seen {
				return channel
			}
		}
		t.Fatalf("post %s opened no conversation", path)
		return ""
	}
	if got := opened(slugPath, "https://shop.example.com"); got != contact.ID {
		t.Errorf("the older address opened on %s, want the form that has it %s", got, contact.ID)
	}
	if got := opened("/api/v1/public/web-forms/"+careers.ID, "https://shop.example.com"); got != careers.ID {
		t.Errorf("careers' address opened on %s, want %s", got, careers.ID)
	}
	if got := opened("/api/v1/public/web-forms/"+contact.ID, "https://shop.example.com"); got != contact.ID {
		t.Errorf("contact's own address opened on %s, want %s", got, contact.ID)
	}
	// Removing the form with the older address leaves that address answering nothing.
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+contact.ID, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove contact: %d", response.Code)
	}
	if response := postForm(api, slugPath, `{"email":"visitor@example.com"}`, ""); response.Code != http.StatusNotFound {
		t.Fatalf("the older address after its form went: %d, want 404", response.Code)
	}
}

// A form's address answers the same 404 for an unknown id, a removed or off form, another type's
// channel and a malformed id, so it tells nothing; and a form of one workspace is never another's.
func TestAFormAddressTellsNothingOfOtherForms(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	alpha := createSession(t, api, service, "Alpha Forms", "admin@alpha-forms.example.com")
	connectEmail(t, api, alpha, "support@alpha-forms.example.com")
	off := addWebForm(t, api, alpha, "Off form", []string{})
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+off.ID, map[string]any{"enabled": false}, alpha, nil); response.Code != http.StatusOK {
		t.Fatalf("turn off: %d", response.Code)
	}
	email := emailChannelOf(t, api, alpha, "").Channel.ChannelID
	for _, id := range []string{off.ID, email, "00000000-0000-4000-8000-000000000000", "nope"} {
		for _, method := range []string{http.MethodPost, http.MethodOptions} {
			req := httptest.NewRequest(method, "/api/v1/public/web-forms/"+id, strings.NewReader(`{"email":"v@example.com"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://x.example.com")
			if response := serve(api, req); response.Code != http.StatusNotFound {
				t.Errorf("%s %s: %d %s, want 404", method, id, response.Code, response.Body.String())
			}
		}
	}
}

// conversationChannels maps every conversation of the workspace to the channel it came through.
func conversationChannels(t *testing.T, api http.Handler, token string) map[string]string {
	t.Helper()
	var page struct {
		Items []struct {
			ID         string `json:"id"`
			ChannelRef *struct {
				ID string `json:"id"`
			} `json:"channel_ref"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations?page_size=100", nil, token, nil), &page)
	channels := map[string]string{}
	for _, item := range page.Items {
		channels[item.ID] = ""
		if item.ChannelRef != nil {
			channels[item.ID] = item.ChannelRef.ID
		}
	}
	return channels
}
