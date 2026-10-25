package httpapi_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/google/googletest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/microsoft/microsofttest"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

const (
	microsoftCallback     = testOrigin + "/api/v1/channels/email/microsoft/callback"
	microsoftAdminConsent = testOrigin + "/api/v1/channels/email/microsoft/admin-consent"
)

// microsoftDesk is a workspace on a server with both Google and Microsoft settings, so a channel can move
// from one provider to the other.
type microsoftDesk struct {
	gmailDesk
	ms *microsofttest.Server
}

func newMicrosoftDesk(t *testing.T, name string, change ...func(*httpapi.Config)) microsoftDesk {
	t.Helper()
	google := googletest.New("client-1", "not-a-secret")
	googleServer := google.Start()
	t.Cleanup(googleServer.Close)
	ms := microsofttest.New("ms-client", "ms-secret")
	msServer := ms.Start()
	t.Cleanup(msServer.Close)
	keys, err := tokenseal.ParseKeys("k1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=")
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Google = googleConfig(keys, googleServer.URL)
		config.Microsoft = microsoft.Config{ClientID: "ms-client", ClientSecret: "ms-secret", Tenant: "common", Keys: keys, Base: msServer.URL}
		for _, apply := range change {
			apply(config)
		}
	})
	desk := microsoftDesk{gmailDesk: gmailDesk{api: api, service: service, pool: pool, fake: google}, ms: ms}
	desk.admin = createSession(t, api, service, name, "admin@"+slugOf(name)+".example.com")
	connectEmail(t, api, desk.admin, "support@"+slugOf(name)+".example.com")
	return desk
}

func googleConfig(keys tokenseal.Keys, base string) google.Config {
	return google.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Keys: keys, Base: base}
}

func (d microsoftDesk) startMicrosoft(t *testing.T, session string) string {
	t.Helper()
	response := request(t, d.api, http.MethodPost, "/api/v1/channels/email/microsoft", nil, session, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("start Microsoft status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/channels/email/microsoft", nil, session, nil), response)
	var body struct {
		URL string `json:"url"`
	}
	decode(t, response, &body)
	return body.URL
}

// finishMicrosoft plays Microsoft's redirect of the browser that holds cookie, answering as account of kind.
func (d microsoftDesk) finishMicrosoft(t *testing.T, consentURL, account, kind string, cookie *http.Cookie) string {
	t.Helper()
	target, err := d.ms.Consent(consentURL, account, kind)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(target, microsoftCallback+"?") {
		t.Fatalf("Microsoft redirects to %s, want the callback", target)
	}
	return d.callback(t, strings.TrimPrefix(target, testOrigin), cookie, "")
}

type microsoftView struct {
	SendingFrom *string `json:"sending_from"`
	Gmail       struct {
		Connection *struct {
			Provider string `json:"provider"`
		} `json:"connection"`
	} `json:"gmail"`
	Microsoft struct {
		Available               bool     `json:"available"`
		MissingSettings         []string `json:"missing_settings"`
		RedirectURI             string   `json:"redirect_uri"`
		AdminConsentRedirectURI string   `json:"admin_consent_redirect_uri"`
		AdminConsentURL         *string  `json:"admin_consent_url"`
		LargeAttachmentBytes    int      `json:"large_attachment_bytes"`
		ServerCredentials       bool     `json:"server_credentials_refused"`
		AddressMatches          *bool    `json:"address_matches"`
		Connection              *struct {
			Provider  string  `json:"provider"`
			Address   string  `json:"address"`
			Status    string  `json:"status"`
			ErrorCode *string `json:"error_code"`
		} `json:"connection"`
	} `json:"microsoft"`
}

