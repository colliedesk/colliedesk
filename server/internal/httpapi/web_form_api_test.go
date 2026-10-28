package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const siteOrigin = "https://site.example.com"

type webFormFixture struct {
	api        http.Handler
	pool       *pgxpool.Pool
	clock      *testClock
	adminA     string
	adminB     string
	slugA      string
	slugB      string
	workspaceA string
	workspaceB string
}

func newWebFormFixture(t *testing.T, mutate func(*httpapi.Config)) webFormFixture {
	t.Helper()
	clock := &testClock{now: trialStart}
	api, service, pool, _ := testAPIOnPool(t, testPool(t), func(config *httpapi.Config) {
		config.Now = clock.Now
		if mutate != nil {
			mutate(config)
		}
	})
	service = service.WithClock(clock.Now)
	f := webFormFixture{api: api, pool: pool, clock: clock}
	f.adminA = createSession(t, api, service, "Form Alpha", "admin@form-alpha.example.com")
	f.adminB = createSession(t, api, service, "Form Bravo", "admin@form-bravo.example.com")
	f.workspaceA, f.workspaceB = workspaceID(t, api, f.adminA), workspaceID(t, api, f.adminB)
	f.slugA, f.slugB = workspaceSlug(t, api, f.adminA), workspaceSlug(t, api, f.adminB)
	return f
}

func workspaceSlug(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	var me struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Workspace.Slug
}

type webFormView struct {
	Enabled        bool     `json:"enabled"`
	AllowedOrigins []string `json:"allowed_origins"`
	SubmitPath     string   `json:"submit_path"`
}

type listedConversation struct {
	ID        string `json:"id"`
	ContactID string `json:"contact_id"`
	Subject   string `json:"subject"`
	Channel   string `json:"channel"`
	Tags      []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

func (f webFormFixture) settings(t *testing.T, token string) webFormView {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/channels/web-form", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET web form status=%d body=%s", response.Code, response.Body.String())
	}
	var view webFormView
	decode(t, response, &view)
	return view
}

func (f webFormFixture) put(t *testing.T, token string, enabled bool, origins ...string) *httptest.ResponseRecorder {
	t.Helper()
	if origins == nil {
		origins = []string{}
	}
	return request(t, f.api, http.MethodPut, "/api/v1/channels/web-form", map[string]any{"enabled": enabled, "allowed_origins": origins}, token, nil)
}

func (f webFormFixture) enable(t *testing.T, token string, origins ...string) {
	t.Helper()
	if response := f.put(t, token, true, origins...); response.Code != http.StatusOK {
		t.Fatalf("enable web form status=%d body=%s", response.Code, response.Body.String())
	}
}

// submit posts to the public form the way a browser does: JSON, an Origin header, a client address.
func (f webFormFixture) submit(slug, body, origin, remote string) *httptest.ResponseRecorder {
	return submitForm(f.api, slug, body, origin, remote)
}

func submitForm(api http.Handler, slug, body, origin, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+slug, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	return serve(api, req)
}

func (f webFormFixture) conversations(t *testing.T, token string) []listedConversation {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/conversations?page_size=100", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []listedConversation `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func (f webFormFixture) count(t *testing.T, table, workspace string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT count(*)::text FROM `+table+` WHERE workspace_id=$1`, workspace)
}

func wantAccepted(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusAccepted || strings.TrimSpace(response.Body.String()) != `{"ok":true}` {
		t.Fatalf("status=%d body=%s, want 202 {\"ok\":true}", response.Code, response.Body.String())
	}
}

