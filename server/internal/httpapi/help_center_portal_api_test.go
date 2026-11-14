package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestPortalOffMatchesAMissingPageAndTheSettingStaysWhenOmitted(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Email.MailFromDomain = "mail.example.com"
		config.PortalMailer = &recordingMailer{}
	})
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Portal Off", "admin@portal-off.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Portal Off", true)

	missing := visit(site, "/help/"+slug+"/no-such-page", "203.0.113.20")
	hidden := visit(site, "/help/"+slug+"/requests", "203.0.113.20")
	if hidden.Code != missing.Code || hidden.Body.String() != missing.Body.String() {
		t.Fatalf("portal off GET = %d %q, missing page = %d %q", hidden.Code, hidden.Body.String(), missing.Code, missing.Body.String())
	}
	postMissing := portalForm(t, site, "/help/"+slug+"/no-such", nil, nil, "203.0.113.20")
	postPortal := portalForm(t, site, "/help/"+slug+"/requests", url.Values{"email": {"ada@portal-off.example"}}, nil, "203.0.113.21")
	if postPortal.Code != http.StatusMethodNotAllowed || postMissing.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST portal off = %d, POST missing = %d, want 405", postPortal.Code, postMissing.Code)
	}

	var on struct {
		PortalEnabled bool `json:"portal_enabled"`
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Portal Off", "locale": "en", "portal_enabled": true}, admin, http.StatusOK, &on)
	if !on.PortalEnabled {
		t.Fatal("portal was not turned on")
	}
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Portal Off", "locale": "en"}, admin, http.StatusOK, &on)
	if !on.PortalEnabled {
		t.Fatal("omitting portal_enabled turned the portal off")
	}
	home := visit(site, "/help/"+slug, "203.0.113.20")
	if !strings.Contains(home.Body.String(), "My requests") || !strings.Contains(home.Body.String(), "/help/"+slug+"/requests") {
		t.Fatalf("home page has no portal link:\n%s", home.Body.String())
	}
}

