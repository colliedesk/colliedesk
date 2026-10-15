package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
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
	code, header, _ = openExportLink(t, f.api, link)
	if code != http.StatusSeeOther || header.Get("Location") != "/settings?export_error=link_invalid" {
		t.Fatalf("second use: status=%d Location=%q, want a 303 to /settings?export_error=link_invalid", code, header.Get("Location"))
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
		if got := request(t, f.api, http.MethodGet, path, nil, "", nil).Code; got != http.StatusSeeOther {
			t.Errorf("%s status=%d, want a 303 to the app", path, got)
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
	if code, _, _ := openExportLink(t, f.api, first); code != http.StatusSeeOther {
		t.Fatalf("replaced link: status=%d, want a 303 to the app", code)
	}
	if code, _, _ := openExportLink(t, f.api, second); code != http.StatusOK {
		t.Fatalf("latest link: status=%d, want 200", code)
	}
}

func TestExportLinkExpiresAfterSixtySeconds(t *testing.T) {
	f := newLimitsFixture(t, true)
	late := makeExportLink(t, f.api, f.adminA)
	f.clock.set(f.clock.Now().Add(61 * time.Second))
	if code, _, _ := openExportLink(t, f.api, late); code != http.StatusSeeOther {
		t.Fatalf("link used 61 s after it was made: status=%d, want a 303 to the app", code)
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

func wantExportRedirect(t *testing.T, code int, header http.Header, reason string) {
	t.Helper()
	if code != http.StatusSeeOther || header.Get("Location") != "/settings?export_error="+reason {
		t.Fatalf("status=%d Location=%q, want a 303 to /settings?export_error=%s (the app, never a raw JSON page)", code, header.Get("Location"), reason)
	}
}

func TestDownloadRefusalsSendTheBrowserBackToTheApp(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		f := newExportFixture(t)
		code, header, body := openExportLink(t, f.api, exportLink{URL: "/api/v1/export/download/nope"})
		wantExportRedirect(t, code, header, "link_invalid")
		if strings.Contains(string(body), "{") {
			t.Fatalf("a redirect must not carry a JSON body: %s", body)
		}
	})
	t.Run("expired token", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		link := makeExportLink(t, f.api, f.adminA)
		f.clock.set(f.clock.Now().Add(61 * time.Second))
		code, header, _ := openExportLink(t, f.api, link)
		wantExportRedirect(t, code, header, "link_invalid")
	})
	t.Run("this workspace is exporting", func(t *testing.T) {
		blocked := startBlockedExport(t, nil)
		code, header, _ := openExportLink(t, blocked.api, blocked.linkA)
		wantExportRedirect(t, code, header, "busy")
		blocked.unblock()
		<-blocked.firstDone
		// The refusal did not spend the link: it works once the slot is free.
		if code, _, body := openExportLink(t, blocked.api, blocked.linkA); code != http.StatusOK || unzip(t, body)["manifest.json"] == nil {
			t.Fatalf("link after a busy refusal: status=%d, want 200", code)
		}
	})
	t.Run("server at its export limit", func(t *testing.T) {
		blocked := startBlockedExport(t, func(config *httpapi.Config) { config.MaxConcurrentExports = 1 })
		code, header, _ := openExportLink(t, blocked.api, blocked.linkB)
		wantExportRedirect(t, code, header, "server_busy")
		blocked.unblock()
		<-blocked.firstDone
		if code, _, _ := openExportLink(t, blocked.api, blocked.linkB); code != http.StatusOK {
			t.Fatalf("link after a 429 refusal: status=%d, want 200", code)
		}
	})
	t.Run("another instance holds the workspace export", func(t *testing.T) {
		f := newExportFixture(t)
		link := makeExportLink(t, f.api, f.adminToken)
		conn, err := f.pool.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock(hashtextextended($1,0))`, "export:"+f.workspaceID); err != nil {
			t.Fatal(err)
		}
		code, header, _ := openExportLink(t, f.api, link)
		wantExportRedirect(t, code, header, "busy")
	})
	t.Run("failure before the first byte", func(t *testing.T) {
		f := newExportFixture(t)
		link := makeExportLink(t, f.api, f.adminToken)
		if _, err := f.pool.Exec(context.Background(), `ALTER TABLE agents RENAME TO agents_gone`); err != nil {
			t.Fatal(err)
		}
		code, header, _ := openExportLink(t, f.api, link)
		wantExportRedirect(t, code, header, "failed")
	})
}

// A download manager or antivirus proxy may send HEAD first; that must not spend the link.
func TestHeadOnADownloadLinkIsRefusedAndKeepsTheLink(t *testing.T) {
	f := newExportFixture(t)
	link := makeExportLink(t, f.api, f.adminToken)
	head := request(t, f.api, http.MethodHead, link.URL, nil, "", nil)
	if head.Code != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD status=%d, want 405", head.Code)
	}
	if code, _, _ := openExportLink(t, f.api, link); code != http.StatusOK {
		t.Fatalf("GET after HEAD: status=%d, want 200 (the link must survive)", code)
	}
}

// Two browsers (or a double click) racing on one link: exactly one gets the file.
func TestTwoGetsRacingOnOneLinkStreamTheArchiveOnce(t *testing.T) {
	f := newExportFixture(t)
	link := makeExportLink(t, f.api, f.adminToken)
	const racers = 8
	codes := make(chan int, racers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, _, _ := openExportLink(t, f.api, link)
			codes <- code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	ok, redirected := 0, 0
	for code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusSeeOther:
			redirected++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if ok != 1 || redirected != racers-1 {
		t.Fatalf("%d streams and %d redirects, want exactly one stream", ok, redirected)
	}
}