func TestWebFormAcceptsSubmissionAndOpensAConversation(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	response := f.submit(f.slugA, `{"email":"Reader@Example.com","message":"Please keep me posted.","source":"site:updates","website":""}`, siteOrigin, "")
	wantAccepted(t, response)

	items := f.conversations(t, f.adminA)
	if len(items) != 1 || items[0].Subject != "Web form: site:updates" || items[0].Channel != "web_form" {
		t.Fatalf("conversations=%+v, want one web_form conversation with subject %q", items, "Web form: site:updates")
	}
	if len(items[0].Tags) != 1 || items[0].Tags[0].Name != "web-form" {
		t.Fatalf("tags=%+v, want [web-form]", items[0].Tags)
	}
	var contact struct {
		Email string `json:"email"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/contacts/"+items[0].ContactID, nil, f.adminA, nil), &contact)
	if contact.Email != "reader@example.com" {
		t.Fatalf("contact email=%q, want reader@example.com", contact.Email)
	}
	var messages struct {
		Items []struct {
			Kind string `json:"kind"`
			Body string `json:"body"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations/"+items[0].ID+"/messages", nil, f.adminA, nil), &messages)
	if len(messages.Items) != 1 || messages.Items[0].Kind != "incoming" || messages.Items[0].Body != "Please keep me posted." {
		t.Fatalf("messages=%+v, want one incoming message with the text", messages.Items)
	}
	if outbox := f.count(t, "channel_outbox", f.workspaceA); outbox != "0" {
		t.Fatalf("channel_outbox rows=%s: the form must not send mail to the submitter", outbox)
	}
}

func TestWebFormReusesTheContactAndTagOfARepeatSubmitter(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	wantAccepted(t, f.submit(f.slugA, `{"email":"again@example.com"}`, "", "192.0.2.10:1000"))
	wantAccepted(t, f.submit(f.slugA, `{"email":"AGAIN@example.com","source":"site:pricing"}`, "", "192.0.2.10:1000"))
	if contacts := f.count(t, "contacts", f.workspaceA); contacts != "1" {
		t.Fatalf("contacts=%s, want 1", contacts)
	}
	if tags := f.count(t, "tags", f.workspaceA); tags != "1" {
		t.Fatalf("tags=%s, want 1", tags)
	}
	if conversations := f.conversations(t, f.adminA); len(conversations) != 2 {
		t.Fatalf("conversations=%d, want 2", len(conversations))
	}
}

func TestWebFormWithoutSourceUsesAPlainSubject(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	wantAccepted(t, f.submit(f.slugA, `{"email":"plain@example.com"}`, "", ""))
	if items := f.conversations(t, f.adminA); len(items) != 1 || items[0].Subject != "Web form" {
		t.Fatalf("conversations=%+v, want subject %q", items, "Web form")
	}
}

func TestWebFormIsOffByDefaultAndAnswersLikeAnUnknownWorkspace(t *testing.T) {
	f := newWebFormFixture(t, nil)
	if view := f.settings(t, f.adminA); view.Enabled || len(view.AllowedOrigins) != 0 || view.SubmitPath != "/api/v1/public/forms/"+f.slugA {
		t.Fatalf("default settings=%+v, want off, no origins, submit path of the slug", view)
	}
	disabled := f.submit(f.slugA, `{"email":"a@example.com"}`, "", "")
	unknown := f.submit("no-such-workspace", `{"email":"a@example.com"}`, "", "")
	if disabled.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound || withoutRequestID(disabled.Body.String()) != withoutRequestID(unknown.Body.String()) {
		t.Fatalf("disabled=%d %s unknown=%d %s, want identical 404", disabled.Code, disabled.Body.String(), unknown.Code, unknown.Body.String())
	}
	if conversations := f.count(t, "conversations", f.workspaceA); conversations != "0" {
		t.Fatalf("conversations=%s, want 0", conversations)
	}
}

func TestWebFormCanBeTurnedOffAgainAndStateIsVisible(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	wantAccepted(t, f.submit(f.slugA, `{"email":"on@example.com"}`, siteOrigin, ""))
	if response := f.put(t, f.adminA, false, siteOrigin); response.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", response.Code, response.Body.String())
	}
	if view := f.settings(t, f.adminA); view.Enabled || len(view.AllowedOrigins) != 1 {
		t.Fatalf("settings after disable=%+v, want off with the origin kept", view)
	}
	if response := f.submit(f.slugA, `{"email":"off@example.com"}`, siteOrigin, ""); response.Code != http.StatusNotFound {
		t.Fatalf("submit while off status=%d, want 404", response.Code)
	}
	if conversations := f.count(t, "conversations", f.workspaceA); conversations != "1" {
		t.Fatalf("conversations=%s, want only the one made while on", conversations)
	}
}

