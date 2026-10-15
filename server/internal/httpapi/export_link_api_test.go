package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type exportLink struct {
	URL              string `json:"url"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

func makeExportLink(t *testing.T, api http.Handler, token string) exportLink {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/export/link", nil, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create export link: status=%d body=%s, want 201", response.Code, response.Body.String())
	}
	var link exportLink
	decode(t, response, &link)
	return link
}

// openExportLink opens the link the way a browser navigation does: no token, no cookie.
func openExportLink(t *testing.T, api http.Handler, link exportLink) (int, http.Header, []byte) {
	t.Helper()
	response := request(t, api, http.MethodGet, link.URL, nil, "", nil)
	return response.Code, response.Header(), response.Body.Bytes()
}

func TestExportLinkStartsTheDownloadWithoutASessionAndWorksOnce(t *testing.T) {
	f := newExportFixture(t)
	link := makeExportLink(t, f.api, f.adminToken)
	if !strings.HasPrefix(link.URL, "/api/v1/export/download/") || link.ExpiresInSeconds != 60 {
		t.Fatalf("link=%+v, want a download path and 60 seconds", link)
	}
	code, header, body := openExportLink(t, f.api, link)
	if code != http.StatusOK || header.Get("Content-Type") != "application/zip" || !strings.Contains(header.Get("Content-Disposition"), "colliedesk-export-acme") {
		t.Fatalf("first use: status=%d type=%q disposition=%q, want the zip of the workspace", code, header.Get("Content-Type"), header.Get("Content-Disposition"))
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(unzip(t, body)["manifest.json"], &manifest); err != nil || manifest.Counts["conversations"] != 3 {
		t.Fatalf("manifest counts=%v err=%v, want the 3 conversations of the workspace", manifest.Counts, err)
	}
	if code, _, _ := openExportLink(t, f.api, link); code != http.StatusNotFound {
		t.Fatalf("second use: status=%d, want 404", code)
	}
}

func TestExportLinkIsAdminOnlyAndNeedsASession(t *testing.T) {
	f := newExportFixture(t)
	if got := request(t, f.api, http.MethodPost, "/api/v1/export/link", nil, f.agentToken, nil).Code; got != http.StatusForbidden {
		t.Errorf("agent status=%d, want 403", got)
	}
	if got := request(t, f.api, http.MethodPost, "/api/v1/export/link", nil, "", nil).Code; got != http.StatusUnauthorized {
		t.Errorf("anonymous status=%d, want 401", got)
	}
}

func TestUnknownExportTokenIsNotFound(t *testing.T) {
	f := newExportFixture(t)
	for _, path := range []string{"/api/v1/export/download/nope", "/api/v1/export/download/" + strings.Repeat("a", 43)} {
		if got := request(t, f.api, http.MethodGet, path, nil, "", nil).Code; got != http.StatusNotFound {
			t.Errorf("%s status=%d, want 404", path, got)
		}
	}
}

func TestExportLinkNeverReachesAnotherWorkspace(t *testing.T) {
	f := newExportFixture(t)
	linkA := makeExportLink(t, f.api, f.adminToken)
	linkB := makeExportLink(t, f.api, f.otherToken)
	_, headerB, bodyB := openExportLink(t, f.api, linkB)
	_, headerA, bodyA := openExportLink(t, f.api, linkA)
	if !strings.Contains(headerA.Get("Content-Disposition"), "export-acme") || !strings.Contains(headerB.Get("Content-Disposition"), "export-rival") {
		t.Fatalf("links mixed up: A=%q B=%q", headerA.Get("Content-Disposition"), headerB.Get("Content-Disposition"))
	}
	for name, content := range unzip(t, bodyA) {
		if strings.Contains(string(content), "Rival secret plans") || strings.Contains(string(content), "rival-only body") {
			t.Fatalf("A's export holds B's data in %s", name)
		}
	}
	if !strings.Contains(string(unzipAll(t, bodyB)), "Rival secret plans") {
		t.Fatal("B's export lacks its own data")
	}
}

func unzipAll(t *testing.T, data []byte) []byte {
	t.Helper()
	var all []byte
	for _, content := range unzip(t, data) {
		all = append(all, content...)
	}
	return all
}

func TestANewExportLinkReplacesThePreviousOne(t *testing.T) {
	f := newExportFixture(t)
	first := makeExportLink(t, f.api, f.adminToken)
	second := makeExportLink(t, f.api, f.adminToken)
	if code, _, _ := openExportLink(t, f.api, first); code != http.StatusNotFound {
		t.Fatalf("replaced link: status=%d, want 404", code)
	}
	if code, _, _ := openExportLink(t, f.api, second); code != http.StatusOK {
		t.Fatalf("latest link: status=%d, want 200", code)
	}
}

func TestExportLinkExpiresAfterSixtySeconds(t *testing.T) {
	f := newLimitsFixture(t, true)
	late := makeExportLink(t, f.api, f.adminA)
	f.clock.set(f.clock.Now().Add(61 * time.Second))
	if code, _, _ := openExportLink(t, f.api, late); code != http.StatusNotFound {
		t.Fatalf("link used 61 s after it was made: status=%d, want 404", code)
	}
	inTime := makeExportLink(t, f.api, f.adminA)
	f.clock.set(f.clock.Now().Add(59 * time.Second))
	if code, _, _ := openExportLink(t, f.api, inTime); code != http.StatusOK {
		t.Fatalf("link used 59 s after it was made: status=%d, want 200", code)
	}
}

func TestLockedWorkspaceAdminGetsAnExportLink(t *testing.T) {
	f := newLimitsFixture(t, true)
	f.conversationA(t)
	f.clock.set(trialEnd)
	link := makeExportLink(t, f.api, f.adminA)
	if code, _, body := openExportLink(t, f.api, link); code != http.StatusOK || unzip(t, body)["manifest.json"] == nil {
		t.Fatalf("download for a locked workspace: status=%d, want 200 with a manifest", code)
	}
}

func TestExportLinkRefusesWhatTheExportWouldRefuse(t *testing.T) {
	blocked := startBlockedExport(t, func(config *httpapi.Config) { config.MaxConcurrentExports = 1 })
	same := request(t, blocked.api, http.MethodPost, "/api/v1/export/link", nil, blocked.adminA, nil)
	if same.Code != http.StatusConflict {
		t.Fatalf("link while this workspace exports: status=%d body=%s, want 409", same.Code, same.Body.String())
	}
	busy := request(t, blocked.api, http.MethodPost, "/api/v1/export/link", nil, blocked.adminB, nil)
	if busy.Code != http.StatusTooManyRequests || busy.Header().Get("Retry-After") == "" {
		t.Fatalf("link at the server limit: status=%d Retry-After=%q, want 429 with Retry-After", busy.Code, busy.Header().Get("Retry-After"))
	}
	blocked.unblock()
	<-blocked.firstDone
	makeExportLink(t, blocked.api, blocked.adminB)
}
