package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
)

// waitFor polls done every 10ms for at most 5 seconds.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 5 seconds for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAReaderPageViewReachesGA4AndBotsHeadsPreviewsAndTheTeamSendNothing(t *testing.T) {
	stub := newAnalyticsStub(t, false, nil)
	// One sender sends in queue order, so when the reader's view arrives every earlier one has too.
	d := newAnalyticsDesk(t, "analytics-views", stub, hcanalytics.Options{Senders: 1})
	d.save(t, ga4Settings(true), http.StatusOK)
	wantPage(t, visit(d.site, d.page, "192.0.2.11", "User-Agent", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.12", "User-Agent", browserAgent, "Sec-Purpose", "prefetch"), http.StatusOK, nil, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.13", "User-Agent", browserAgent, "Cookie", "colliedesk_session="+d.admin), http.StatusOK, nil, nil)
	head := httptest.NewRequest(http.MethodHead, d.page, nil)
	head.RemoteAddr, head.Host = "192.0.2.14:4242", testOriginHost
	head.Header.Set("User-Agent", browserAgent)
	if response := serve(d.site, head); response.Code != http.StatusOK {
		t.Fatalf("HEAD status=%d", response.Code)
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/help-center/theme/preview", map[string]any{"page": "article", "theme": map[string]any{}}, d.admin, http.StatusOK, nil)
	wantPage(t, visit(d.site, d.page, "192.0.2.10", "User-Agent", browserAgent), http.StatusOK, nil, nil)
	waitFor(t, "the reader's page view", func() bool { return len(stub.received()) > 0 })
	calls := stub.received()
	if len(calls) != 1 {
		t.Fatalf("GA4 received %d events, want 1: none for a bot, a prefetch, a signed-in team member, a HEAD or a theme preview: %+v", len(calls), calls)
	}
	call := calls[0]
	if call.Path != "/mp/collect" || !strings.Contains(call.Query, "measurement_id=G-TEST1234") || !strings.Contains(call.Query, "api_secret="+secretCanary) {
		t.Fatalf("GA4 request = %+v", call)
	}
	for _, want := range []string{`"name":"page_view"`, `"page_location":"` + testOrigin + d.page + `"`, `"client_id":"`, `Reset a password`} {
		if !strings.Contains(call.Body, want) {
			t.Errorf("GA4 body lacks %s: %s", want, call.Body)
		}
	}
	for _, private := range []string{"192.0.2.10", "Macintosh"} {
		if strings.Contains(call.Body, private) || strings.Contains(call.Query, private) {
			t.Errorf("GA4 received the reader's %q: %+v", private, call)
		}
	}
	if err := d.sender.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := d.settings(t, d.admin); got["last_sent_at"] == nil || got["last_error"] != nil {
		t.Fatalf("status after a send = %v, want last_sent_at and no last_error", got)
	}
}

func TestAPageNeverWaitsOnAHangingAnalyticsServiceAndTheFailureIsShown(t *testing.T) {
	stub := newAnalyticsStub(t, true, nil)
	d := newAnalyticsDesk(t, "analytics-hang", stub, hcanalytics.Options{Timeout: 300 * time.Millisecond, RetryDelays: []time.Duration{10 * time.Millisecond}})
	d.save(t, ga4Settings(true), http.StatusOK)
	durations := make([]time.Duration, 20)
	for index := range durations {
		started := time.Now()
		wantPage(t, visit(d.site, d.page, fmt.Sprintf("192.0.2.%d", 40+index), "User-Agent", browserAgent), http.StatusOK, nil, nil)
		durations[index] = time.Since(started)
	}
	for _, took := range durations {
		if took > 100*time.Millisecond {
			t.Fatalf("a page took %s while GA4 hung, want at most 100ms: %v", took, durations)
		}
	}
	var status map[string]any
	waitFor(t, "the failure to be recorded", func() bool {
		if err := d.sender.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		status = d.settings(t, d.admin)
		return status["last_error"] != nil
	})
	if status["last_error"] != "GA4 did not answer within 300ms." || status["last_error_at"] == nil || status["last_sent_at"] != nil {
		t.Fatalf("status = %v, want the timeout as last_error", status)
	}
	if got := len(stub.received()); got < 2 {
		t.Fatalf("GA4 received %d requests, want the first send and its retry", got)
	}
}

func TestTheHelpCenterPageIsByteIdenticalWithAnalyticsOnAndOff(t *testing.T) {
	stub := newAnalyticsStub(t, false, nil)
	d := newAnalyticsDesk(t, "analytics-same", stub, hcanalytics.Options{})
	pages := []string{d.page, "/help/" + d.slug}
	type served struct{ body, policy string }
	load := func() []served {
		var out []served
		for _, path := range pages {
			response := visit(d.site, path, "192.0.2.60", "User-Agent", browserAgent)
			wantPage(t, response, http.StatusOK, nil, []string{"googletagmanager", "gtag(", "plausible"})
			out = append(out, served{response.Body.String(), response.Header().Get("Content-Security-Policy")})
		}
		return out
	}
	off := load()
	d.save(t, ga4Settings(true), http.StatusOK)
	on := load()
	for index := range pages {
		if on[index].policy != off[index].policy {
			t.Fatalf("%s: Content-Security-Policy changed:\noff: %s\non:  %s", pages[index], off[index].policy, on[index].policy)
		}
		if on[index].body != off[index].body {
			t.Fatalf("%s: the page changed when analytics was turned on", pages[index])
		}
	}
	waitFor(t, "the page views", func() bool { return len(stub.received()) == len(pages) })
}
