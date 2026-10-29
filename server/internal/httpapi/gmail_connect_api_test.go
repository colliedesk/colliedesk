package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/google/googletest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

const gmailCallback = testOrigin + "/api/v1/channels/email/gmail/callback"

type gmailDesk struct {
	api     http.Handler
	service *auth.Service
	pool    *pgxpool.Pool
	fake    *googletest.Server
	admin   string
}

func newGmailDesk(t *testing.T, name string) gmailDesk {
	t.Helper()
	fake := googletest.New("client-1", "not-a-secret")
	server := fake.Start()
	t.Cleanup(server.Close)
	keys, err := tokenseal.ParseKeys("k1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=")
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Google = google.Config{ClientID: "client-1", ClientSecret: "not-a-secret", Keys: keys, Base: server.URL}
	})
	desk := gmailDesk{api: api, service: service, pool: pool, fake: fake}
	desk.admin = createSession(t, api, service, name, "admin@"+slugOf(name)+".example.com")
	connectEmail(t, api, desk.admin, "support@"+slugOf(name)+".example.com")
	return desk
}

// useAddress changes the support address of the session's default email channel, so a Gmail account of
// that address may be connected to it.
func (d gmailDesk) useAddress(t *testing.T, session, address string) {
	t.Helper()
	if response := request(t, d.api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": address}, session, nil); response.Code != http.StatusOK {
		t.Fatalf("support address %s: %d %s", address, response.Code, response.Body.String())
	}
}

