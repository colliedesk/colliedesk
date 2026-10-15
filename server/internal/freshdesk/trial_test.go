package freshdesk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

var (
	trialEnd14   = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC) // trial started 2026-10-01T09:00Z
	trialEnd30   = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	importDay10  = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	importLocked = time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
)

func (e *importEnv) setTrialEnd(t *testing.T, workspaceID string, ends time.Time) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `UPDATE workspaces SET trial_ends_at=$2, trial_extended_at=NULL WHERE id=$1`, workspaceID, ends); err != nil {
		t.Fatal(err)
	}
}

func (e *importEnv) trial(t *testing.T, workspaceID string) (ends time.Time, extended bool) {
	t.Helper()
	var extendedAt *time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT trial_ends_at, trial_extended_at FROM workspaces WHERE id=$1`, workspaceID).Scan(&ends, &extendedAt); err != nil {
		t.Fatal(err)
	}
	return ends.UTC(), extendedAt != nil
}

func (e *importEnv) runAt(t *testing.T, now time.Time, mutate func(*freshdesk.Options)) (*freshdesk.Report, error) {
	t.Helper()
	return e.run(t, context.Background(), func(o *freshdesk.Options) {
		o.Now = func() time.Time { return now }
		if mutate != nil {
			mutate(o)
		}
	})
}

func TestImportOnDay10ExtendsTheTrialToThirtyDaysOnce(t *testing.T) {
	env := newImportEnv(t)
	env.setTrialEnd(t, env.workspaceID, trialEnd14)
	report, err := env.runAt(t, importDay10, nil)
	if err != nil {
		t.Fatal(err)
	}
	ends, extended := env.trial(t, env.workspaceID)
	if !ends.Equal(trialEnd30) || !extended {
		t.Fatalf("trial ends %s extended=%v, want %s extended", ends, extended, trialEnd30)
	}
	if report.TrialExtendedTo == nil || !report.TrialExtendedTo.Equal(trialEnd30) {
		t.Fatalf("report.TrialExtendedTo = %v, want %s", report.TrialExtendedTo, trialEnd30)
	}
	if !strings.Contains(report.Markdown(), "trial: extended to 2026-10-31T09:00:00Z") {
		t.Fatalf("report does not name the extension:\n%s", report.Markdown())
	}

	again, err := env.runAt(t, importDay10.Add(24*time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	ends, _ = env.trial(t, env.workspaceID)
	if !ends.Equal(trialEnd30) || again.TrialExtendedTo != nil || strings.Contains(again.Markdown(), "trial: extended") {
		t.Fatalf("second run: trial ends %s, extendedTo %v", ends, again.TrialExtendedTo)
	}
}

func TestImportAfterTheTrialEndedDoesNotUnlock(t *testing.T) {
	env := newImportEnv(t)
	env.setTrialEnd(t, env.workspaceID, trialEnd14)
	report, err := env.runAt(t, importLocked, nil)
	if err != nil {
		t.Fatal(err)
	}
	ends, extended := env.trial(t, env.workspaceID)
	if !ends.Equal(trialEnd14) || extended || report.TrialExtendedTo != nil {
		t.Fatalf("locked workspace: trial ends %s extended=%v report=%v", ends, extended, report.TrialExtendedTo)
	}
}

func TestImportThatStopsBeforeWritingOrWritesNothingNeverExtends(t *testing.T) {
	env := newImportEnv(t)
	env.setTrialEnd(t, env.workspaceID, trialEnd14)
	if _, err := env.runAt(t, importDay10, func(o *freshdesk.Options) { o.APIKey = testAPIKey + "-wrong" }); err == nil {
		t.Fatal("wrong API key was accepted")
	}
	if _, err := env.runAt(t, importDay10, func(o *freshdesk.Options) { o.DryRun = true }); err != nil {
		t.Fatal(err)
	}
	if ends, extended := env.trial(t, env.workspaceID); !ends.Equal(trialEnd14) || extended {
		t.Fatalf("trial ends %s extended=%v after a refused run and a dry run", ends, extended)
	}
}

func TestUndoDoesNotRevokeTheExtension(t *testing.T) {
	env := newImportEnv(t)
	env.setTrialEnd(t, env.workspaceID, trialEnd14)
	if _, err := env.runAt(t, importDay10, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"}); err != nil {
		t.Fatal(err)
	}
	if ends, extended := env.trial(t, env.workspaceID); !ends.Equal(trialEnd30) || !extended {
		t.Fatalf("trial ends %s extended=%v after undo, want %s", ends, extended, trialEnd30)
	}
}

func TestImportIntoOneWorkspaceNeverChangesAnothersTrial(t *testing.T) {
	env := newImportEnv(t)
	other, _, err := env.auth.CreateWorkspace(context.Background(), "Other Desk", "bob@other.example")
	if err != nil {
		t.Fatal(err)
	}
	env.setTrialEnd(t, env.workspaceID, trialEnd14)
	env.setTrialEnd(t, other.ID, trialEnd14)
	if _, err := env.runAt(t, importDay10, nil); err != nil {
		t.Fatal(err)
	}
	if ends, extended := env.trial(t, other.ID); !ends.Equal(trialEnd14) || extended {
		t.Fatalf("other workspace trial ends %s extended=%v", ends, extended)
	}
}