func (d microsoftDesk) view(t *testing.T, session string) microsoftView {
	t.Helper()
	response := request(t, d.api, http.MethodGet, "/api/v1/channels/email", nil, session, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/channels/email", nil, session, nil), response)
	var view microsoftView
	decode(t, response, &view)
	return view
}

func TestAdminConnectsMicrosoft365AndRepliesAreQueuedForIt(t *testing.T) {
	desk := newMicrosoftDesk(t, "MS Desk")
	before := desk.view(t, desk.admin)
	if !before.Microsoft.Available || before.Microsoft.Connection != nil || before.Microsoft.RedirectURI != microsoftCallback ||
		before.Microsoft.AdminConsentRedirectURI != microsoftAdminConsent || before.Microsoft.LargeAttachmentBytes != 3<<20 {
		t.Fatalf("before = %+v", before.Microsoft)
	}
	// The admin consent link names the channel it returns to, and never the common tenant.
	if before.Microsoft.AdminConsentURL == nil || !strings.Contains(*before.Microsoft.AdminConsentURL, "/organizations/v2.0/adminconsent") ||
		!strings.Contains(*before.Microsoft.AdminConsentURL, "state="+strings.TrimPrefix(desk.page(t, desk.admin), "/settings/channels/")) {
		t.Fatalf("admin consent URL = %v", before.Microsoft.AdminConsentURL)
	}
	consent := desk.startMicrosoft(t, desk.admin)
	parsed, _ := url.Parse(consent)
	if parsed.Query().Get("login_hint") != "support@ms-desk.example.com" || parsed.Query().Get("redirect_uri") != microsoftCallback ||
		!strings.Contains(parsed.Query().Get("scope"), "Mail.Send") || strings.Contains(parsed.Query().Get("scope"), "Mail.Read") {
		t.Fatalf("consent address = %s", consent)
	}
	if location := desk.finishMicrosoft(t, consent, "Support@MS-Desk.example.com", microsofttest.Work, sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&microsoft=connected" {
		t.Fatalf("Location = %s", location)
	}
	after := desk.view(t, desk.admin)
	if after.Microsoft.Connection == nil || after.Microsoft.Connection.Provider != "microsoft" || after.Microsoft.Connection.Address != "support@ms-desk.example.com" ||
		after.Gmail.Connection != nil || after.SendingFrom == nil || *after.SendingFrom != "support@ms-desk.example.com" ||
		after.Microsoft.AddressMatches == nil || !*after.Microsoft.AddressMatches {
		t.Fatalf("after = %+v gmail=%+v sending_from=%v", after.Microsoft, after.Gmail.Connection, after.SendingFrom)
	}
	if account := queryString(t, desk.pool, `SELECT COALESCE(account_id,'') FROM mailbox_connections`); account == "" {
		t.Fatal("the Microsoft account id was not stored")
	}
	contact := create(t, desk.api, desk.admin, "/api/v1/contacts", map[string]any{"name": "Olive", "email": "olive@customer.example"})
	conversation := create(t, desk.api, desk.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hi"})
	create(t, desk.api, desk.admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Hello"})
	if transport := queryString(t, desk.pool, `SELECT transport||'/'||status FROM channel_outbox`); transport != "microsoft/queued" {
		t.Fatalf("outbox row = %s, want microsoft/queued", transport)
	}
	// The Gmail route does not remove a Microsoft connection.
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, desk.admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("DELETE gmail of a Microsoft channel: %d", response.Code)
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/microsoft", nil, desk.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", response.Code, response.Body.String())
	}
	if view := desk.view(t, desk.admin); view.Microsoft.Connection != nil {
		t.Fatalf("still connected: %+v", view.Microsoft.Connection)
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/microsoft", nil, desk.admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("disconnect again: %d", response.Code)
	}
}

func TestAPersonalOutlookAccountConnectsByItsSignInName(t *testing.T) {
	desk := newMicrosoftDesk(t, "Outlook Desk")
	desk.useAddress(t, desk.admin, "help@outlook.com")
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "Help@Outlook.com", microsofttest.Personal, sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&microsoft=connected" {
		t.Fatalf("Location = %s", location)
	}
	if view := desk.view(t, desk.admin); view.Microsoft.Connection == nil || view.Microsoft.Connection.Address != "help@outlook.com" {
		t.Fatalf("connection = %+v", view.Microsoft.Connection)
	}
}