func TestPortalShowsOnlyThatRequestersPublicConversation(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Email.MailFromDomain = "mail.example.com"
		config.PortalMailer = mailer
	})
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Portal Alpha", "admin@portal-alpha.example.com")
	other := createSession(t, api, service, "Portal Bravo", "admin@portal-bravo.example.com")
	slug := workspaceSlug(t, api, admin)
	bravo := workspaceSlug(t, api, other)
	workspace := workspaceID(t, api, admin)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Alpha Help", "locale": "en", "portal_enabled": true}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Bravo Help", "locale": "en", "portal_enabled": true}, other, http.StatusOK, nil)

	ada := signInToPortal(t, site, mailer, "/help/"+slug, "ada@portal-alpha.example")
	if ada.Name != "colliedesk_portal" || !ada.HttpOnly || !ada.Secure || ada.SameSite != http.SameSiteLaxMode || ada.Path != "/help/"+slug {
		t.Fatalf("portal cookie = %+v, want HttpOnly Secure Lax path /help/%s", ada, slug)
	}
	if me := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, "", &http.Cookie{Name: "colliedesk_session", Value: ada.Value}); me.Code != http.StatusUnauthorized {
		t.Fatalf("portal token as an agent session = %d, want 401", me.Code)
	}
	created := portalForm(t, site, "/help/"+slug+"/requests/new", url.Values{"subject": {"Ada needs help"}, "message": {"The pump is loud."}}, ada, "203.0.113.30")
	if created.Code != http.StatusSeeOther {
		t.Fatalf("new request = %d %s", created.Code, created.Body.String())
	}
	ticketPath := created.Header().Get("Location")
	page := portalGet(t, site, ticketPath, ada, "203.0.113.30")
	wantPage(t, page, http.StatusOK, []string{"Ada needs help", "The pump is loud.", "You"}, []string{"NOTE-SECRET-BODY"})

	conversation := queryString(t, pool, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND subject='Ada needs help'`, workspace)
	if _, err := pool.Exec(t.Context(), `INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'note','NOTE-SECRET-BODY'),($1,$2,'outgoing','PUBLIC-ANSWER')`, workspace, conversation); err != nil {
		t.Fatal(err)
	}
	var bobNumber string
	if err := pool.QueryRow(t.Context(), `WITH c AS (
			INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Bob','bob@portal-alpha.example') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject,channel)
		SELECT $1,id,'BOB-PRIVATE-TICKET','email' FROM c RETURNING number::text`, workspace).Scan(&bobNumber); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), `WITH c AS (
			INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Ada','ada@portal-alpha.example') RETURNING id)
		INSERT INTO conversations(workspace_id,contact_id,subject) SELECT $1,id,'BRAVO-SECRET' FROM c`, workspaceID(t, api, other)); err != nil {
		t.Fatal(err)
	}

	shown := portalGet(t, site, ticketPath, ada, "203.0.113.30")
	wantPage(t, shown, http.StatusOK, []string{"PUBLIC-ANSWER", "Ada needs help"}, []string{"NOTE-SECRET-BODY", "BOB-PRIVATE-TICKET", "BRAVO-SECRET"})
	messages := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, admin, nil)
	if messages.Code != http.StatusOK || !strings.Contains(messages.Body.String(), "NOTE-SECRET-BODY") {
		t.Fatalf("agent timeline = %d %s, want the note", messages.Code, messages.Body.String())
	}
	stolen := portalGet(t, site, "/help/"+slug+"/requests/ticket/"+bobNumber, ada, "203.0.113.30")
	wantPage(t, stolen, http.StatusNotFound, nil, []string{"BOB-PRIVATE-TICKET", "NOTE-SECRET-BODY", "The pump is loud."})

	list := portalGet(t, site, "/help/"+slug+"/requests", ada, "203.0.113.30")
	wantPage(t, list, http.StatusOK, []string{"Ada needs help"}, []string{"BOB-PRIVATE-TICKET", "BRAVO-SECRET", "NOTE-SECRET-BODY"})
	crossed := portalGet(t, site, "/help/"+bravo+"/requests", ada, "203.0.113.30")
	wantPage(t, crossed, http.StatusOK, []string{"See your requests"}, []string{"Ada needs help", "BRAVO-SECRET", "The pump is loud."})

	reply := portalForm(t, site, ticketPath+"/reply", url.Values{"message": {"It is still loud."}}, ada, "203.0.113.30")
	if reply.Code != http.StatusSeeOther {
		t.Fatalf("reply = %d %s", reply.Code, reply.Body.String())
	}
	again := portalGet(t, site, ticketPath, ada, "203.0.113.30")
	wantPage(t, again, http.StatusOK, []string{"It is still loud."}, []string{"NOTE-SECRET-BODY"})
	if got := queryString(t, pool, `SELECT kind||' '||(email_meta->>'via')||' '||(email_meta->>'from') FROM messages WHERE workspace_id=$1 AND body='It is still loud.'`, workspace); got != "incoming portal ada@portal-alpha.example" {
		t.Fatalf("stored reply = %q", got)
	}

	out := portalForm(t, site, "/help/"+slug+"/requests/sign-out", nil, ada, "203.0.113.30")
	if out.Code != http.StatusSeeOther {
		t.Fatalf("sign out = %d", out.Code)
	}
	after := portalGet(t, site, "/help/"+slug+"/requests", ada, "203.0.113.30")
	wantPage(t, after, http.StatusOK, []string{"See your requests"}, []string{"Ada needs help", "The pump is loud."})
}

func TestPortalLinkIsSpentOnceAndAHoneypotStoresNothing(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Email.MailFromDomain = "mail.example.com"
		config.PortalMailer = mailer
	})
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Portal Links", "admin@portal-links.example.com")
	slug := workspaceSlug(t, api, admin)
	workspace := workspaceID(t, api, admin)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Links", "locale": "en", "portal_enabled": true}, admin, http.StatusOK, nil)
	home := "/help/" + slug + "/requests"

	trap := portalForm(t, site, home, url.Values{"email": {"bot@portal-links.example"}, "website": {"http://spam.example"}}, nil, "203.0.113.40")
	if trap.Code != http.StatusSeeOther || len(mailer.all()) != 0 {
		t.Fatalf("honeypot = %d, emails %d", trap.Code, len(mailer.all()))
	}
	if n := queryString(t, pool, `SELECT count(*)::text FROM portal_links WHERE workspace_id=$1`, workspace); n != "0" {
		t.Fatalf("honeypot stored %s links", n)
	}

	first := portalForm(t, site, home, url.Values{"email": {"ada@portal-links.example"}}, nil, "203.0.113.41")
	if first.Code != http.StatusSeeOther || len(mailer.all()) != 1 {
		t.Fatalf("sign-in email = %d, emails %d, body %s", first.Code, len(mailer.all()), first.Body.String())
	}
	path, token := portalLink(t, mailer.all()[0].Message)
	stored := queryString(t, pool, `SELECT encode(token_hash,'hex') FROM portal_links WHERE workspace_id=$1 AND email='ada@portal-links.example'`, workspace)
	sum := sha256.Sum256([]byte(token))
	if stored != hex.EncodeToString(sum[:]) || strings.Contains(stored, token) {
		t.Fatalf("stored hash %s is not the token's sha256", stored)
	}

	page := portalGet(t, site, path, nil, "203.0.113.41")
	wantPage(t, page, http.StatusOK, []string{"Continue"}, nil)
	if n := queryString(t, pool, `SELECT count(*)::text FROM portal_sessions WHERE workspace_id=$1`, workspace); n != "0" {
		t.Fatalf("opening the link spent it, sessions=%s", n)
	}
	spent := portalForm(t, site, path, nil, nil, "203.0.113.41")
	if spent.Code != http.StatusSeeOther {
		t.Fatalf("continue = %d %s", spent.Code, spent.Body.String())
	}
	again := portalForm(t, site, path, nil, nil, "203.0.113.41")
	wantPage(t, again, http.StatusOK, []string{"already used"}, nil)

	for i := 0; i < 4; i++ {
		if got := portalForm(t, site, home, url.Values{"email": {"ada@portal-links.example"}}, nil, "203.0.113.41"); got.Code != http.StatusSeeOther {
			t.Fatalf("sign-in %d = %d %s", i, got.Code, got.Body.String())
		}
	}
	limited := portalForm(t, site, home, url.Values{"email": {"ada@portal-links.example"}}, nil, "203.0.113.41")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit = %d retry %q", limited.Code, limited.Header().Get("Retry-After"))
	}
}

func TestTrialCanReplyToAPortalRequest(t *testing.T) {
	mailer := &recordingMailer{}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		config.Email.MailFromDomain = "mail.example.com"
		config.PortalMailer = mailer
	})
	slug := workspaceSlug(t, f.api, f.adminA)
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Trial Help", "locale": "en", "portal_enabled": true}, f.adminA, http.StatusOK, nil)
	site := helpCenterSite(f.api)
	cookie := signInToPortal(t, site, mailer, "/help/"+slug, "customer@trial-portal.example")
	created := portalForm(t, site, "/help/"+slug+"/requests/new", url.Values{"subject": {"Trial question"}, "message": {"Hello from the portal."}}, cookie, "203.0.113.50")
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	conversation := queryString(t, f.pool, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND subject='Trial question'`, f.workspaceA)
	f.wantReplyQueued(t, conversation, "trial reply to a portal request")
}