func TestWebFormHoneypotAnswersLikeSuccessAndStoresNothing(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	wantAccepted(t, f.submit(f.slugA, `{"email":"bot@example.com","website":"https://spam.example"}`, "", ""))
	for _, table := range []string{"conversations", "contacts", "messages", "tags"} {
		if n := f.count(t, table, f.workspaceA); n != "0" {
			t.Fatalf("%s=%s after a honeypot hit, want 0", table, n)
		}
	}
}

func TestWebFormRejectsInvalidInputWithFieldErrors(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	cases := map[string]struct{ body, field string }{
		"missing email":  {`{}`, "email"},
		"bad email":      {`{"email":"not-an-email"}`, "email"},
		"display name":   {`{"email":"Eve <eve@example.com>"}`, "email"},
		"long message":   {`{"email":"a@example.com","message":"` + strings.Repeat("a", 5001) + `"}`, "message"},
		"long source":    {`{"email":"a@example.com","source":"` + strings.Repeat("s", 101) + `"}`, "source"},
		"long email":     {`{"email":"` + strings.Repeat("a", 250) + `@example.com"}`, "email"},
		"multibyte over": {`{"email":"a@example.com","message":"` + strings.Repeat("é", 5001) + `"}`, "message"},
	}
	client := 0
	for name, tc := range cases {
		client++
		t.Run(name, func(t *testing.T) {
			// Every attempt counts toward the limit, so each case comes from its own address.
			response := f.submit(f.slugA, tc.body, "", fmt.Sprintf("192.0.2.%d:1", 100+client))
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status=%d body=%s, want 422", response.Code, response.Body.String())
			}
			var body struct {
				Error struct {
					Code   string            `json:"code"`
					Fields map[string]string `json:"fields"`
				} `json:"error"`
			}
			decode(t, response, &body)
			if body.Error.Code != "validation_failed" || body.Error.Fields[tc.field] == "" {
				t.Fatalf("error=%+v, want validation_failed with a %q message", body.Error, tc.field)
			}
		})
	}
	// 5000 characters of two bytes each are within the limit: it counts characters, not bytes.
	wantAccepted(t, f.submit(f.slugA, `{"email":"a@example.com","message":"`+strings.Repeat("é", 5000)+`"}`, "", "192.0.2.50:1"))
	if n := f.count(t, "conversations", f.workspaceA); n != "1" {
		t.Fatalf("conversations=%s, want 1: invalid input stores nothing", n)
	}
}

func TestWebFormNamesTheFieldAndTheLimitInTheErrorMessage(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	response := f.submit(f.slugA, `{"email":"a@example.com","message":"`+strings.Repeat("a", 5001)+`"}`, "", "192.0.2.77:1")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s, want 422", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		} `json:"error"`
	}
	decode(t, response, &body)
	const want = "Message is too long: at most 5,000 characters."
	if body.Error.Message != want || body.Error.Fields["message"] != want {
		t.Fatalf("error message=%q fields=%v, want %q in both", body.Error.Message, body.Error.Fields, want)
	}
}

func TestWebFormRefusesBodiesThatAreNotJSON(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	// text/plain is a CORS "simple" content type: a page on another site could send it without a preflight.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+f.slugA, strings.NewReader(`{"email":"a@example.com"}`))
	req.Header.Set("Content-Type", "text/plain")
	if response := serve(f.api, req); response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain status=%d, want 415", response.Code)
	}
	if response := f.submit(f.slugA, `not json`, "", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON status=%d, want 400", response.Code)
	}
	if n := f.count(t, "conversations", f.workspaceA); n != "0" {
		t.Fatalf("conversations=%s, want 0", n)
	}
}

