package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type presenceViewer struct {
	AgentID      string  `json:"agent_id"`
	Name         string  `json:"name"`
	Writing      bool    `json:"writing"`
	WritingSince *string `json:"writing_since"`
	EditedAt     *string `json:"edited_at"`
	AgentToken   *struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Scope string `json:"scope"`
	} `json:"agent_token"`
}

type presenceAnswer struct {
	Viewers          []presenceViewer `json:"viewers"`
	HeartbeatSeconds int              `json:"heartbeat_seconds"`
}

// presenceDesk is one workspace with an admin and a teammate, a conversation, and a second workspace.
type presenceDesk struct {
	api                    http.Handler
	pool                   *pgxpool.Pool
	clock                  *testClock
	admin, teammate, other string
	adminID, teammateID    string
	conversation           string
	otherConversation      string
	// announced is when each tab last announced each conversation through put.
	announced map[string]time.Time
}

func newPresenceDesk(t *testing.T) presenceDesk {
	t.Helper()
	clock := &testClock{now: time.Date(2026, time.October, 22, 9, 0, 0, 0, time.UTC)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = clock.Now })
	d := presenceDesk{api: api, pool: pool, clock: clock, announced: map[string]time.Time{}}
	d.admin = createSession(t, api, service, "Presence Desk", "admin@presence.example.com")
	d.teammate = joinAsAgent(t, api, d.admin, "lan@presence.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Lan"}, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("display name status=%d body=%s", response.Code, response.Body.String())
	}
	d.adminID, d.teammateID = agentIDOf(t, api, d.admin), agentIDOf(t, api, d.teammate)
	contact := create(t, api, d.admin, "/api/v1/contacts", map[string]any{"name": "Customer", "email": "customer@presence.example.com"})
	d.conversation = create(t, api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Where is my order?"})
	d.other = createSession(t, api, service, "Other Desk", "admin@other-presence.example.com")
	otherContact := create(t, api, d.other, "/api/v1/contacts", map[string]any{"name": "Other", "email": "other@presence.example.com"})
	d.otherConversation = create(t, api, d.other, "/api/v1/conversations", map[string]any{"contact_id": otherContact, "subject": "Other"})
	return d
}

func agentIDOf(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Agent.ID
}

func presencePath(conversation string) string {
	return "/api/v1/conversations/" + conversation + "/presence"
}

func (d presenceDesk) put(t *testing.T, token, conversation, client string, writing bool) presenceAnswer {
	t.Helper()
	// One tab announces one conversation at most every 2 seconds; a test that calls again sooner waits.
	key := token + "|" + conversation + "|" + client
	if last, ok := d.announced[key]; ok && d.clock.Now().Before(last.Add(2*time.Second)) {
		d.clock.set(last.Add(2 * time.Second))
	}
	d.announced[key] = d.clock.Now()
	response := request(t, d.api, http.MethodPut, presencePath(conversation), map[string]any{"client_id": client, "writing": writing}, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT presence status=%d body=%s", response.Code, response.Body.String())
	}
	var answer presenceAnswer
	decode(t, response, &answer)
	return answer
}

func (d presenceDesk) leave(t *testing.T, token, conversation, client string) {
	t.Helper()
	if response := request(t, d.api, http.MethodDelete, presencePath(conversation)+"/"+client, nil, token, nil); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE presence status=%d body=%s", response.Code, response.Body.String())
	}
}