func signInToPortal(t *testing.T, site http.Handler, mailer *recordingMailer, home, address string) *http.Cookie {
	t.Helper()
	before := len(mailer.all())
	sent := portalForm(t, site, home+"/requests", url.Values{"email": {address}}, nil, "203.0.113.77")
	if sent.Code != http.StatusSeeOther {
		t.Fatalf("request link = %d %s", sent.Code, sent.Body.String())
	}
	messages := mailer.all()
	if len(messages) != before+1 {
		t.Fatalf("emails %d, want %d", len(messages), before+1)
	}
	path, _ := portalLink(t, messages[len(messages)-1].Message)
	if !strings.HasPrefix(path, home+"/requests/continue/") {
		t.Fatalf("link path %s, want under %s", path, home)
	}
	opened := portalGet(t, site, path, nil, "203.0.113.77")
	wantPage(t, opened, http.StatusOK, []string{"Continue"}, nil)
	continued := portalForm(t, site, path, nil, nil, "203.0.113.77")
	if continued.Code != http.StatusSeeOther {
		t.Fatalf("continue = %d %s", continued.Code, continued.Body.String())
	}
	cookie := continued.Result().Cookies()
	for _, item := range cookie {
		if item.Name == "colliedesk_portal" && item.Value != "" {
			return item
		}
	}
	t.Fatalf("no portal cookie on %+v", continued.Header())
	return nil
}

func portalLink(t *testing.T, raw string) (path, token string) {
	t.Helper()
	message, err := netmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(message.Body))
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`https://desk\.example\.com(/help/[^\s]+/requests/continue/([A-Za-z0-9_-]+))`).FindStringSubmatch(string(body))
	if found == nil {
		t.Fatalf("no sign-in link in %s", body)
	}
	return found[1], found[2]
}

func portalGet(t *testing.T, site http.Handler, path string, cookie *http.Cookie, client string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host, req.RemoteAddr = testOriginHost, client+":4242"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return serve(site, req)
}

func portalForm(t *testing.T, site http.Handler, path string, form url.Values, cookie *http.Cookie, client string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Host, req.RemoteAddr = testOriginHost, client+":4242"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testOrigin)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return serve(site, req)
}
