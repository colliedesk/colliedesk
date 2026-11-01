package mcpserver_test

import (
	"testing"
)

func TestOneMCPCallShowsTheMoveAndNextLink(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Move MCP", "admin@move-mcp.example")
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO workspace_onboarding(workspace_id,goal) VALUES($1,'leave_helpdesk')`, w.id); err != nil {
		t.Fatal(err)
	}
	session := e.connect(w.read, "2025-06-18")
	out := e.ok(session, "colliedesk_setup_progress_get", map[string]any{})
	steps := out["steps"].([]any)
	if len(steps) != 6 || out["next_url"] != "/settings/import-freshdesk" {
		t.Fatalf("move=%v", out)
	}
	if step := steps[1].(map[string]any); step["key"] != "preview" || step["url"] == nil {
		t.Fatalf("preview=%v", step)
	}
	admin := e.connect(w.adminTier, "2025-06-18")
	out = e.ok(admin, "colliedesk_setup_move_step_set", map[string]any{"step": "custom_domain", "state": "skipped"})
	if out["steps"].([]any)[2].(map[string]any)["state"] != "skipped" {
		t.Fatalf("skip=%v", out)
	}
	other := e.newWorkspace("Other MCP", "admin@other-move.example")
	denied := e.call(e.connect(other.adminTier, "2025-06-18"), "colliedesk_setup_move_step_set", map[string]any{"step": "preview", "state": "done"})
	if !denied.IsError {
		t.Fatal("another workspace confirmed a preview without public articles")
	}
}
