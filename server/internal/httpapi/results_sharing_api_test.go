package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

type shareLinkCreated struct {
	URL  string `json:"url"`
	Link struct {
		CreatedAt     string  `json:"created_at"`
		CreatedByName *string `json:"created_by_name"`
	} `json:"link"`
}

func createShareLink(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/results/share-link", nil, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("create share link: %d %s", response.Code, response.Body.String())
	}
	var created shareLinkCreated
	decode(t, response, &created)
	_, shared, ok := strings.Cut(created.URL, "/r#")
	if !ok || shared == "" || created.Link.CreatedByName == nil {
		t.Fatalf("share link = %+v", created)
	}
	return shared
}

// errorOf is an error answer without its request id, to compare two refusals.
func errorOf(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("not an error answer: %s", response.Body.String())
	}
	return body.Error.Code + ": " + body.Error.Message
}

func openShared(t *testing.T, api http.Handler, shareToken string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, api, http.MethodPost, "/api/v1/results/shared", map[string]any{"token": shareToken}, "", nil)
}

// keysOf lists every object key of a JSON document, with its path, so a test can pin what may leave.
func keysOf(prefix string, value any, into map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			// Window names are data, not fields.
			if key == "30d" || key == "7d" {
				path = prefix + ".<window>"
			}
			into[path] = true
			keysOf(path, child, into)
		}
	case []any:
		for _, child := range typed {
			keysOf(prefix, child, into)
		}
	}
}

