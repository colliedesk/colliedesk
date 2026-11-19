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
	// The guide reads no workspace data, so it keeps working; the user guide says so.
	if docs := e.ok(session, "colliedesk_docs_search", map[string]any{"query": "agent token"}); docs["count"] == float64(0) {
		t.Errorf("docs_search must still answer on a locked workspace: %v", docs)
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

// A wrong argument name says which name to use, and a missing one says what to put in it, so the next
// call is the right one instead of a guess.
func TestAnInvalidCallSaysWhichArgumentToUse(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Hint Desk", "admin@hint.example.com")
	session := e.connect(w.adminTier, "")
	id := "9758712d-1cd3-4eea-b8b2-881a7cac46da"
	for _, c := range []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"colliedesk_inbox_search", map[string]any{"query": "refund"}, []string{"Did you mean q instead of query?"}},
		{"colliedesk_inbox_search", map[string]any{"search": "refund"}, []string{"colliedesk_inbox_search takes: ", " q, "}},
		{"colliedesk_conversation_get", map[string]any{"id": id}, []string{"Did you mean conversation_id instead of id?"}},
		{"colliedesk_conversation_update", map[string]any{"conversation_id": id, "assignee_id": id}, []string{"Did you mean assignee instead of assignee_id?"}},
		{"colliedesk_conversation_update", map[string]any{"conversation_id": id, "tags": []string{"billing"}}, []string{"Did you mean add_tags or remove_tags instead of tags?"}},
		{"colliedesk_reply_send", map[string]any{"conversation_id": id, "body": "Hi"},
			[]string{"seen_reply_number: latest_reply_number from the colliedesk_conversation_get you based this reply on."}},
	} {
		message := e.fails(session, c.tool, c.args)
		for _, want := range c.want {
			if !strings.HasPrefix(message, "Invalid arguments") || !strings.Contains(message, want) {
				t.Errorf("%s %v: message %q, want it to say %q", c.tool, c.args, message, want)
			}
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

// A refused field is named with why, whatever the status (400 or 422), so an agent fixes its next call.
func TestARefusedFieldIsNamedWithWhy(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Field Desk", "admin@field.example.com")
	session := e.connect(w.adminTier, "")
	created := e.ok(session, "colliedesk_channels_create", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": "5550001", "app_id": "700003"}})
	channel, _ := created["channel"].(map[string]any)
	id, _ := channel["id"].(string)
	message := e.fails(session, "colliedesk_channels_update", map[string]any{"channel_id": id, "config": map[string]any{"site_verification": "not a code"}})
	if !strings.Contains(message, "site_verification") || !strings.Contains(message, "8 to 100") {
		t.Fatalf("a wrong code: %q, want it to name site_verification and its rule", message)
	}
}
