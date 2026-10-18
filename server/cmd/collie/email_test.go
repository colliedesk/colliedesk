package main

import (
	"context"
	"strings"
	"testing"
)

func (e *trialEnv) pause(reason string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE workspaces SET email_paused_at=now(),email_pause_reason=$2 WHERE id=$1`, e.workspaceID, reason); err != nil {
		e.t.Fatal(err)
	}
}

func (e *trialEnv) paused() bool {
	e.t.Helper()
	var paused bool
	if err := e.pool.QueryRow(context.Background(), `SELECT email_paused_at IS NOT NULL FROM workspaces WHERE id=$1`, e.workspaceID).Scan(&paused); err != nil {
		e.t.Fatal(err)
	}
	return paused
}

func TestEmailResumeClearsOnlyTheNamedWorkspacesPause(t *testing.T) {
	env := newTrialEnv(t, "Pause Desk")
	other := env.withWorkspace("Pause Other")
	env.pause("3 spam complaints on 100 replies in the last 7 days.")
	other.pause("4 spam complaints on 200 replies in the last 7 days.")

	output := env.mustRun("email", "resume", "pause-desk")
	if env.paused() {
		t.Fatal("email resume left the workspace paused")
	}
	if !other.paused() {
		t.Fatal("resuming one workspace cleared another's pause")
	}
	if !strings.Contains(output, "pause-desk") || !strings.Contains(output, "3 spam complaints") {
		t.Fatalf("output must name the workspace and the reason that was lifted:\n%s", output)
	}
}

func TestEmailResumeOnAWorkspaceThatIsNotPausedSaysSo(t *testing.T) {
	env := newTrialEnv(t, "Pause Idle")
	output := env.mustRun("email", "resume", "pause-idle")
	if !strings.Contains(output, "not paused") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestEmailResumeRejectsBadInput(t *testing.T) {
	env := newTrialEnv(t, "Pause Bad")
	env.pause("3 spam complaints on 100 replies in the last 7 days.")
	for name, args := range map[string][]string{
		"no workspace":      {"email", "resume"},
		"two workspaces":    {"email", "resume", "pause-bad", "other"},
		"unknown workspace": {"email", "resume", "nobody"},
		"unknown verb":      {"email", "pause", "pause-bad"},
		"no verb":           {"email"},
	} {
		if _, err := env.run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if !env.paused() {
		t.Fatal("a rejected command cleared the pause")
	}
}
