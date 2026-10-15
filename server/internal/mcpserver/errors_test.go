package mcpserver_test

import (
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
