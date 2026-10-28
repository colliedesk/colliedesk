package freshdesk_test

import (
	"context"
	"testing"
)

// An import never asks an AI provider for anything, even with triage on: every message it writes
// carries its origin, which the triage trigger skips. A later importer must keep setting origin.
func TestAnImportQueuesNoAITriageEvenWithTriageOn(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `INSERT INTO ai_slots(workspace_id,slot,uses_server,model,enabled,consented_at) VALUES($1,'triage',true,'claude-haiku-5-5',true,now())`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	env.mustRun(t)
	var conversations, tasks, suggestions int
	if err := env.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM conversations WHERE workspace_id=$1),
			(SELECT count(*) FROM ai_tasks WHERE workspace_id=$1 AND kind='triage'),
			(SELECT count(*) FROM ai_triage_suggestions WHERE workspace_id=$1)`, env.workspaceID).Scan(&conversations, &tasks, &suggestions); err != nil {
		t.Fatal(err)
	}
	if conversations == 0 {
		t.Fatal("the import wrote no conversation")
	}
	if tasks != 0 || suggestions != 0 {
		t.Fatalf("an import of %d conversations queued %d triage tasks and %d suggestions", conversations, tasks, suggestions)
	}
}