func (d presenceDesk) rows(t *testing.T) int {
	t.Helper()
	var count int
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversation_presence`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func viewerSummary(viewers []presenceViewer) string {
	parts := make([]string, len(viewers))
	for index, viewer := range viewers {
		parts[index] = fmt.Sprintf("%s writing=%v", viewer.Name, viewer.Writing)
		if viewer.AgentToken != nil {
			parts[index] += " via " + viewer.AgentToken.Label
		}
	}
	return strings.Join(parts, "; ")
}

func TestPresenceListsOtherAgentsNotSelfOncePerPerson(t *testing.T) {
	d := newPresenceDesk(t)
	if answer := d.put(t, d.teammate, d.conversation, "lan-tab-one", false); len(answer.Viewers) != 0 || answer.HeartbeatSeconds != 5 {
		t.Fatalf("first viewer sees %+v", answer)
	}
	d.put(t, d.teammate, d.conversation, "lan-tab-two", true)
	answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false)
	if got := viewerSummary(answer.Viewers); got != "Lan writing=true" {
		t.Fatalf("admin sees %q, want Lan once and writing", got)
	}
	lan := answer.Viewers[0]
	if lan.AgentID != d.teammateID || lan.WritingSince == nil || lan.EditedAt == nil || lan.AgentToken != nil {
		t.Fatalf("Lan = %+v", lan)
	}
	// The caller's own tabs are never listed, even another tab of the same person.
	if got := viewerSummary(d.put(t, d.teammate, d.conversation, "lan-tab-one", false).Viewers); got != "admin@presence.example.com writing=false" {
		t.Fatalf("Lan sees %q", got)
	}
	// Closing the writing tab keeps the other one.
	d.leave(t, d.teammate, d.conversation, "lan-tab-two")
	if got := viewerSummary(d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers); got != "Lan writing=false" {
		t.Fatalf("after one tab left admin sees %q", got)
	}
	d.leave(t, d.teammate, d.conversation, "lan-tab-one")
	d.leave(t, d.teammate, d.conversation, "lan-tab-one") // twice is harmless
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("after both tabs left admin sees %q", viewerSummary(answer.Viewers))
	}
}

func TestPresenceMovesWithTheTabAndAsksHiddenTabsForFewerCalls(t *testing.T) {
	d := newPresenceDesk(t)
	contact := create(t, d.api, d.admin, "/api/v1/contacts", map[string]any{"name": "Second", "email": "second@presence.example.com"})
	second := create(t, d.api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Second"})
	d.put(t, d.teammate, d.conversation, "lan-tab-one", false)
	// The same tab opening another conversation is no longer on the first one.
	d.put(t, d.teammate, second, "lan-tab-one", false)
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("first conversation still lists %q", viewerSummary(answer.Viewers))
	}
	if got := viewerSummary(d.put(t, d.admin, second, "admin-tab-1", false).Viewers); got != "Lan writing=false" {
		t.Fatalf("second conversation lists %q", got)
	}
	d.clock.set(d.clock.Now().Add(2 * time.Second))
	response := request(t, d.api, http.MethodPut, presencePath(second), map[string]any{"client_id": "lan-tab-one", "writing": true, "hidden": true}, d.teammate, nil)
	var answer presenceAnswer
	decode(t, response, &answer)
	if response.Code != http.StatusOK || answer.HeartbeatSeconds != 30 {
		t.Fatalf("hidden tab status=%d answer=%+v", response.Code, answer)
	}
}

func TestPresenceWritingSinceStaysAndEditedAtFollowsChanges(t *testing.T) {
	d := newPresenceDesk(t)
	started := d.clock.Now()
	d.put(t, d.teammate, d.conversation, "lan-tab-one", true)
	d.clock.set(started.Add(90 * time.Second))
	// A heartbeat without a change keeps both times.
	d.put(t, d.teammate, d.conversation, "lan-tab-one", true)
	lan := d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers[0]
	if *lan.WritingSince != started.Format(time.RFC3339) || *lan.EditedAt != started.Format(time.RFC3339) {
		t.Fatalf("after an idle heartbeat Lan = since %v edited %v", *lan.WritingSince, *lan.EditedAt)
	}
	edited := started.Add(100 * time.Second)
	d.clock.set(edited)
	d.announced[d.teammate+"|"+d.conversation+"|lan-tab-one"] = edited
	if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), map[string]any{"client_id": "lan-tab-one", "writing": true, "edited": true}, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("edited heartbeat status=%d", response.Code)
	}
	lan = d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers[0]
	if *lan.WritingSince != started.Format(time.RFC3339) || *lan.EditedAt != edited.Format(time.RFC3339) {
		t.Fatalf("after an edit Lan = since %v edited %v", *lan.WritingSince, *lan.EditedAt)
	}
	// Sending (an empty box) ends the reply; the next one starts fresh.
	d.put(t, d.teammate, d.conversation, "lan-tab-one", false)
	lan = d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers[0]
	if lan.Writing || lan.WritingSince != nil || lan.EditedAt != nil {
		t.Fatalf("after the box emptied Lan = %+v", lan)
	}
}

func TestPresenceExpires(t *testing.T) {
	d := newPresenceDesk(t)
	start := d.clock.Now()
	d.put(t, d.teammate, d.conversation, "lan-viewing", false)
	d.clock.set(start.Add(14 * time.Second))
	if got := viewerSummary(d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers); got != "Lan writing=false" {
		t.Fatalf("at 14 s admin sees %q", got)
	}
	d.clock.set(start.Add(16 * time.Second))
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("a viewing row outlived 15 s: %q", viewerSummary(answer.Viewers))
	}

	// A reply not sent yet is kept for 150 s: a tab in the background renews it rarely.
	writingStart := d.clock.Now()
	d.put(t, d.teammate, d.conversation, "lan-writing", true)
	d.clock.set(writingStart.Add(149 * time.Second))
	if got := viewerSummary(d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers); got != "Lan writing=true" {
		t.Fatalf("at 149 s admin sees %q", got)
	}
	d.clock.set(writingStart.Add(151 * time.Second))
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("a writing row outlived 150 s: %q", viewerSummary(answer.Viewers))
	}
	// Expired rows of the workspace are removed by the next announce, so the table does not grow.
	if got := d.rows(t); got != 1 {
		t.Fatalf("rows left = %d, want only the admin's", got)
	}
}

func TestPresenceLeaveOnlyRemovesTheCallersOwnRow(t *testing.T) {
	d := newPresenceDesk(t)
	d.put(t, d.teammate, d.conversation, "lan-secret-tab", true)
	// Knowing another person's tab id is not enough to remove them.
	d.leave(t, d.admin, d.conversation, "lan-secret-tab")
	if got := viewerSummary(d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers); got != "Lan writing=true" {
		t.Fatalf("admin removed Lan's row: sees %q", got)
	}
}

func TestPresenceNeverCrossesWorkspaces(t *testing.T) {
	d := newPresenceDesk(t)
	d.put(t, d.teammate, d.conversation, "lan-tab-one", true)
	for _, response := range []*httptest.ResponseRecorder{
		request(t, d.api, http.MethodPut, presencePath(d.conversation), map[string]any{"client_id": "other-tab-1", "writing": true}, d.other, nil),
		request(t, d.api, http.MethodDelete, presencePath(d.conversation)+"/lan-tab-one", nil, d.other, nil),
		request(t, d.api, http.MethodPut, presencePath(d.otherConversation), map[string]any{"client_id": "lan-tab-one", "writing": false}, d.teammate, nil),
		request(t, d.api, http.MethodDelete, presencePath(d.otherConversation)+"/lan-tab-one", nil, d.teammate, nil),
		request(t, d.api, http.MethodPut, presencePath("00000000-0000-4000-8000-000000000000"), map[string]any{"client_id": "lan-tab-one", "writing": false}, d.teammate, nil),
		request(t, d.api, http.MethodPut, presencePath("not-an-id"), map[string]any{"client_id": "lan-tab-one", "writing": false}, d.teammate, nil),
	} {
		if response.Code != http.StatusNotFound {
			t.Fatalf("cross-workspace presence status=%d body=%s, want 404", response.Code, response.Body.String())
		}
	}
	if got := d.rows(t); got != 1 {
		t.Fatalf("rows = %d, want only Lan's", got)
	}
	// The other workspace's own conversation never shows this one's people.
	if answer := d.put(t, d.other, d.otherConversation, "other-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("other workspace sees %q", viewerSummary(answer.Viewers))
	}
	if got := viewerSummary(d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers); got != "Lan writing=true" {
		t.Fatalf("admin sees %q", got)
	}
}

func TestDeactivatedAgentDisappearsFromPresence(t *testing.T) {
	d := newPresenceDesk(t)
	d.put(t, d.teammate, d.conversation, "lan-tab-one", true)
	if response := request(t, d.api, http.MethodPatch, "/api/v1/agents/"+d.teammateID, map[string]any{"active": false}, d.admin, nil); response.Code != http.StatusOK {
		t.Fatalf("deactivate status=%d body=%s", response.Code, response.Body.String())
	}
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("a deactivated agent is still listed: %q", viewerSummary(answer.Viewers))
	}
	if got := d.rows(t); got != 1 {
		t.Fatalf("rows = %d, want the deactivated agent's removed", got)
	}
	if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), map[string]any{"client_id": "lan-tab-one", "writing": true}, d.teammate, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("heartbeat of a revoked session status=%d, want 401", response.Code)
	}
}

func TestRevokedSessionDisappearsFromPresence(t *testing.T) {
	d := newPresenceDesk(t)
	d.put(t, d.teammate, d.conversation, "lan-tab-one", false)
	if response := request(t, d.api, http.MethodPost, "/api/v1/auth/logout", nil, d.teammate, nil); response.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d", response.Code)
	}
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("a signed-out session is still listed: %q", viewerSummary(answer.Viewers))
	}
}

func TestAgentTokenPresenceNamesTheTokenAndLastsTwoMinutes(t *testing.T) {
	d := newPresenceDesk(t)
	token := issueToken(t, d.api, d.teammate, "Claude Code", "read")
	start := d.clock.Now()
	answer := d.put(t, token.Token, d.conversation, "mcp-agent", false)
	if len(answer.Viewers) != 0 || answer.HeartbeatSeconds != 120 {
		t.Fatalf("token answer = %+v", answer)
	}
	d.put(t, d.teammate, d.conversation, "lan-tab-one", true)
	// The person's own tab is listed for their agent token: the AI must know a person is writing.
	if got := viewerSummary(d.put(t, token.Token, d.conversation, "mcp-agent", false).Viewers); got != "Lan writing=true" {
		t.Fatalf("token sees %q", got)
	}
	start = d.clock.Now() // the token's row lives 2 minutes from its last announce
	viewers := d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers
	if got := viewerSummary(viewers); got != "Lan writing=true; Lan writing=false via Claude Code" {
		t.Fatalf("admin sees %q", got)
	}
	if viewers[1].AgentToken.ID != token.Session.ID || viewers[1].AgentToken.Scope != "read" {
		t.Fatalf("token viewer = %+v", viewers[1].AgentToken)
	}
	d.leave(t, d.teammate, d.conversation, "lan-tab-one")
	d.clock.set(start.Add(119 * time.Second))
	if got := viewerSummary(d.put(t, d.admin, d.conversation, "admin-tab-1", false).Viewers); got != "Lan writing=false via Claude Code" {
		t.Fatalf("at 119 s admin sees %q", got)
	}
	d.clock.set(start.Add(121 * time.Second))
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("token presence outlived 2 minutes: %q", viewerSummary(answer.Viewers))
	}
	d.put(t, token.Token, d.conversation, "mcp-agent", false)
	d.leave(t, token.Token, d.conversation, "mcp-agent")
	if answer := d.put(t, d.admin, d.conversation, "admin-tab-1", false); len(answer.Viewers) != 0 {
		t.Fatalf("token left but is listed: %q", viewerSummary(answer.Viewers))
	}
}

func TestPresenceRefusesABadBody(t *testing.T) {
	d := newPresenceDesk(t)
	for _, body := range []any{
		map[string]any{"client_id": "short", "writing": false},
		map[string]any{"client_id": "has spaces in it", "writing": false},
		map[string]any{"client_id": strings.Repeat("a", 65), "writing": false},
		map[string]any{"writing": false},
		map[string]any{"client_id": "lan-tab-one"},
		map[string]any{"client_id": "lan-tab-one", "writing": false, "text": "a draft is never sent"},
	} {
		if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), body, d.teammate, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("body %v status=%d, want 400", body, response.Code)
		}
	}
	if response := request(t, d.api, http.MethodDelete, presencePath(d.conversation)+"/short", nil, d.teammate, nil); response.Code != http.StatusNotFound {
		t.Fatalf("DELETE with a malformed tab id status=%d, want 404", response.Code)
	}
}

func TestPresenceKeepsAtMostTwentyTabsPerPerson(t *testing.T) {
	d := newPresenceDesk(t)
	for index := range 25 {
		d.clock.set(d.clock.Now().Add(100 * time.Millisecond))
		d.put(t, d.teammate, d.conversation, fmt.Sprintf("lan-tab-%02d", index), false)
	}
	if got := d.rows(t); got != 20 {
		t.Fatalf("rows = %d, want the 20 newest tabs", got)
	}
	var oldest string
	if err := d.pool.QueryRow(context.Background(), `SELECT min(client_id) FROM conversation_presence`).Scan(&oldest); err != nil || oldest != "lan-tab-05" {
		t.Fatalf("oldest kept tab = %q %v, want lan-tab-05", oldest, err)
	}
}

func TestPresenceLeaveByCookieNeedsTheOrigin(t *testing.T) {
	d := newPresenceDesk(t)
	d.put(t, d.teammate, d.conversation, "lan-tab-one", false)
	cookie := &http.Cookie{Name: "colliedesk_session", Value: d.teammate}
	if response := request(t, d.api, http.MethodDelete, presencePath(d.conversation)+"/lan-tab-one", nil, "", cookie); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_rejected") {
		t.Fatalf("cookie DELETE without Origin status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestWithOrigin(t, d.api, http.MethodDelete, presencePath(d.conversation)+"/lan-tab-one", cookie, testOrigin); response.Code != http.StatusNoContent {
		t.Fatalf("cookie DELETE with Origin status=%d body=%s", response.Code, response.Body.String())
	}
	if got := d.rows(t); got != 0 {
		t.Fatalf("rows = %d after leaving", got)
	}
}

type viewingActivity struct {
	Viewing []struct {
		ConversationID string           `json:"conversation_id"`
		Viewers        []presenceViewer `json:"viewers"`
	} `json:"viewing"`
}

// viewingOf polls the inbox activity, without a cursor (the first poll of a page) or with one, and
// returns who is listed on each conversation.
func (d presenceDesk) viewingOf(t *testing.T, token, since string) map[string]string {
	t.Helper()
	path := "/api/v1/inbox/activity"
	if since != "" {
		path += "?since=" + since
	}
	response := request(t, d.api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
	}
	var body viewingActivity
	decode(t, response, &body)
	if body.Viewing == nil {
		t.Fatalf("activity has no viewing list: %s", response.Body.String())
	}
	out := map[string]string{}
	for _, item := range body.Viewing {
		out[item.ConversationID] = viewerSummary(item.Viewers)
	}
	return out
}

func TestInboxActivityListsWhoHasEachConversationOpenFromTheFirstPoll(t *testing.T) {
	d := newPresenceDesk(t)
	contact := create(t, d.api, d.admin, "/api/v1/contacts", map[string]any{"name": "Second", "email": "second@presence.example.com"})
	second := create(t, d.api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Second"})
	d.put(t, d.teammate, d.conversation, "lan-tab-one", true)
	d.put(t, d.teammate, second, "lan-tab-two", false)
	d.put(t, d.admin, second, "admin-tab-1", false)
	token := issueToken(t, d.api, d.admin, "Claude Code", "read")
	d.put(t, token.Token, d.conversation, "mcp-agent", false)

	want := fmt.Sprint(map[string]string{d.conversation: "Lan writing=true; admin@presence.example.com writing=false via Claude Code", second: "Lan writing=false"})
	for _, since := range []string{"", d.clock.Now().Add(-time.Minute).Format(time.RFC3339)} {
		if got := fmt.Sprint(d.viewingOf(t, d.admin, since)); got != want {
			t.Fatalf("admin (since %q) sees %s, want %s", since, got, want)
		}
	}
	// Lan's own tabs are hers, never listed back to her.
	if got := fmt.Sprint(d.viewingOf(t, d.teammate, "")); got != fmt.Sprint(map[string]string{d.conversation: "admin@presence.example.com writing=false via Claude Code", second: "admin@presence.example.com writing=false"}) {
		t.Fatalf("Lan sees %s", got)
	}
	// Another workspace sees none of it, and expired rows are gone.
	if got := d.viewingOf(t, d.other, ""); len(got) != 0 {
		t.Fatalf("other workspace sees %v", got)
	}
	d.clock.set(d.clock.Now().Add(3 * time.Minute))
	if got := d.viewingOf(t, d.admin, ""); len(got) != 0 {
		t.Fatalf("after every row expired admin sees %v", got)
	}
}

func TestPresenceRefusesATabThatAnnouncesTooOften(t *testing.T) {
	d := newPresenceDesk(t)
	start := d.clock.Now()
	body := map[string]any{"client_id": "lan-tab-one", "writing": false}
	if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), body, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("first announce status=%d", response.Code)
	}
	// A token, or a script, repeating itself at once writes nothing: 429 with Retry-After, the row as it was.
	d.clock.set(start.Add(1900 * time.Millisecond))
	response := request(t, d.api, http.MethodPut, presencePath(d.conversation), body, d.teammate, nil)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "2" || !strings.Contains(response.Body.String(), "too_many_requests") {
		t.Fatalf("announce after 1.9 s status=%d retry=%q body=%s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	row := func() (time.Time, *time.Time) {
		var seen time.Time
		var since *time.Time
		if err := d.pool.QueryRow(context.Background(), `SELECT seen_at,writing_since FROM conversation_presence WHERE client_id='lan-tab-one'`).Scan(&seen, &since); err != nil {
			t.Fatal(err)
		}
		return seen, since
	}
	if seen, since := row(); !seen.Equal(start) || since != nil {
		t.Fatalf("row after the refused announce: seen %v writing %v", seen, since)
	}
	// Starting a reply is said at once, even right after a heartbeat; flipping it back and forth in a loop
	// is not: within half a second the change is refused too.
	writing := map[string]any{"client_id": "lan-tab-one", "writing": true}
	d.clock.set(start.Add(600 * time.Millisecond))
	if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), writing, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("starting a reply 0.6 s after a heartbeat status=%d", response.Code)
	}
	if seen, since := row(); !seen.Equal(start.Add(600*time.Millisecond)) || since == nil {
		t.Fatalf("row after the reply started: seen %v writing %v", seen, since)
	}
	d.clock.set(start.Add(1000 * time.Millisecond))
	if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), body, d.teammate, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("ending it 0.4 s later status=%d, want 429", response.Code)
	}
	d.clock.set(start.Add(1200 * time.Millisecond))
	if response := request(t, d.api, http.MethodPut, presencePath(d.conversation), body, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("ending it 0.6 s later status=%d", response.Code)
	}
	// Opening another conversation is a new place, not a repeat; and 2 seconds later the same one is fine.
	contact := create(t, d.api, d.admin, "/api/v1/contacts", map[string]any{"name": "Second", "email": "second@presence.example.com"})
	second := create(t, d.api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Second"})
	if response := request(t, d.api, http.MethodPut, presencePath(second), writing, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("moving to another conversation status=%d", response.Code)
	}
	d.clock.set(start.Add(4 * time.Second))
	if response := request(t, d.api, http.MethodPut, presencePath(second), writing, d.teammate, nil); response.Code != http.StatusOK {
		t.Fatalf("announce after 2 s status=%d", response.Code)
	}
}
