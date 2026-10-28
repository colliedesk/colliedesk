package mcpserver_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/results"
)

// An AI agent reads the results page and the conversations behind a number, and never another
// workspace's.
func TestResultsToolShowsTheNumbersAndInboxSearchOpensThem(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE conversations SET created_at=now()-interval '1 hour' WHERE id=ANY($1::uuid[])`, []string{f.c1, f.c2}); err != nil {
		t.Fatal(err)
	}
	// c1: a customer wrote, the team answered 10 minutes later; c2: a customer wrote, no answer yet.
	for _, step := range []struct{ conversation, kind, after string }{{f.c1, "incoming", "0"}, {f.c1, "outgoing", "10 minutes"}, {f.c2, "incoming", "0"}} {
		if _, err := e.pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at)
			SELECT workspace_id,id,$2,'Text',created_at + $3::interval FROM conversations WHERE id=$1`, step.conversation, step.kind, step.after); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := results.NewWorker(database.New(e.pool)).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	session := e.connect(f.w.read, "")
	out := e.ok(session, "colliedesk_results_get", nil)
	snapshot, _ := at(t, out, "results", "current", "id").(string)
	if snapshot == "" || at(t, out, "results", "current", "windows", "30d", "first_response", "answered") != float64(1) || at(t, out, "results", "baseline_state") != "none" {
		t.Fatalf("results = %v", out)
	}
	answered := e.ok(session, "colliedesk_inbox_search", map[string]any{"result": snapshot + ":30d.first_response"})
	if ids := conversationIDs(t, answered); len(ids) != 1 || ids[0] != f.c1 {
		t.Errorf("answered = %v, want %s", ids, f.c1)
	}
	waiting := e.ok(session, "colliedesk_inbox_search", map[string]any{"result": snapshot + ":30d.first_response_waiting"})
	if ids := conversationIDs(t, waiting); len(ids) != 1 || ids[0] != f.c2 {
		t.Errorf("waiting = %v, want %s", ids, f.c2)
	}
	if message := e.fails(e.connect(f.other.read, ""), "colliedesk_inbox_search", map[string]any{"result": snapshot + ":30d.first_response"}); message == "" {
		t.Error("another workspace opened this workspace's snapshot")
	}
}
