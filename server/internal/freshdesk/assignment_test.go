package freshdesk_test

import (
	"context"
	"testing"
)

// An imported assignment is history, not news: when a re-import changes the assignee, no earlier
// actor or timestamp stays attached to the new assignee, so nobody is notified.
func TestImportedAssigneeCarriesNoAssignmentActivity(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	ctx := context.Background()
	var conversation, assignee string
	if err := env.pool.QueryRow(ctx, `SELECT id::text,assignee_id::text FROM conversations WHERE workspace_id=$1 AND assignee_id IS NOT NULL LIMIT 1`, env.workspaceID).Scan(&conversation, &assignee); err != nil {
		t.Fatalf("fixture has no assigned conversation: %v", err)
	}
	// Stale state: assigned to nobody now, but an old actor and time are still recorded.
	if _, err := env.pool.Exec(ctx, `UPDATE conversations SET assignee_id=NULL,assigned_at=now(),assigned_by_agent_id=$3 WHERE workspace_id=$1 AND id=$2`, env.workspaceID, conversation, assignee); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE import_items SET status='failed' WHERE workspace_id=$1 AND entity='ticket' AND target_id=$2`, env.workspaceID, conversation); err != nil {
		t.Fatal(err)
	}
	env.mustRun(t)

	var gotAssignee *string
	var assignedAt, assignedBy *string
	if err := env.pool.QueryRow(ctx, `SELECT assignee_id::text,assigned_at::text,assigned_by_agent_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`,
		env.workspaceID, conversation).Scan(&gotAssignee, &assignedAt, &assignedBy); err != nil {
		t.Fatal(err)
	}
	if gotAssignee == nil || *gotAssignee != assignee {
		t.Fatalf("re-import did not restore the assignee: %v", gotAssignee)
	}
	if assignedAt != nil || assignedBy != nil {
		t.Fatalf("imported assignee keeps assignment activity: at=%v by=%v", assignedAt, assignedBy)
	}
}