// page is the settings page of the session's default email channel, where a consent for it returns.
func (d gmailDesk) page(t *testing.T, session string) string {
	t.Helper()
	var view struct {
		Channel struct {
			ChannelID string `json:"channel_id"`
		} `json:"channel"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/channels/email", nil, session, nil), &view)
	return "/settings/channels/" + view.Channel.ChannelID
}

func slugOf(name string) string { return strings.ToLower(strings.ReplaceAll(name, " ", "-")) }

func sessionCookieOf(token string) *http.Cookie {
	return &http.Cookie{Name: "colliedesk_session", Value: token}
}

// startGmail asks for the consent address as the person with this session.
func (d gmailDesk) startGmail(t *testing.T, session string) string {
	t.Helper()
	response := request(t, d.api, http.MethodPost, "/api/v1/channels/email/gmail", nil, session, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("start Gmail status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/channels/email/gmail", nil, session, nil), response)
	var body struct {
		URL string `json:"url"`
	}
	decode(t, response, &body)
	return body.URL
}

// finish plays Google's redirect of the browser that holds cookie (nil for none), answering the consent as account.
func (d gmailDesk) finish(t *testing.T, consentURL, account string, cookie *http.Cookie) string {
	t.Helper()
	target, err := d.fake.Consent(consentURL, account)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(target, gmailCallback+"?") {
		t.Fatalf("Google redirects to %s, want the callback", target)
	}
	return d.callback(t, strings.TrimPrefix(target, testOrigin), cookie, "")
}

func (d gmailDesk) callback(t *testing.T, path string, cookie *http.Cookie, bearer string) string {
	t.Helper()
	response := request(t, d.api, http.MethodGet, path, nil, bearer, cookie)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("callback status=%d body=%s, want 303", response.Code, response.Body.String())
	}
	return response.Header().Get("Location")
}

type gmailView struct {
	SendingFrom *string `json:"sending_from"`
	Gmail       struct {
		Available       bool     `json:"available"`
		MissingSettings []string `json:"missing_settings"`
		OffReason       *string  `json:"off_reason"`
		RedirectURI     string   `json:"redirect_uri"`
		Connection      *struct {
			Address   string  `json:"address"`
			Status    string  `json:"status"`
			ErrorCode *string `json:"error_code"`
		} `json:"connection"`
	} `json:"gmail"`
}

func (d gmailDesk) settings(t *testing.T, session string) gmailView {
	t.Helper()
	response := request(t, d.api, http.MethodGet, "/api/v1/channels/email", nil, session, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("settings status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/channels/email", nil, session, nil), response)
	var view gmailView
	decode(t, response, &view)
	return view
}

func TestAdminConnectsGmailAndRepliesAreQueuedForIt(t *testing.T) {
	desk := newGmailDesk(t, "Gmail Desk")
	before := desk.settings(t, desk.admin)
	if !before.Gmail.Available || before.Gmail.Connection != nil || before.Gmail.RedirectURI != gmailCallback {
		t.Fatalf("before = %+v", before.Gmail)
	}
	consent := desk.startGmail(t, desk.admin)
	if !strings.Contains(consent, url.QueryEscape(gmailCallback)) || !strings.Contains(consent, "gmail.send") {
		t.Fatalf("consent address = %s", consent)
	}
	if location := desk.finish(t, consent, "Support@Gmail-Desk.example.com", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail=connected" {
		t.Fatalf("Location = %s", location)
	}
	after := desk.settings(t, desk.admin)
	if after.Gmail.Connection == nil || after.Gmail.Connection.Address != "support@gmail-desk.example.com" || after.Gmail.Connection.Status != "connected" ||
		after.SendingFrom == nil || *after.SendingFrom != "support@gmail-desk.example.com" {
		t.Fatalf("after = %+v sending_from=%v", after.Gmail.Connection, after.SendingFrom)
	}
	// The stored grant is sealed: the refresh token is nowhere in the row.
	for _, token := range desk.fake.RefreshTokens() {
		var plain bool
		if err := desk.pool.QueryRow(context.Background(), `SELECT position($1::bytea in refresh_token_sealed)>0 FROM mailbox_connections`, []byte(token)).Scan(&plain); err != nil || plain {
			t.Fatalf("refresh token stored in clear (or query failed: %v)", err)
		}
	}
	contact := create(t, desk.api, desk.admin, "/api/v1/contacts", map[string]any{"name": "Olive", "email": "olive@customer.example"})
	conversation := create(t, desk.api, desk.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hi"})
	create(t, desk.api, desk.admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Hello"})
	if transport := queryString(t, desk.pool, `SELECT transport||'/'||status FROM channel_outbox`); transport != "gmail/queued" {
		t.Fatalf("outbox row = %s, want gmail/queued", transport)
	}
	// The connected address is ours: copying it would send the reply to our own mailbox.
	copied := request(t, desk.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "Again", "cc": []string{"Support+x@gmail-desk.example.com"}}, desk.admin, nil)
	if copied.Code != http.StatusBadRequest {
		t.Fatalf("Cc of the connected Gmail address: %d %s", copied.Code, copied.Body.String())
	}
}

func TestAServerWithoutGoogleSettingsSaysWhatIsMissing(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "No Google", "admin@no-google.example.com")
	connectEmail(t, api, admin, "support@no-google.example.com")
	desk := gmailDesk{api: api, admin: admin}
	view := desk.settings(t, admin)
	// The workspace hears why without a setting's name; whoever runs the server reads the names in the log.
	if view.Gmail.Available || len(view.Gmail.MissingSettings) != 0 || view.Gmail.OffReason == nil || *view.Gmail.OffReason != "not_set_up" {
		t.Fatalf("gmail = %+v", view.Gmail)
	}
	logs := captureLogs(t)
	response := request(t, api, http.MethodPost, "/api/v1/channels/email/gmail", nil, admin, nil)
	wantMissingSettingsLogged(t, logs, "google", "GOOGLE_OAUTH_CLIENT_ID,GOOGLE_OAUTH_CLIENT_SECRET,MAILBOX_TOKEN_KEYS")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "gmail_not_configured") || strings.Contains(response.Body.String(), "GOOGLE_OAUTH") {
		t.Fatalf("start without settings: %d %s", response.Code, response.Body.String())
	}
}

// The operator removed the Google settings after an admin connected Gmail: as with Gmail switched off, the
// reply is kept for Gmail (never handed to SMTP) and the outbox holds it with the reason until the settings
// are back (emailoutbox TestAReplyWaitsWhileTheServerMissesAGmailSettingAndIsSentOnceItIsBack).
func TestAConnectionTheServerCanNoLongerUseKeepsRepliesForIt(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Lost Google", "admin@lost-google.example.com")
	connectEmail(t, api, admin, "support@lost-google.example.com")
	st := store.New(pool)
	channel, err := st.GetEmailChannel(context.Background(), workspaceID(t, api, admin))
	if err != nil || channel == nil {
		t.Fatalf("email channel: %v %v", channel, err)
	}
	if _, _, err := st.SaveMailboxConnection(context.Background(), workspaceID(t, api, admin), channel.ChannelID, "", "support@lost-google.example.com", google.ScopeGmailSend,
		store.SealedGrant{KeyID: "k1", Sealed: []byte("sealed-with-a-key-this-server-lacks"), Provider: domain.MailboxGoogle}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	desk := gmailDesk{api: api, admin: admin}
	if view := desk.settings(t, admin); view.SendingFrom != nil {
		t.Fatalf("sending_from = %q, want null: this server cannot send through Gmail", *view.SendingFrom)
	}
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Olive", "email": "olive@customer.example"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hi"})
	create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Hello"})
	if row := queryString(t, pool, `SELECT status||' '||transport FROM channel_outbox`); row != "queued gmail" {
		t.Fatalf("outbox row = %q, want queued for Gmail", row)
	}
}

// Pressing Connect again and again keeps only the latest few consents a person started; the newest still works.
func TestStartingGmailRepeatedlyKeepsOnlyTheLatestConsents(t *testing.T) {
	desk := newGmailDesk(t, "Many Starts")
	var last string
	for range 8 {
		last = desk.startGmail(t, desk.admin)
	}
	if count := queryString(t, desk.pool, `SELECT count(*)::text FROM mailbox_oauth_states`); count != "5" {
		t.Fatalf("consent states kept = %s, want 5", count)
	}
	if location := desk.finish(t, last, "support@many-starts.example.com", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail=connected" {
		t.Fatalf("newest consent: %s", location)
	}
}

func TestGmailNeedsTheEmailChannelFirst(t *testing.T) {
	desk := newGmailDesk(t, "Channel First")
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email", nil, desk.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect channel: %d", response.Code)
	}
	response := request(t, desk.api, http.MethodPost, "/api/v1/channels/email/gmail", nil, desk.admin, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "email_channel_missing") {
		t.Fatalf("start without a channel: %d %s", response.Code, response.Body.String())
	}
}

func TestTheCallbackFinishesOnlyForThePersonWhoStartedIt(t *testing.T) {
	desk := newGmailDesk(t, "Callback Desk")
	consent := desk.startGmail(t, desk.admin)
	// A bearer token is not a browser session.
	target, _ := desk.fake.Consent(consent, "a@callback-desk.example.com")
	if location := desk.callback(t, strings.TrimPrefix(target, testOrigin), nil, desk.admin); location != "/settings/channels/type/email?tab=replies&gmail_error=signed_out" {
		t.Fatalf("bearer callback Location = %s", location)
	}
	// Another admin of the same workspace cannot finish someone else's consent.
	_, link := createContractInvite(t, desk.api, desk.admin, "second@callback-desk.example.com", "admin")
	redeemed := request(t, desk.api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	var second struct {
		Token string `json:"token"`
	}
	decode(t, redeemed, &second)
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "a@callback-desk.example.com", sessionCookieOf(second.Token)); location != "/settings/channels/type/email?tab=replies&gmail_error=wrong_person" {
		t.Fatalf("other admin Location = %s", location)
	}
	// An agent (not an admin) cannot start or finish.
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@callback-desk.example.com")
	if response := request(t, desk.api, http.MethodPost, "/api/v1/channels/email/gmail", nil, agent, nil); response.Code != http.StatusForbidden {
		t.Fatalf("agent start: %d", response.Code)
	}
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "a@callback-desk.example.com", sessionCookieOf(agent)); location != "/settings/channels/type/email?tab=replies&gmail_error=forbidden" {
		t.Fatalf("agent callback Location = %s", location)
	}
	// Denied at Google, then the same state again: both refused, nothing connected.
	denied := desk.startGmail(t, desk.admin)
	if location := desk.finish(t, denied, "", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail_error=denied" {
		t.Fatalf("denied Location = %s", location)
	}
	if location := desk.finish(t, denied, "a@callback-desk.example.com", sessionCookieOf(desk.admin)); location != "/settings/channels/type/email?tab=replies&gmail_error=expired" {
		t.Fatalf("reused state Location = %s", location)
	}
	if location := desk.callback(t, "/api/v1/channels/email/gmail/callback?code=x&state=made-up", sessionCookieOf(desk.admin), ""); location != "/settings/channels/type/email?tab=replies&gmail_error=expired" {
		t.Fatalf("made-up state Location = %s", location)
	}
	if view := desk.settings(t, desk.admin); view.Gmail.Connection != nil {
		t.Fatalf("a refused callback connected Gmail: %+v", view.Gmail.Connection)
	}
}

func TestAConsentStartedInOneWorkspaceNeverConnectsAnother(t *testing.T) {
	desk := newGmailDesk(t, "Desk A")
	other := createSession(t, desk.api, desk.service, "Desk B", "admin@desk-b.example.com")
	connectEmail(t, desk.api, other, "support@desk-b.example.com")
	consent := desk.startGmail(t, desk.admin)
	target, err := desk.fake.Consent(consent, "support@desk-a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimPrefix(target, testOrigin)
	if location := desk.callback(t, path, sessionCookieOf(other), ""); location != "/settings/channels/type/email?tab=replies&gmail_error=expired" {
		t.Fatalf("B finishing A's consent: Location = %s", location)
	}
	if view := desk.settings(t, other); view.Gmail.Connection != nil {
		t.Fatalf("B got a connection: %+v", view.Gmail.Connection)
	}
	// The state was looked up in B only, so A can still finish its own consent.
	if location := desk.callback(t, path, sessionCookieOf(desk.admin), ""); location != desk.page(t, desk.admin)+"?tab=replies&gmail=connected" {
		t.Fatalf("A finishing its own consent: Location = %s", location)
	}
	if view := desk.settings(t, other); view.Gmail.Connection != nil {
		t.Fatalf("A's connection shows in B: %+v", view.Gmail.Connection)
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("B disconnecting A's Gmail: %d", response.Code)
	}
	if view := desk.settings(t, desk.admin); view.Gmail.Connection == nil {
		t.Fatal("B's disconnect removed A's connection")
	}
}

func TestGoogleRefusalsAreNamedNotEchoed(t *testing.T) {
	desk := newGmailDesk(t, "Refusals")
	desk.fake.Set(func(s *googletest.Server) { s.GrantScope = "openid email" })
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "a@refusals.example.com", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail_error=send_not_granted" {
		t.Fatalf("unticked send: %s", location)
	}
	desk.fake.Set(func(s *googletest.Server) { s.GrantScope = ""; s.OmitRefreshToken = true })
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "a@refusals.example.com", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail_error=no_refresh_token" {
		t.Fatalf("no refresh token: %s", location)
	}
	desk.fake.Set(func(s *googletest.Server) { s.OmitRefreshToken = false; s.TokenDown = true })
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "a@refusals.example.com", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail_error=google_unavailable" {
		t.Fatalf("token endpoint down: %s", location)
	}
	if view := desk.settings(t, desk.admin); view.Gmail.Connection != nil {
		t.Fatalf("connected anyway: %+v", view.Gmail.Connection)
	}
}

func TestDisconnectAndReconnectRevokeTheGrantTheyDrop(t *testing.T) {
	desk := newGmailDesk(t, "Reconnect")
	desk.useAddress(t, desk.admin, "first@reconnect.example.com")
	desk.finish(t, desk.startGmail(t, desk.admin), "first@reconnect.example.com", sessionCookieOf(desk.admin))
	first := desk.fake.RefreshTokens()
	// Reconnecting with another account (the channel moved to its address) replaces the first and revokes its grant.
	desk.useAddress(t, desk.admin, "second@reconnect.example.com")
	desk.finish(t, desk.startGmail(t, desk.admin), "second@reconnect.example.com", sessionCookieOf(desk.admin))
	if view := desk.settings(t, desk.admin); view.Gmail.Connection == nil || view.Gmail.Connection.Address != "second@reconnect.example.com" {
		t.Fatalf("after reconnect: %+v", view.Gmail.Connection)
	}
	if len(first) != 1 || !desk.fake.Revoked(first[0]) {
		t.Fatal("the replaced grant was not revoked at Google")
	}
	response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, desk.admin, nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("disconnect: %d %s", response.Code, response.Body.String())
	}
	for _, token := range desk.fake.RefreshTokens() {
		if !desk.fake.Revoked(token) {
			t.Fatal("disconnect left a grant working at Google")
		}
	}
	view := desk.settings(t, desk.admin)
	if view.Gmail.Connection != nil || (view.SendingFrom != nil && strings.Contains(*view.SendingFrom, "reconnect.example.com") && *view.SendingFrom != "second@reconnect.example.com") {
		t.Fatalf("after disconnect: %+v %v", view.Gmail.Connection, view.SendingFrom)
	}
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, desk.admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("second disconnect: %d", response.Code)
	}
	// Connecting again works after a disconnect, and removing the whole channel revokes too.
	desk.useAddress(t, desk.admin, "third@reconnect.example.com")
	desk.finish(t, desk.startGmail(t, desk.admin), "third@reconnect.example.com", sessionCookieOf(desk.admin))
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email", nil, desk.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect channel: %d", response.Code)
	}
	for _, token := range desk.fake.RefreshTokens() {
		if !desk.fake.Revoked(token) {
			t.Fatal("removing the channel left the Gmail grant working at Google")
		}
	}
}

// Google revokes an account's whole grant to the project, so dropping the account in one workspace must not
// revoke it while another workspace still sends from it; the last one to drop it revokes.
func TestDroppingAGmailAccountAnotherWorkspaceStillUsesDoesNotRevokeIt(t *testing.T) {
	desk := newGmailDesk(t, "Shared A")
	other := createSession(t, desk.api, desk.service, "Shared B", "admin@shared-b.example.com")
	connectEmail(t, desk.api, other, "team@shared.example.com")
	desk.useAddress(t, desk.admin, "team@shared.example.com")
	desk.finish(t, desk.startGmail(t, desk.admin), "team@shared.example.com", sessionCookieOf(desk.admin))
	desk.finish(t, desk.startGmail(t, other), "team@shared.example.com", sessionCookieOf(other))
	anyRevoked := func() bool {
		for _, token := range desk.fake.RefreshTokens() {
			if desk.fake.Revoked(token) {
				return true
			}
		}
		return false
	}
	allRevoked := func() bool {
		for _, token := range desk.fake.RefreshTokens() {
			if !desk.fake.Revoked(token) {
				return false
			}
		}
		return true
	}
	// A switches to another account, then disconnects, then removes its channel: B keeps sending.
	desk.useAddress(t, desk.admin, "elsewhere@shared.example.com")
	desk.finish(t, desk.startGmail(t, desk.admin), "elsewhere@shared.example.com", sessionCookieOf(desk.admin))
	if anyRevoked() {
		t.Fatal("switching accounts in A revoked the account B still uses")
	}
	desk.useAddress(t, desk.admin, "team@shared.example.com")
	desk.finish(t, desk.startGmail(t, desk.admin), "team@shared.example.com", sessionCookieOf(desk.admin))
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, desk.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect A: %d", response.Code)
	}
	desk.finish(t, desk.startGmail(t, desk.admin), "team@shared.example.com", sessionCookieOf(desk.admin))
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email", nil, desk.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove A's channel: %d", response.Code)
	}
	for _, token := range desk.fake.RefreshTokens() {
		if desk.fake.Revoked(token) && desk.fake.AccountOf(token) == "team@shared.example.com" {
			t.Fatal("A dropping the shared account revoked B's grant")
		}
	}
	// B is the last holder: its disconnect revokes the account (every token of it, as Google does).
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, other, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect B: %d", response.Code)
	}
	if !allRevoked() {
		t.Fatal("the last workspace to drop the account left its grant alive at Google")
	}
}

// Two workspaces dropping a shared account at the same moment: each removes its own row before it asks
// who else holds the account, so at least one of them revokes.
func TestTwoWorkspacesDisconnectingTogetherStillRevoke(t *testing.T) {
	for round := range 5 {
		t.Run("round"+strings.Repeat("i", round+1), func(t *testing.T) { disconnectTogether(t, round) })
	}
}

func disconnectTogether(t *testing.T, round int) {
	{
		desk := newGmailDesk(t, "Race A")
		other := createSession(t, desk.api, desk.service, "Race B", "admin@race-b.example.com")
		connectEmail(t, desk.api, other, "team@race.example.com")
		desk.useAddress(t, desk.admin, "team@race.example.com")
		desk.finish(t, desk.startGmail(t, desk.admin), "team@race.example.com", sessionCookieOf(desk.admin))
		desk.finish(t, desk.startGmail(t, other), "team@race.example.com", sessionCookieOf(other))
		start := make(chan struct{})
		done := make(chan int, 2)
		for _, session := range []string{desk.admin, other} {
			go func() {
				<-start
				done <- serve(desk.api, newRequest(t, http.MethodDelete, "/api/v1/channels/email/gmail", nil, session, nil)).Code
			}()
		}
		close(start)
		if a, b := <-done, <-done; a != http.StatusNoContent || b != http.StatusNoContent {
			t.Fatalf("disconnects answered %d and %d", a, b)
		}
		for _, token := range desk.fake.RefreshTokens() {
			if !desk.fake.Revoked(token) {
				t.Fatalf("round %d: both workspaces dropped the account and its grant is still alive at Google", round)
			}
		}
	}
}

func TestAnAgentTokenAsksAPersonToConnectOrDisconnectGmail(t *testing.T) {
	desk := newGmailDesk(t, "Token Gmail")
	admin := issueToken(t, desk.api, desk.admin, "Ops agent", "admin").Token
	asked := accepted(t, desk.api, http.MethodPost, "/api/v1/channels/email/gmail", nil, admin)
	approved := approve(t, desk.api, desk.admin, asked.RequestID, nil)
	if approved.Code != http.StatusOK || !strings.Contains(approved.Body.String(), "accounts") && !strings.Contains(approved.Body.String(), "/o/oauth2/v2/auth") {
		t.Fatalf("approver gets the consent address: %d %s", approved.Code, approved.Body.String())
	}
	if stored := getRequest(t, desk.api, admin, asked.RequestID); strings.Contains(string(stored.Result), "oauth2") {
		t.Fatalf("the token can read the consent address: %s", stored.Result)
	}
	accepted(t, desk.api, http.MethodDelete, "/api/v1/channels/email/gmail", nil, admin)
}

// Gmail sends as the channel: the account must be the channel's support address mailbox, and one
// account sends for one email channel of a workspace. A refused grant nobody holds is revoked; a
// connection made before the rule keeps sending and says it does not match.
func TestGmailConnectsOnlyTheChannelsOwnAddressAndOnlyOnce(t *testing.T) {
	desk := newGmailDesk(t, "Own Address")
	if location := desk.finish(t, desk.startGmail(t, desk.admin), "someone.else@gmail.example.com", sessionCookieOf(desk.admin)); location != desk.page(t, desk.admin)+"?tab=replies&gmail_error=address_mismatch" {
		t.Fatalf("another account: Location = %s", location)
	}
	for _, token := range desk.fake.RefreshTokens() {
		if !desk.fake.Revoked(token) {
			t.Fatal("the refused grant is still alive at Google")
		}
	}
	if view := desk.settings(t, desk.admin); view.Gmail.Connection != nil {
		t.Fatalf("a refused account connected: %+v", view.Gmail.Connection)
	}
	// A connection made before the rule: the default channel sends through billing@, which a second channel is.
	defaultID := strings.TrimPrefix(desk.page(t, desk.admin), "/settings/channels/")
	st := store.New(desk.pool)
	if _, _, err := st.SaveMailboxConnection(t.Context(), workspaceID(t, desk.api, desk.admin), defaultID, "", "billing@own-address.example.com", google.ScopeGmailSend,
		store.SealedGrant{KeyID: "k1", Sealed: []byte("sealed"), Provider: domain.MailboxGoogle}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var view struct {
		Gmail struct {
			AddressMatches *bool `json:"address_matches"`
		} `json:"gmail"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/channels/email", nil, desk.admin, nil), &view)
	if view.Gmail.AddressMatches == nil || *view.Gmail.AddressMatches {
		t.Fatalf("address_matches = %v, want false for a connection of another address", view.Gmail.AddressMatches)
	}
	billing := addEmailChannel(t, desk.api, desk.admin, "billing@own-address.example.com")
	response := request(t, desk.api, http.MethodPost, "/api/v1/channels/"+billing+"/email/gmail", nil, desk.admin, nil)
	var consent struct {
		URL string `json:"url"`
	}
	decode(t, response, &consent)
	location := desk.finish(t, consent.URL, "billing@own-address.example.com", sessionCookieOf(desk.admin))
	if location != "/settings/channels/"+billing+"?tab=replies&gmail_error=account_in_use&gmail_channel="+defaultID {
		t.Fatalf("an account another channel sends through: Location = %s", location)
	}
	for _, token := range desk.fake.RefreshTokens() {
		if desk.fake.Revoked(token) && desk.fake.AccountOf(token) == "billing@own-address.example.com" {
			t.Fatal("refusing the account revoked it while another channel holds it")
		}
	}
}