// A share link shows only totals: a fixed set of keys, with no conversation or snapshot id, no
// account, no names and no text. Creating another turns the old one off; turning it off makes it
// answer the same 404 as a link that never existed; it never shows another workspace.
func TestResultsShareLinkShowsTotalsOnlyAndCanBeTurnedOff(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Share Desk", "admin@share.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "agent@share.example.com", "agent")
	agent := redeemLabelled(t, api, inviteURL, "Agent")
	other := createSession(t, api, service, "Other Share Desk", "admin@other-share.example.com")
	workspace := workspaceID(t, api, admin)
	seedCustomerConversations(t, pool, workspace, 10, 20, 30, 40, 50, 0)
	runResultsWorker(t, pool)

	var state struct {
		Link *json.RawMessage `json:"link"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/results/share-link", nil, admin, nil), &state)
	if state.Link != nil {
		t.Fatalf("a new workspace has a share link: %s", *state.Link)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/results/share-link", nil, agent, nil); response.Code != http.StatusForbidden {
		t.Fatalf("an agent created a share link: %d", response.Code)
	}

	first := createShareLink(t, api, admin)
	opened := openShared(t, api, first)
	if opened.Code != http.StatusOK || opened.Header().Get("Cache-Control") != "no-store" || opened.Header().Get("Referrer-Policy") != "no-referrer" ||
		opened.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("shared = %d %v %s", opened.Code, opened.Header(), opened.Body.String())
	}
	var document map[string]any
	if err := json.Unmarshal(opened.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	keysOf("", document, keys)
	allowed := map[string]bool{"workspace_name": true, "computed_at": true, "baseline_from": true, "baseline_import_status": true, "current": true, "baseline": true, "hours_saved": true,
		"hours_saved.<window>": true, "current.measured_at": true, "current.windows": true, "current.windows.<window>": true}
	for _, field := range []string{"created", "created.from", "created.to", "resolved_created", "resolved_created.from", "resolved_created.to", "available_on",
		"first_response", "first_response.median_seconds", "first_response.p90_seconds", "first_response.answered", "first_response.waiting",
		"resolution", "resolution.median_seconds", "resolution.resolved", "resolution.unresolved", "resolution.without_resolve_time",
		"reopened", "reopened.rate", "reopened.reopened", "reopened.resolutions", "volume", "volume.count", "volume.from", "volume.days", "volume.per_week",
		"handle", "ai", "ai.sent_unchanged", "ai.sent_edited", "ai.discarded", "ai.conversations_sent_unchanged"} {
		allowed["current.windows.<window>."+field] = true
	}
	var extra []string
	for key := range keys {
		if !allowed[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 || document["workspace_name"] != "Share Desk" {
		t.Fatalf("shared holds keys it must not: %v (workspace %v)", extra, document["workspace_name"])
	}
	for _, secret := range []string{workspace, "casey", "Casey", "Help", "On it", "admin@share.example.com"} {
		if strings.Contains(opened.Body.String(), secret) {
			t.Fatalf("shared answer contains %q: %s", secret, opened.Body.String())
		}
	}

	// The other workspace sees no link of this one.
	state.Link = nil
	decode(t, request(t, api, http.MethodGet, "/api/v1/results/share-link", nil, other, nil), &state)
	if state.Link != nil {
		t.Fatalf("another workspace sees this workspace's link")
	}

	unknown := openShared(t, api, "no-such-link")
	second := createShareLink(t, api, admin)
	replaced := openShared(t, api, first)
	if replaced.Code != http.StatusNotFound || errorOf(t, replaced) != errorOf(t, unknown) || errorOf(t, unknown) != "share_link_off: This link was turned off. Ask the team for a new one." {
		t.Fatalf("replaced link = %d %s, want the unknown link's 404 %s", replaced.Code, replaced.Body.String(), unknown.Body.String())
	}
	if response := openShared(t, api, second); response.Code != http.StatusOK {
		t.Fatalf("new link = %d", response.Code)
	}
	for range 2 {
		if response := request(t, api, http.MethodDelete, "/api/v1/results/share-link", nil, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("turn off = %d", response.Code)
		}
	}
	if response := openShared(t, api, second); response.Code != http.StatusNotFound || errorOf(t, response) != errorOf(t, unknown) {
		t.Fatalf("turned off link = %d %s", response.Code, response.Body.String())
	}
}

// A locked workspace's link answers like one that never existed; a paying one keeps working.
func TestResultsShareLinkOfALockedWorkspaceIsOff(t *testing.T) {
	f := newLimitsFixture(t, true)
	alpha, bravo := createShareLink(t, f.api, f.adminA), createShareLink(t, f.api, f.adminB)
	f.upgrade(t, f.workspaceB, "sub_share_bravo")
	unknown := openShared(t, f.api, "no-such-link")
	f.clock.set(trialEnd)
	if response := openShared(t, f.api, alpha); response.Code != http.StatusNotFound || errorOf(t, response) != errorOf(t, unknown) {
		t.Fatalf("locked workspace's link = %d %s", response.Code, response.Body.String())
	}
	if response := openShared(t, f.api, bravo); response.Code != http.StatusOK {
		t.Fatalf("paying workspace's link = %d", response.Code)
	}
}

// Each admin turns the weekly email off and on for themselves; agents have none.
func TestResultsWeeklyEmailIsEachAdminsChoice(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	admin := createSession(t, api, service, "Weekly Desk", "admin@weekly.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "agent@weekly.example.com", "agent")
	agent := redeemLabelled(t, api, inviteURL, "Agent")
	var page struct {
		WeeklyEmail *struct {
			Subscribed bool   `json:"subscribed"`
			CanSend    bool   `json:"can_send"`
			NextAt     string `json:"next_at"`
		} `json:"weekly_email"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	if page.WeeklyEmail == nil || !page.WeeklyEmail.Subscribed || page.WeeklyEmail.NextAt == "" {
		t.Fatalf("admin weekly email = %+v", page.WeeklyEmail)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/results-email", map[string]any{"weekly": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("turn off = %d %s", response.Code, response.Body.String())
	}
	page.WeeklyEmail = nil
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	if page.WeeklyEmail == nil || page.WeeklyEmail.Subscribed {
		t.Fatalf("after turning off = %+v", page.WeeklyEmail)
	}
	page.WeeklyEmail = nil
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, agent, nil), &page)
	if page.WeeklyEmail != nil {
		t.Fatalf("agent weekly email = %+v", page.WeeklyEmail)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/results-email", map[string]any{}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("empty change = %d", response.Code)
	}
}