func TestWebFormCORSAllowsOnlyListedOrigins(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)

	preflight := func(slug, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/public/forms/"+slug, nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type")
		return serve(f.api, req)
	}
	allowed := preflight(f.slugA, siteOrigin)
	if allowed.Code != http.StatusNoContent || allowed.Header().Get("Access-Control-Allow-Origin") != siteOrigin ||
		!strings.Contains(allowed.Header().Get("Access-Control-Allow-Methods"), "POST") ||
		!strings.Contains(strings.ToLower(allowed.Header().Get("Access-Control-Allow-Headers")), "content-type") ||
		allowed.Header().Get("Vary") != "Origin" {
		t.Fatalf("allowed preflight=%d headers=%v", allowed.Code, allowed.Header())
	}
	for name, response := range map[string]*httptest.ResponseRecorder{
		"other origin":      preflight(f.slugA, "https://evil.example"),
		"same host, http":   preflight(f.slugA, "http://site.example.com"),
		"other workspace":   preflight(f.slugB, siteOrigin),
		"unknown workspace": preflight("no-such-workspace", siteOrigin),
	} {
		if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("%s: Access-Control-Allow-Origin=%q, want none", name, got)
		}
		if response.Code == http.StatusNoContent {
			t.Fatalf("%s: preflight status=204, want a refusal", name)
		}
	}

	ok := f.submit(f.slugA, `{"email":"cors@example.com"}`, siteOrigin, "")
	if ok.Header().Get("Access-Control-Allow-Origin") != siteOrigin || ok.Header().Get("Vary") != "Origin" {
		t.Fatalf("allowed POST headers=%v, want the origin echoed", ok.Header())
	}
	invalid := f.submit(f.slugA, `{"email":"bad"}`, siteOrigin, "")
	if invalid.Code != http.StatusUnprocessableEntity || invalid.Header().Get("Access-Control-Allow-Origin") != siteOrigin {
		t.Fatalf("error response must carry CORS headers so the page can read it: %d %v", invalid.Code, invalid.Header())
	}
	other := f.submit(f.slugA, `{"email":"cors2@example.com"}`, "https://evil.example", "")
	if other.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin got Access-Control-Allow-Origin=%q", other.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestWebFormRateLimitsPerClientAndWorkspace(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	f.enable(t, f.adminB)
	for i := 0; i < 5; i++ {
		wantAccepted(t, f.submit(f.slugA, `{"email":"rate@example.com"}`, "", "198.51.100.7:4000"))
	}
	limited := f.submit(f.slugA, `{"email":"rate@example.com"}`, "", "198.51.100.7:4001")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth request status=%d headers=%v, want 429 with Retry-After", limited.Code, limited.Header())
	}
	var body apiError
	decode(t, limited, &body)
	if body.Error.Code != "rate_limited" {
		t.Fatalf("error=%+v, want rate_limited", body.Error)
	}
	if n := f.count(t, "conversations", f.workspaceA); n != "5" {
		t.Fatalf("conversations=%s, want 5: the limited request stores nothing", n)
	}
	wantAccepted(t, f.submit(f.slugA, `{"email":"rate@example.com"}`, "", "198.51.100.8:4000"))
	wantAccepted(t, f.submit(f.slugB, `{"email":"rate@example.com"}`, "", "198.51.100.7:4000"))
	f.clock.set(trialStart.Add(61 * time.Second))
	wantAccepted(t, f.submit(f.slugA, `{"email":"rate@example.com"}`, "", "198.51.100.7:4000"))
}

