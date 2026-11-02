package mcpserver_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// A token that holds workspace:export gets the export at once, as a one-time link or, while small, the
// ZIP itself; a token without it is refused.
func TestWorkspaceExportGivesTheLinkOrTheFileAtOnce(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Export Tools Desk", "admin@export-tools.example.com")
	refusedWithoutPermission(t, e, w.send, "colliedesk_workspace_export", nil)

	admin := e.connect(w.adminTier, "")
	link := e.ok(admin, "colliedesk_workspace_export", nil)
	if url, _ := link["url"].(string); !strings.HasPrefix(url, "/api/v1/export/download/") || link["expires_in_seconds"] == nil {
		t.Fatalf("link = %v", link)
	}
	file := e.ok(admin, "colliedesk_workspace_export", map[string]any{"delivery": "download"})
	data, err := base64.StdEncoding.DecodeString(file["data_base64"].(string))
	if err != nil || len(data) < 4 || string(data[:2]) != "PK" || file["size"] != float64(len(data)) {
		t.Fatalf("download = %v (%v), want the ZIP", file["filename"], err)
	}
}

// A reply draft reaches no customer: it waits above the reply box for a person, and a token without
// ai_assist:create is refused.
func TestReplyDraftLeavesAWrittenDraftForAPerson(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	refusedWithoutPermission(t, e, f.w.read, "colliedesk_reply_draft", map[string]any{"conversation_id": f.c2, "body": "Hello"})

	out := e.ok(e.connect(f.w.draft, ""), "colliedesk_reply_draft", map[string]any{"conversation_id": f.c2, "body": "Your refund is on its way."})
	if at(t, out, "draft", "conversation_id") != f.c2 || at(t, out, "draft", "status") != "ready" || !strings.Contains(out["next"].(string), "Nothing reached the customer") {
		t.Fatalf("draft = %v", out)
	}
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 0 {
		t.Fatalf("a draft added messages: %v", kinds)
	}
	status, page := e.rest(http.MethodGet, "/api/v1/conversations?has_pending_draft=true", nil, f.w.admin)
	if status != http.StatusOK || !strings.Contains(stringify(page), f.c2) {
		t.Fatalf("has_pending_draft: %d %v, want the conversation", status, page)
	}
}