func TestMicrosoftRefusalsAreNamed(t *testing.T) {
	desk := newMicrosoftDesk(t, "MS Refusals")
	page := desk.page(t, desk.admin)
	cookie := sessionCookieOf(desk.admin)
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "", microsofttest.Work, cookie); location != page+"?tab=replies&microsoft_error=denied" {
		t.Errorf("cancelled: %s", location)
	}
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "someone@ms-refusals.example.com", microsofttest.Work, cookie); location != page+"?tab=replies&microsoft_error=address_mismatch" {
		t.Errorf("another mailbox: %s", location)
	}
	desk.ms.Set(func(s *microsofttest.Server) { s.NoMailbox = true })
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@ms-refusals.example.com", microsofttest.Work, cookie); location != page+"?tab=replies&microsoft_error=mailbox_unavailable" {
		t.Errorf("no mailbox: %s", location)
	}
	desk.ms.Set(func(s *microsofttest.Server) { s.NoMailbox, s.GrantScope = false, "openid offline_access User.Read" })
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@ms-refusals.example.com", microsofttest.Work, cookie); location != page+"?tab=replies&microsoft_error=send_not_granted" {
		t.Errorf("no Mail.Send: %s", location)
	}
	desk.ms.Set(func(s *microsofttest.Server) { s.GrantScope, s.OmitRefreshToken = "", true })
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@ms-refusals.example.com", microsofttest.Work, cookie); location != page+"?tab=replies&microsoft_error=no_refresh_token" {
		t.Errorf("no refresh token: %s", location)
	}
	desk.ms.Set(func(s *microsofttest.Server) { s.OmitRefreshToken, s.ClientSecret = false, "rotated-secret" })
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@ms-refusals.example.com", microsofttest.Work, cookie); location != page+"?tab=replies&microsoft_error=server_misconfigured" {
		t.Errorf("the server's client secret refused: %s", location)
	}
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@ms-refusals.example.com", microsofttest.Work, nil); location != "/settings/channels/type/email?tab=replies&microsoft_error=signed_out" {
		t.Errorf("no session: %s", location)
	}
	if location := desk.callback(t, "/api/v1/channels/email/microsoft/callback?state=never-started&code=x", cookie, ""); location != "/settings/channels/type/email?tab=replies&microsoft_error=expired" {
		t.Errorf("unknown state: %s", location)
	}
	if view := desk.view(t, desk.admin); view.Microsoft.Connection != nil {
		t.Fatalf("a refused consent connected: %+v", view.Microsoft.Connection)
	}
}

func TestAMicrosoftConsentStartedInOneWorkspaceNeverConnectsAnother(t *testing.T) {
	desk := newMicrosoftDesk(t, "MS Desk A")
	other := createSession(t, desk.api, desk.service, "MS Desk B", "admin@ms-desk-b.example.com")
	connectEmail(t, desk.api, other, "support@ms-desk-a.example.com")
	target, err := desk.ms.Consent(desk.startMicrosoft(t, desk.admin), "support@ms-desk-a.example.com", microsofttest.Work)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(target, testOrigin)
	if location := desk.callback(t, path, sessionCookieOf(other), ""); location != "/settings/channels/type/email?tab=replies&microsoft_error=expired" {
		t.Fatalf("B finishing A's consent: Location = %s", location)
	}
	if view := desk.view(t, other); view.Microsoft.Connection != nil {
		t.Fatalf("B got a connection: %+v", view.Microsoft.Connection)
	}
	if location := desk.callback(t, path, sessionCookieOf(desk.admin), ""); location != desk.page(t, desk.admin)+"?tab=replies&microsoft=connected" {
		t.Fatalf("A finishing its own consent: Location = %s", location)
	}
	if view := desk.view(t, other); view.Microsoft.Connection != nil {
		t.Fatalf("A's connection shows in B: %+v", view.Microsoft.Connection)
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/microsoft", nil, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("B disconnecting A's Microsoft 365: %d", response.Code)
	}
	if view := desk.view(t, desk.admin); view.Microsoft.Connection == nil {
		t.Fatal("B's disconnect removed A's connection")
	}
}

