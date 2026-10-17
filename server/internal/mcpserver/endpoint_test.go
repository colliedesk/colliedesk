package mcpserver_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// toolNames is the list a client sees, written out by hand: the SDK lists tools by name.
var toolNames = []string{
	"colliedesk_agents_list",
	"colliedesk_attachment_get",
	"colliedesk_contact_create",
	"colliedesk_contact_get",
	"colliedesk_contacts_find",
	"colliedesk_conversation_create",
	"colliedesk_conversation_get",
	"colliedesk_conversation_update",
	"colliedesk_conversations_bulk_update",
	"colliedesk_docs_search",
	"colliedesk_inbox_activity",
	"colliedesk_inbox_search",
	"colliedesk_note_add",
	"colliedesk_reply_retry",
	"colliedesk_reply_send",
	"colliedesk_tag_delete",
	"colliedesk_tag_rename",
	"colliedesk_tags_list",
	"colliedesk_time_entries_list",
	"colliedesk_token_revoke",
	"colliedesk_whoami",
}

// wantAnnotations: read only, idempotent, destructive (cannot be undone: it reaches a customer, deletes, or
// revokes), and the tier each description must state.
var wantAnnotations = map[string]struct {
	readOnly, idempotent, destructive bool
	tier                              string
}{
	"colliedesk_agents_list":               {readOnly: true, tier: "read"},
	"colliedesk_attachment_get":            {readOnly: true, tier: "read"},
	"colliedesk_contact_create":            {tier: "send"},
	"colliedesk_contact_get":               {readOnly: true, tier: "read"},
	"colliedesk_contacts_find":             {readOnly: true, tier: "read"},
	"colliedesk_conversation_create":       {tier: "send"},
	"colliedesk_conversation_get":          {readOnly: true, tier: "read"},
	"colliedesk_conversation_update":       {idempotent: true, tier: "draft"},
	"colliedesk_conversations_bulk_update": {idempotent: true, tier: "draft"},
	"colliedesk_docs_search":               {readOnly: true, tier: "any"},
	"colliedesk_inbox_activity":            {readOnly: true, tier: "read"},
	"colliedesk_inbox_search":              {readOnly: true, tier: "read"},
	"colliedesk_note_add":                  {tier: "draft"},
	"colliedesk_reply_retry":               {destructive: true, tier: "send"},
	"colliedesk_reply_send":                {destructive: true, tier: "send"},
	"colliedesk_tag_delete":                {destructive: true, tier: "send"},
	"colliedesk_tag_rename":                {tier: "send"},
	"colliedesk_tags_list":                 {readOnly: true, tier: "read"},
	"colliedesk_time_entries_list":         {readOnly: true, tier: "read"},
	"colliedesk_token_revoke":              {destructive: true, tier: "any"},
	"colliedesk_whoami":                    {readOnly: true, tier: "any"},
}

func TestToolsListIsTheCatalogInFixedOrderWithAnnotations(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("List Desk", "admin@list.example.com")
	for _, version := range []string{"", "2025-11-25"} {
		session := e.connect(w.read, version)
		if info := session.InitializeResult().ServerInfo; info.Name != "colliedesk" || info.Version != testVersion {
			t.Errorf("protocol %q: serverInfo=%+v, want colliedesk %s", version, info, testVersion)
		}
		for round := 0; round < 2; round++ {
			listed, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatalf("protocol %q: list tools: %v", version, err)
			}
			var names []string
			for _, tool := range listed.Tools {
				names = append(names, tool.Name)
			}
			if !slices.Equal(names, toolNames) {
				t.Fatalf("protocol %q round %d: tools=%v, want %v", version, round, names, toolNames)
			}
			for _, tool := range listed.Tools {
				want := wantAnnotations[tool.Name]
				a := tool.Annotations
				if a == nil || a.ReadOnlyHint != want.readOnly || a.IdempotentHint != want.idempotent || a.Title == "" || tool.Title == "" {
					t.Errorf("%s: annotations=%+v title=%q, want readOnly=%v idempotent=%v and a title", tool.Name, a, tool.Title, want.readOnly, want.idempotent)
				}
				if !want.readOnly && (a == nil || a.DestructiveHint == nil || *a.DestructiveHint != want.destructive) {
					t.Errorf("%s: a writing tool must state destructiveHint=%v", tool.Name, want.destructive)
				}
				if want.destructive && !strings.Contains(tool.Description, "cannot be undone") {
					t.Errorf("%s: a destructive tool must say it cannot be undone\n%s", tool.Name, tool.Description)
				}
				if !strings.Contains(tool.Description, "Tier: "+want.tier) {
					t.Errorf("%s: description does not state %q\n%s", tool.Name, "Tier: "+want.tier, tool.Description)
				}
				if tool.InputSchema == nil || tool.OutputSchema == nil {
					t.Errorf("%s: missing input or output schema", tool.Name)
				}
			}
		}
	}
}

