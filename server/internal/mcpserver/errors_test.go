package mcpserver_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/polar"
)

func TestLockedWorkspaceSaysOnlyExportWorks(t *testing.T) {
	created := time.Now().UTC()
	clock := created
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.Billing = polar.Config{AccessToken: "token-for-test", WebhookSecret: "secret-for-test", APIURL: "http://127.0.0.1:1"}
		config.Now = func() time.Time { return clock }
	})
	w := e.newWorkspace("Locked Desk", "admin@locked.example.com")
	session := e.connect(w.draft, "")
	if out := e.ok(session, "colliedesk_inbox_search", nil); out["conversations"] == nil {
		t.Fatalf("a workspace in its trial must work: %v", out)
	}

	clock = created.Add(20 * 24 * time.Hour)
	for _, tool := range []string{"colliedesk_inbox_search", "colliedesk_tags_list", "colliedesk_agents_list"} {
		message := e.fails(session, tool, nil)
		if !strings.Contains(message, "locked") || !strings.Contains(message, "export") {
			t.Errorf("%s on a locked workspace: %q, want it to say the workspace is locked and only export works", tool, message)
		}
	}
	who := e.ok(session, "colliedesk_whoami", nil)
	if who["workspace_locked"] != true {
		t.Errorf("whoami must still answer on a locked workspace and say so: %v", who)
	}
}

func TestToolArgumentsAreValidatedAgainstTheSchema(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Args Desk", "admin@args.example.com")
	session := e.connect(w.read, "")
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"colliedesk_inbox_search", map[string]any{"status": "done"}, "status"},
		{"colliedesk_inbox_search", map[string]any{"assignee": "bob"}, "assignee"},
		{"colliedesk_inbox_search", map[string]any{"detail": "everything"}, "detail"},
		{"colliedesk_inbox_search", map[string]any{"surprise": true}, "surprise"},
		{"colliedesk_conversation_get", nil, "conversation_id"},
	} {
		message := e.fails(session, c.tool, c.args)
		if !strings.Contains(message, c.want) || !strings.HasPrefix(message, "Invalid arguments") {
			t.Errorf("%s %v: message %q, want it to start with Invalid arguments and name %q", c.tool, c.args, message, c.want)
		}
	}
}

func TestAnswerTooLargeForOneCallIsExplainedNotAnInternalError(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Huge Desk", "admin@huge.example.com")
	contact := e.created("/api/v1/contacts", map[string]any{"name": "Wordy", "email": "wordy@huge.example.com"}, w.admin)
	conversation := e.created("/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Huge thread"}, w.admin)
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO messages(workspace_id,conversation_id,kind,body) VALUES($1,$2,'incoming',repeat('x', 17*1024*1024))`, w.id, conversation); err != nil {
		t.Fatal(err)
	}
	message := e.fails(e.connect(w.read, ""), "colliedesk_conversation_get", map[string]any{"conversation_id": conversation})
	if !strings.Contains(message, "too large") || strings.Contains(message, "internal problem") {
		t.Errorf("message=%q, want a plain too large explanation", message)
	}
}