func TestACallbackFinishesOnlyTheProviderItWasStartedFor(t *testing.T) {
	desk := newMicrosoftDesk(t, "Two Providers")
	gmailConsent := desk.startGmail(t, desk.admin)
	target, err := desk.fake.Consent(gmailConsent, "support@two-providers.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Google's answer replayed at Microsoft's callback: the state is Google's, so it is not found there.
	answer := strings.TrimPrefix(target, gmailCallback)
	if location := desk.callback(t, "/api/v1/channels/email/microsoft/callback"+answer, sessionCookieOf(desk.admin), ""); location != "/settings/channels/type/email?tab=replies&microsoft_error=expired" {
		t.Fatalf("Google consent at the Microsoft callback: Location = %s", location)
	}
	// ...and stays usable at Google's own callback.
	if location := desk.callback(t, strings.TrimPrefix(target, testOrigin), sessionCookieOf(desk.admin), ""); location != desk.page(t, desk.admin)+"?tab=replies&gmail=connected" {
		t.Fatalf("Google consent at its own callback: Location = %s", location)
	}
}

func TestConnectingMicrosoftReplacesGmailAndRevokesItsGrant(t *testing.T) {
	desk := newMicrosoftDesk(t, "Switch Desk")
	cookie := sessionCookieOf(desk.admin)
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "support@switch-desk.example.com", cookie); !strings.HasSuffix(location, "gmail=connected") {
		t.Fatalf("Gmail: %s", location)
	}
	googleTokens := desk.fake.RefreshTokens()
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@switch-desk.example.com", microsofttest.Work, cookie); !strings.HasSuffix(location, "microsoft=connected") {
		t.Fatalf("Microsoft: %s", location)
	}
	for _, token := range googleTokens {
		if !desk.fake.Revoked(token) {
			t.Fatal("the replaced Gmail grant is still alive at Google")
		}
	}
	view := desk.view(t, desk.admin)
	if view.Gmail.Connection != nil || view.Microsoft.Connection == nil {
		t.Fatalf("gmail=%+v microsoft=%+v, want Microsoft only", view.Gmail.Connection, view.Microsoft.Connection)
	}
	// Back to Gmail: the Microsoft connection is replaced (Microsoft cannot revoke it; the token is dropped).
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "support@switch-desk.example.com", cookie); !strings.HasSuffix(location, "gmail=connected") {
		t.Fatalf("Gmail again: %s", location)
	}
	if view := desk.view(t, desk.admin); view.Microsoft.Connection != nil || view.Gmail.Connection == nil {
		t.Fatalf("after switching back: gmail=%+v microsoft=%+v", view.Gmail.Connection, view.Microsoft.Connection)
	}
}

func TestMicrosoftConnectsOnlyOnceAWorkspace(t *testing.T) {
	desk := newMicrosoftDesk(t, "Once Desk")
	cookie := sessionCookieOf(desk.admin)
	defaultID := strings.TrimPrefix(desk.page(t, desk.admin), "/settings/channels/")
	if location := desk.finishMicrosoft(t, desk.startMicrosoft(t, desk.admin), "support@once-desk.example.com", microsofttest.Work, cookie); !strings.HasSuffix(location, "microsoft=connected") {
		t.Fatalf("first: %s", location)
	}
	// The default channel moves to another address and keeps sending through support@ (flagged); a second
	// channel then takes support@, whose mailbox the default still holds.
	desk.useAddress(t, desk.admin, "hello@once-desk.example.com")
	if view := desk.view(t, desk.admin); view.Microsoft.AddressMatches == nil || *view.Microsoft.AddressMatches {
		t.Fatalf("address_matches = %v, want false", view.Microsoft.AddressMatches)
	}
	second := addEmailChannel(t, desk.api, desk.admin, "support@once-desk.example.com")
	response := request(t, desk.api, http.MethodPost, "/api/v1/channels/"+second+"/email/microsoft", nil, desk.admin, nil)
	var consent struct {
		URL string `json:"url"`
	}
	decode(t, response, &consent)
	if hint, _ := url.Parse(consent.URL); hint.Query().Get("login_hint") != "support@once-desk.example.com" {
		t.Fatalf("the second channel's sign-in hints %s", consent.URL)
	}
	location := desk.finishMicrosoft(t, consent.URL, "support@once-desk.example.com", microsofttest.Work, cookie)
	if location != "/settings/channels/"+second+"?tab=replies&microsoft_error=account_in_use&microsoft_channel="+defaultID {
		t.Fatalf("an account another channel sends through: %s", location)
	}
}