func forwardedSubmit(f webFormFixture, remote, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+f.slugA, strings.NewReader(`{"email":"proxy@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", header)
	req.RemoteAddr = remote
	return serve(f.api, req)
}

func TestWebFormIgnoresForwardedForFromAnUntrustedPeer(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	for i := 1; i <= 5; i++ {
		wantAccepted(t, forwardedSubmit(f, "203.0.113.9:1", "10.0.0."+string(rune('0'+i))))
	}
	if response := forwardedSubmit(f, "203.0.113.9:1", "10.0.0.9"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d, want 429: a spoofed X-Forwarded-For must not mint new buckets", response.Code)
	}
}

func TestWebFormTakesTheClientFromForwardedForBehindATrustedProxy(t *testing.T) {
	f := newWebFormFixture(t, func(config *httpapi.Config) {
		config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}
	})
	f.enable(t, f.adminA)
	for i := 0; i < 5; i++ {
		wantAccepted(t, forwardedSubmit(f, "172.16.0.2:1", "198.51.100.20, 172.16.0.3"))
	}
	if response := forwardedSubmit(f, "172.16.0.2:1", "198.51.100.20"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("same client behind the proxy status=%d, want 429", response.Code)
	}
	// An entry the client wrote to the left of the proxy's own is ignored: the rightmost untrusted address counts.
	if response := forwardedSubmit(f, "172.16.0.2:1", "1.2.3.4, 198.51.100.20"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed leading entry status=%d, want 429", response.Code)
	}
	wantAccepted(t, forwardedSubmit(f, "172.16.0.2:1", "198.51.100.21"))
}

func TestWebFormCannotCrossWorkspaces(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	// B never turned the form on: A's setting does not open it, and a submission to B's slug creates nothing.
	if response := f.submit(f.slugB, `{"email":"x@example.com"}`, siteOrigin, ""); response.Code != http.StatusNotFound {
		t.Fatalf("B's slug status=%d, want 404", response.Code)
	}
	if view := f.settings(t, f.adminB); view.Enabled || len(view.AllowedOrigins) != 0 {
		t.Fatalf("B settings=%+v, want untouched", view)
	}
	wantAccepted(t, f.submit(f.slugA, `{"email":"only-a@example.com"}`, siteOrigin, ""))
	if a, b := f.count(t, "conversations", f.workspaceA), f.count(t, "conversations", f.workspaceB); a != "1" || b != "0" {
		t.Fatalf("conversations A=%s B=%s, want 1 and 0", a, b)
	}
	if got := f.conversations(t, f.adminB); len(got) != 0 {
		t.Fatalf("B sees A's submission: %+v", got)
	}
	// B's own origin list does not touch A's, and B's origin does not work on A's form.
	f.enable(t, f.adminB, "https://other.example.com")
	if view := f.settings(t, f.adminA); len(view.AllowedOrigins) != 1 || view.AllowedOrigins[0] != siteOrigin {
		t.Fatalf("A settings=%+v after B changed theirs", view)
	}
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/public/forms/"+f.slugA, nil)
	req.Header.Set("Origin", "https://other.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	if response := serve(f.api, req); response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("B's origin was allowed on A's form")
	}
}

func TestWebFormSettingsNeedValidOrigins(t *testing.T) {
	f := newWebFormFixture(t, nil)
	if response := request(t, f.api, http.MethodGet, "/api/v1/channels/web-form", nil, "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status=%d, want 401", response.Code)
	}
	for name, origins := range map[string][]string{
		"wildcard":       {"*"},
		"wildcard host":  {"https://*.example.com"},
		"path":           {"https://site.example.com/form"},
		"trailing slash": {"https://site.example.com/"},
		"no scheme":      {"site.example.com"},
		"ftp":            {"ftp://site.example.com"},
		"credentials":    {"https://user@site.example.com"},
		"query":          {"https://site.example.com?x=1"},
		"too many":       manyOrigins(21),
	} {
		if response := f.put(t, f.adminA, true, origins...); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s, want 400", name, response.Code, response.Body.String())
		}
	}
	response := f.put(t, f.adminA, true, "HTTPS://Site.Example.com", "https://site.example.com", "http://localhost:3000")
	if response.Code != http.StatusOK {
		t.Fatalf("valid origins status=%d body=%s", response.Code, response.Body.String())
	}
	var view webFormView
	decode(t, response, &view)
	if len(view.AllowedOrigins) != 2 || view.AllowedOrigins[0] != "https://site.example.com" || view.AllowedOrigins[1] != "http://localhost:3000" {
		t.Fatalf("origins=%v, want lowercased and de-duplicated in order", view.AllowedOrigins)
	}
}

func manyOrigins(n int) []string {
	origins := make([]string, n)
	for i := range origins {
		origins[i] = "https://site" + strings.Repeat("a", i+1) + ".example.com"
	}
	return origins
}

func TestAgentCanSeeButNotChangeTheWebForm(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	invite := request(t, f.api, http.MethodPost, "/api/v1/auth/invites", map[string]any{"email": "agent@form-alpha.example.com", "role_ceiling": "agent"}, f.adminA, nil)
	if invite.Code != http.StatusCreated {
		t.Fatalf("invite status=%d body=%s", invite.Code, invite.Body.String())
	}
	var created struct {
		URL string `json:"url"`
	}
	decode(t, invite, &created)
	var redeemed struct {
		Token string `json:"token"`
	}
	decode(t, request(t, f.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, created.URL)}, "", nil), &redeemed)
	if view := f.settings(t, redeemed.Token); !view.Enabled {
		t.Fatalf("agent view=%+v, want the state visible", view)
	}
	if response := f.put(t, redeemed.Token, false); response.Code != http.StatusForbidden {
		t.Fatalf("agent PUT status=%d, want 403", response.Code)
	}
}

func TestLockedWorkspaceWebFormAnswersLikeAnUnknownWorkspace(t *testing.T) {
	f := newLimitsFixture(t, true)
	slugA, slugB := workspaceSlug(t, f.api, f.adminA), workspaceSlug(t, f.api, f.adminB)
	for _, token := range []string{f.adminA, f.adminB} {
		body := map[string]any{"enabled": true, "allowed_origins": []string{}}
		if response := request(t, f.api, http.MethodPut, "/api/v1/channels/web-form", body, token, nil); response.Code != http.StatusOK {
			t.Fatalf("enable status=%d body=%s", response.Code, response.Body.String())
		}
	}
	post := func(slug, remote string) *httptest.ResponseRecorder {
		return submitForm(f.api, slug, `{"email":"late@example.com"}`, "", remote)
	}
	wantAccepted(t, post(slugA, "192.0.2.1:1"))
	f.upgrade(t, f.workspaceB, "sub_form_bravo")
	f.clock.set(trialEnd)
	locked, unknown := post(slugA, "192.0.2.2:1"), post("no-such-workspace", "192.0.2.3:1")
	if locked.Code != http.StatusNotFound || withoutRequestID(locked.Body.String()) != withoutRequestID(unknown.Body.String()) {
		t.Fatalf("locked=%d %s unknown=%d %s, want the same 404", locked.Code, locked.Body.String(), unknown.Code, unknown.Body.String())
	}
	wantAccepted(t, post(slugB, "192.0.2.4:1"))
	if n := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, f.workspaceA); n != "1" {
		t.Fatalf("locked workspace conversations=%s, want only the one from before the lock", n)
	}
}

// keyedSubmit posts to the public form with the Idempotency-Key a page sends with each press.
func keyedSubmit(api http.Handler, slug, body, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/public/forms/"+slug, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", siteOrigin)
	req.Header.Set("Idempotency-Key", key)
	return serve(api, req)
}

func TestWebFormStoresOneMessageForARepeatedPress(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA, siteOrigin)
	press := `{"email":"twice@example.com","message":"Is the shop open on Sunday?"}`
	wantAccepted(t, keyedSubmit(f.api, f.slugA, press, "press-1"))
	wantAccepted(t, keyedSubmit(f.api, f.slugA, press, "press-1"))
	if messages := f.count(t, "messages", f.workspaceA); messages != "1" {
		t.Fatalf("messages=%s after one press sent twice, want 1", messages)
	}
	// The same key with other words is another submission, not a replay that drops them.
	wantAccepted(t, keyedSubmit(f.api, f.slugA, `{"email":"twice@example.com","message":"And on Monday?"}`, "press-1"))
	// Without a key every submission is stored, as before.
	wantAccepted(t, f.submit(f.slugA, press, siteOrigin, ""))
	if messages := f.count(t, "messages", f.workspaceA); messages != "3" {
		t.Fatalf("messages=%s, want 3: the repeat dropped, the new words and the unkeyed press kept", messages)
	}
	// The same key on another workspace's form names another submission.
	f.enable(t, f.adminB, siteOrigin)
	wantAccepted(t, keyedSubmit(f.api, f.slugB, press, "press-1"))
	if messages := f.count(t, "messages", f.workspaceB); messages != "1" {
		t.Fatalf("workspace B messages=%s, want 1", messages)
	}
	if bad := keyedSubmit(f.api, f.slugA, press, strings.Repeat("k", 201)); bad.Code != http.StatusBadRequest {
		t.Fatalf("a 201-character key: status=%d body=%s, want 400", bad.Code, bad.Body.String())
	}

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/public/forms/"+f.slugA, nil)
	req.Header.Set("Origin", siteOrigin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,idempotency-key")
	if allowed := serve(f.api, req); !strings.Contains(strings.ToLower(allowed.Header().Get("Access-Control-Allow-Headers")), "idempotency-key") {
		t.Fatalf("preflight allows headers %q, want Idempotency-Key among them", allowed.Header().Get("Access-Control-Allow-Headers"))
	}
}
