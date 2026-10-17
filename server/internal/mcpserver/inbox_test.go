package mcpserver_test

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var utcSeconds = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

// inboxFixture: three conversations of workspace A, newest first c3, c2, c1, and one of workspace B.
type inboxFixture struct {
	w                workspace
	c1, c2, c3       string
	adminID          string
	other            workspace
	otherConvID      string
	otherSubject     string
	otherContactMail string
}

func seedInbox(t *testing.T, e *env) inboxFixture {
	t.Helper()
	f := inboxFixture{w: e.newWorkspace("Inbox Desk", "admin@inbox.example.com")}
	e.nameAgents(f.w, "Ada Admin")
	_, me := e.rest(http.MethodGet, "/api/v1/auth/me", nil, f.w.admin)
	f.adminID = at(t, me, "agent", "id").(string)
	grace := e.created("/api/v1/contacts", map[string]any{"name": "Grace", "email": "grace@cust.example.com"}, f.w.admin)
	alan := e.created("/api/v1/contacts", map[string]any{"name": "Alan", "email": "alan@cust.example.com"}, f.w.admin)
	billing := e.created("/api/v1/tags", map[string]any{"name": "billing"}, f.w.admin)

	f.c1 = e.created("/api/v1/conversations", map[string]any{"contact_id": grace, "subject": "Printer jam"}, f.w.admin)
	e.rest(http.MethodPatch, "/api/v1/conversations/"+f.c1, map[string]any{"status": "pending", "assignee_id": f.adminID}, f.w.admin)
	e.rest(http.MethodPut, "/api/v1/conversations/"+f.c1+"/tags/"+billing, nil, f.w.admin)
	f.c2 = e.created("/api/v1/conversations", map[string]any{"contact_id": alan, "subject": "Refund request"}, f.w.admin)
	f.c3 = e.created("/api/v1/conversations", map[string]any{"contact_id": grace, "subject": "Login broken"}, f.w.admin)

	f.other = e.newWorkspace("Other Desk", "admin@other.example.com")
	f.otherContactMail = "bravo@secret.example.com"
	f.otherSubject = "Bravo confidential subject"
	bravo := e.created("/api/v1/contacts", map[string]any{"name": "Bravo Customer", "email": f.otherContactMail}, f.other.admin)
	f.otherConvID = e.created("/api/v1/conversations", map[string]any{"contact_id": bravo, "subject": f.otherSubject}, f.other.admin)
	return f
}

func conversationIDs(t *testing.T, out map[string]any) []string {
	t.Helper()
	var ids []string
	items, _ := out["conversations"].([]any)
	for _, item := range items {
		ids = append(ids, at(t, item, "id").(string))
	}
	return ids
}

func TestInboxSearchReturnsReadableConversationsAndACursor(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")

	first := e.ok(session, "colliedesk_inbox_search", map[string]any{"limit": 2})
	if got := conversationIDs(t, first); !slices.Equal(got, []string{f.c3, f.c2}) {
		t.Fatalf("first page=%v, want [%s %s] newest first", got, f.c3, f.c2)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" {
		t.Fatalf("a full page must carry next_cursor: %v", first)
	}
	if got := first["timezone"]; got != "UTC" {
		t.Errorf("timezone=%v, want the workspace timezone UTC", got)
	}
	c3 := at(t, first, "conversations", 0)
	if at(t, c3, "assignee") != nil || len(at(t, c3, "tags").([]any)) != 0 || at(t, c3, "status") != "open" || at(t, c3, "priority") != "normal" {
		t.Errorf("unassigned open conversation rendered as %v", c3)
	}
	if at(t, c3, "untrusted_customer_content", "subject") != "Login broken" || at(t, c3, "untrusted_customer_content", "contact", "name") != "Grace" ||
		at(t, c3, "untrusted_customer_content", "contact", "email") != "grace@cust.example.com" {
		t.Errorf("subject or contact of c3 not readable: %v", c3)
	}
	if !utcSeconds.MatchString(at(t, c3, "updated_at").(string)) {
		t.Errorf("updated_at=%v, want UTC to the second", at(t, c3, "updated_at"))
	}

	second := e.ok(session, "colliedesk_inbox_search", map[string]any{"limit": 2, "cursor": cursor})
	if got := conversationIDs(t, second); !slices.Equal(got, []string{f.c1}) {
		t.Fatalf("second page=%v, want [%s]", got, f.c1)
	}
	if second["next_cursor"] != nil {
		t.Errorf("last page must end the cursor, got %v", second["next_cursor"])
	}
	c1 := at(t, second, "conversations", 0)
	if at(t, c1, "status") != "pending" || at(t, c1, "assignee", "name") != "Ada Admin" || at(t, c1, "assignee", "email") != "admin@inbox.example.com" ||
		!slices.Equal(at(t, c1, "tags").([]any), []any{"billing"}) {
		t.Errorf("assigned pending tagged c1 rendered as %v", c1)
	}
}

func TestInboxSearchFilters(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")
	cases := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"status", map[string]any{"status": "pending"}, []string{f.c1}},
		{"unassigned", map[string]any{"assignee": "unassigned"}, []string{f.c3, f.c2}},
		{"mine", map[string]any{"assignee": "me"}, []string{f.c1}},
		{"anyone", map[string]any{"assignee": "anyone"}, []string{f.c1}},
		{"tag by name, any case", map[string]any{"tag": "BILLING"}, []string{f.c1}},
		{"text", map[string]any{"q": "refund"}, []string{f.c2}},
	}
	for _, c := range cases {
		if got := conversationIDs(t, e.ok(session, "colliedesk_inbox_search", c.args)); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if msg := e.fails(session, "colliedesk_inbox_search", map[string]any{"tag": "nonesuch"}); !strings.Contains(msg, "No tag named") || !strings.Contains(msg, "billing") {
		t.Errorf("unknown tag message=%q, want it to list the existing tags", msg)
	}
	if msg := e.fails(session, "colliedesk_inbox_search", map[string]any{"limit": 51}); !strings.Contains(msg, "limit") {
		t.Errorf("limit 51 message=%q, want it to name limit", msg)
	}
	if msg := e.fails(session, "colliedesk_inbox_search", map[string]any{"cursor": "garbage"}); !strings.Contains(msg, "cursor") {
		t.Errorf("garbage cursor message=%q", msg)
	}
	page := e.ok(session, "colliedesk_inbox_search", map[string]any{"limit": 1, "status": "open"})
	if msg := e.fails(session, "colliedesk_inbox_search", map[string]any{"limit": 1, "status": "pending", "cursor": page["next_cursor"]}); !strings.Contains(msg, "cursor") {
		t.Errorf("cursor of another search accepted: %q", msg)
	}
	full := e.ok(session, "colliedesk_inbox_search", map[string]any{"status": "pending", "detail": "full"})
	c1 := at(t, full, "conversations", 0)
	if !utcSeconds.MatchString(at(t, c1, "created_at").(string)) {
		t.Errorf("detail=full must add created_at: %v", c1)
	}
}

func TestInboxSearchNeverShowsAnotherWorkspace(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	for _, query := range []map[string]any{nil, {"q": "confidential"}, {"q": "bravo@secret"}, {"detail": "full", "limit": 50}} {
		result := e.call(e.connect(f.w.read, ""), "colliedesk_inbox_search", query)
		blob := text(result)
		for _, secret := range []string{f.otherConvID, f.otherSubject, f.otherContactMail, "Bravo Customer"} {
			if strings.Contains(blob, secret) {
				t.Errorf("query %v leaked %q of another workspace: %s", query, secret, blob)
			}
		}
	}
}