func TestWhoamiReturnsTheTierOfTheToken(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Whoami Desk", "admin@whoami.example.com")
	for tier, token := range map[string]string{"read": w.read, "draft": w.draft, "send": w.send} {
		out := e.ok(e.connect(token, ""), "colliedesk_whoami", nil)
		if got := at(t, out, "token_tier"); got != tier {
			t.Errorf("token_tier=%v, want %q (%v)", got, tier, out)
		}
		if got := at(t, out, "agent", "email"); got != "admin@whoami.example.com" {
			t.Errorf("%s: agent email=%v", tier, got)
		}
		if got := at(t, out, "workspace", "name"); got != "Whoami Desk" {
			t.Errorf("%s: workspace=%v", tier, got)
		}
		if got := at(t, out, "workspace", "timezone"); got != "UTC" {
			t.Errorf("%s: timezone=%v", tier, got)
		}
		tools, _ := at(t, out, "tools_you_can_use").([]any)
		canUpdate := slices.Contains(tools, any("colliedesk_conversation_update"))
		if canUpdate != (tier != "read") {
			t.Errorf("%s: tools_you_can_use=%v, conversation_update available=%v", tier, tools, canUpdate)
		}
	}
}

func TestMCPRequiresABearerTokenAndNeverACookie(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Gate Desk", "admin@gate.example.com")
	initialize := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

	post := func(headers map[string]string, cookie *http.Cookie) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, e.mcpURL(), strings.NewReader(initialize))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		return response
	}
	cases := []struct {
		name    string
		headers map[string]string
		cookie  *http.Cookie
	}{
		{"no credential", nil, nil},
		{"empty bearer", map[string]string{"Authorization": "Bearer "}, nil},
		{"unknown token", map[string]string{"Authorization": "Bearer not-a-real-token"}, nil},
		{"wrong scheme", map[string]string{"Authorization": "Basic " + w.read}, nil},
		{"person session as cookie", nil, &http.Cookie{Name: "colliedesk_session", Value: w.admin}},
		{"agent token as cookie", nil, &http.Cookie{Name: "colliedesk_session", Value: w.read}},
		{"valid cookie with a bad bearer", map[string]string{"Authorization": "Bearer nope"}, &http.Cookie{Name: "colliedesk_session", Value: w.admin}},
		{"person session as bearer", map[string]string{"Authorization": "Bearer " + w.admin}, nil},
	}
	for _, c := range cases {
		response := post(c.headers, c.cookie)
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(response.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s: status=%d WWW-Authenticate=%q body=%s, want 401 with a Bearer challenge", c.name, response.StatusCode, response.Header.Get("WWW-Authenticate"), body)
		}
	}
	if response := post(map[string]string{"Authorization": "Bearer " + w.read}, nil); response.StatusCode != http.StatusOK {
		t.Errorf("agent token as bearer: status=%d, want 200", response.StatusCode)
	}
	// Stateless: there is no stream to open and no session to end.
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(method, e.mcpURL(), nil)
		req.Header.Set("Authorization", "Bearer "+w.read)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp: status=%d, want 405", method, response.StatusCode)
		}
	}
}

func TestMCPServesTheHostNameOfAReverseProxyOnALoopbackListener(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Proxy Desk", "admin@proxy.example.com")
	req, _ := http.NewRequest(http.MethodPost, e.mcpURL(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Host = "desk.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+w.read)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status=%d body=%s, want 200: a proxy on the same host forwards the public Host header", response.StatusCode, body)
	}
}

// Only a failing database is a reason for 503. Every way a token can be refused is a 401, so a client
// knows to ask for a new token instead of retrying.
func TestMCPAnswers401ForEveryRefusedToken(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Refused Desk", "admin@refused.example.com")
	issue := func(label string) string {
		status, issued := e.rest(http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": label, "scope": "read"}, w.admin)
		if status != http.StatusCreated {
			t.Fatalf("create token %q status=%d body=%v", label, status, issued)
		}
		return issued["token"].(string)
	}
	age := func(label, set string) {
		if _, err := e.pool.Exec(context.Background(), `UPDATE sessions SET `+set+` WHERE workspace_id=$1 AND label=$2`, w.id, label); err != nil {
			t.Fatal(err)
		}
	}
	pastTheCap, expired, revoked := issue("past the cap"), issue("expired"), issue("revoked")
	age("past the cap", "created_at=now()-interval '366 days'")
	age("expired", "expires_at=now()-interval '1 minute'")
	age("revoked", "revoked_at=now()")

	for name, token := range map[string]string{"past the one-year cap": pastTheCap, "expired": expired, "revoked": revoked, "unknown": "cdk_agent_" + strings.Repeat("A", 43)} {
		req, err := http.NewRequest(http.MethodPost, e.mcpURL(), strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized || !strings.HasPrefix(response.Header.Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%s token: status=%d WWW-Authenticate=%q body=%s, want 401 with a Bearer challenge", name, response.StatusCode, response.Header.Get("WWW-Authenticate"), body)
		}
	}
}