func TestAnAgentTokenAsksAPersonToConnectOrDisconnectMicrosoft(t *testing.T) {
	desk := newMicrosoftDesk(t, "Token MS")
	admin := issueToken(t, desk.api, desk.admin, "Ops agent", "admin").Token
	asked := accepted(t, desk.api, http.MethodPost, "/api/v1/channels/email/microsoft", nil, admin)
	approved := approve(t, desk.api, desk.admin, asked.RequestID, nil)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), "oauth2/v2.0/authorize") {
		t.Fatalf("approver gets the sign-in address: %d %s", approved.Code, approved.Body.String())
	}
	if stored := getRequest(t, desk.api, admin, asked.RequestID); strings.Contains(string(stored.Result), "oauth2") {
		t.Fatalf("the token can read the sign-in address: %s", stored.Result)
	}
	accepted(t, desk.api, http.MethodDelete, "/api/v1/channels/email/microsoft", nil, admin)
}

func TestAServerWithoutMicrosoftSettingsSaysWhatIsMissing(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "No Microsoft", "admin@no-microsoft.example.com")
	connectEmail(t, api, admin, "support@no-microsoft.example.com")
	response := request(t, api, http.MethodPost, "/api/v1/channels/email/microsoft", nil, admin, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "microsoft_not_configured") || !strings.Contains(response.Body.String(), "MICROSOFT_OAUTH_CLIENT_ID") {
		t.Fatalf("connect without settings: %d %s", response.Code, response.Body.String())
	}
	var view microsoftView
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/email", nil, admin, nil), &view)
	if view.Microsoft.Available || view.Microsoft.AdminConsentURL != nil || len(view.Microsoft.MissingSettings) != 3 {
		t.Fatalf("settings = %+v", view.Microsoft)
	}
}

// The admin who approves Colliedesk for an organization often has no Colliedesk session: they get a page
// that says what happens next. A signed-in person returns to the channel the link was for. Nothing is
// stored and the tenant Microsoft names is not trusted.
func TestAnAdminConsentReturnsToAPageOrToTheChannel(t *testing.T) {
	desk := newMicrosoftDesk(t, "Admin Consent")
	view := desk.view(t, desk.admin)
	consentURL, err := url.Parse(*view.Microsoft.AdminConsentURL)
	if err != nil {
		t.Fatal(err)
	}
	target, err := desk.ms.AdminConsent(consentURL.Query(), true)
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(target, testOrigin)
	page := request(t, desk.api, http.MethodGet, path, nil, "", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Colliedesk is approved for your organization") ||
		strings.Contains(page.Body.String(), "11111111-2222") || page.Header().Get("X-Frame-Options") != "DENY" || page.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("without a session: %d %v %s", page.Code, page.Header(), page.Body.String())
	}
	if location := desk.callback(t, path, sessionCookieOf(desk.admin), ""); location != desk.page(t, desk.admin)+"?tab=replies&microsoft=admin_consent_done" {
		t.Fatalf("with a session: %s", location)
	}
	refused, err := desk.ms.AdminConsent(consentURL.Query(), false)
	if err != nil {
		t.Fatal(err)
	}
	refusedPath := strings.TrimPrefix(refused, testOrigin)
	if page := request(t, desk.api, http.MethodGet, refusedPath, nil, "", nil); !strings.Contains(page.Body.String(), "The approval was not completed") {
		t.Fatalf("refused without a session: %s", page.Body.String())
	}
	if location := desk.callback(t, refusedPath, sessionCookieOf(desk.admin), ""); location != desk.page(t, desk.admin)+"?tab=replies&microsoft_error=admin_consent_failed" {
		t.Fatalf("refused with a session: %s", location)
	}
	// Another workspace's channel in state: the signed-in person returns to their own default channel.
	other := createSession(t, desk.api, desk.service, "Admin Consent B", "admin@admin-consent-b.example.com")
	connectEmail(t, desk.api, other, "support@admin-consent-b.example.com")
	if location := desk.callback(t, path, sessionCookieOf(other), ""); location != "/settings/channels/type/email?tab=replies&microsoft=admin_consent_done" {
		t.Fatalf("another workspace's channel: %s", location)
	}
	if count := queryString(t, desk.pool, `SELECT count(*)::text FROM mailbox_connections`); count != "0" {
		t.Fatalf("admin consent stored %s connections", count)
	}
}
