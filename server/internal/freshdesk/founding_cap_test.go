package freshdesk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

// The fixture account holds three attachments of 10 bytes each: 30 bytes. A small cap stands in
// for the Founding plan's 20 GiB.

func (e *importEnv) setPlan(t *testing.T, workspaceID, plan string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,current_period_end,source_modified_at)
		VALUES($1::uuid,'sub_'||$1::text,$2,'cus_test','active','2099-01-01T00:00:00Z','2026-10-01T09:00:00Z')`, workspaceID, plan); err != nil {
		t.Fatal(err)
	}
}

func (e *importEnv) storedBytes(t *testing.T, workspaceID string) int64 {
	t.Helper()
	var stored int64
	if err := e.pool.QueryRow(context.Background(), `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`, workspaceID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	return stored
}

func (e *importEnv) runCapped(t *testing.T, cap int64) (*freshdesk.Report, error) {
	t.Helper()
	return e.run(t, context.Background(), func(o *freshdesk.Options) {
		o.Cloud = true
		o.FoundingStorageBytes = cap
	})
}

func (e *importEnv) rowCounts(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s import_items=%d", e.tableCounts(t), e.count(t, "import_items"))
}

func TestFoundingImportOverTheCapWritesNothing(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	before := env.rowCounts(t)

	report, err := env.runCapped(t, 29)
	var capErr *freshdesk.StorageCapError
	if !errors.As(err, &capErr) || report != nil {
		t.Fatalf("report=%v err=%v, want a *StorageCapError and no report", report, err)
	}
	for _, want := range []string{"30 bytes", "29 bytes", "Team"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
	if after := env.rowCounts(t); after != before {
		t.Fatalf("rows changed from %s to %s: a refused import must write nothing", before, after)
	}
	if env.storedBytes(t, env.workspaceID) != 0 {
		t.Fatalf("stored bytes = %d, want 0", env.storedBytes(t, env.workspaceID))
	}
	if env.fake.count("/files/9003") != 0 {
		t.Fatal("an attachment was downloaded although the import was refused")
	}
}

func TestFoundingImportThatFitsExactlyIsImportedAndCounted(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	report, err := env.runCapped(t, 30)
	if err != nil || report.Status != "OK" {
		t.Fatalf("report=%+v err=%v, want OK", report, err)
	}
	if got := env.storedBytes(t, env.workspaceID); got != 30 {
		t.Fatalf("stored bytes = %d, want the 30 imported bytes counted", got)
	}
	var counted int64
	if err := env.pool.QueryRow(context.Background(), `SELECT counted_storage_bytes FROM workspaces WHERE id=$1`, env.workspaceID).Scan(&counted); err != nil || counted != 0 {
		t.Fatalf("counted bytes = %d (%v), want 0: the trial counter ignores imports", counted, err)
	}
}

func TestFoundingImportCountsWhatTheWorkspaceAlreadyHolds(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	if _, err := env.pool.Exec(context.Background(), `UPDATE workspaces SET stored_storage_bytes=5 WHERE id=$1`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	before := env.rowCounts(t)
	if _, err := env.runCapped(t, 34); err == nil || !strings.Contains(err.Error(), "30 bytes") || !strings.Contains(err.Error(), "29 bytes") {
		t.Fatalf("err=%v, want a refusal naming 30 bytes needed and 29 free: 5 held + 30 needed passes 34", err)
	}
	if after := env.rowCounts(t); after != before {
		t.Fatalf("rows changed from %s to %s", before, after)
	}
	if report, err := env.runCapped(t, 35); err != nil || report.Status != "OK" {
		t.Fatalf("5 held + 30 needed fits 35: report=%+v err=%v", report, err)
	}
}

func TestFoundingImportUndoReleasesTheBytes(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	if _, err := env.runCapped(t, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"}); err != nil {
		t.Fatal(err)
	}
	if got := env.storedBytes(t, env.workspaceID); got != 0 {
		t.Fatalf("stored bytes after undo = %d, want 0", got)
	}
	if report, err := env.runCapped(t, 30); err != nil || report.Status != "OK" {
		t.Fatalf("import after undo: report=%+v err=%v, want OK again", report, err)
	}
}

func TestTrialAndTeamImportsIgnoreTheFoundingCap(t *testing.T) {
	for _, plan := range []string{"", "team"} {
		env := newImportEnv(t)
		if plan != "" {
			env.setPlan(t, env.workspaceID, plan)
		}
		report, err := env.runCapped(t, 1)
		if err != nil || report.Status != "OK" {
			t.Fatalf("plan %q: report=%+v err=%v, want an uncapped import", plan, report, err)
		}
	}
}

func TestSelfHostedImportIgnoresTheFoundingCap(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.FoundingStorageBytes = 1 })
	if err != nil || report.Status != "OK" {
		t.Fatalf("self-hosted run (Cloud false): report=%+v err=%v, want OK", report, err)
	}
}

func TestAnotherWorkspacesBytesDoNotCountForTheImport(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	other, _, err := env.auth.CreateWorkspace(context.Background(), "Other Desk", "bob@other.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(context.Background(), `UPDATE workspaces SET stored_storage_bytes=1000000 WHERE id=$1`, other.ID); err != nil {
		t.Fatal(err)
	}
	if report, err := env.runCapped(t, 30); err != nil || report.Status != "OK" {
		t.Fatalf("report=%+v err=%v, want OK: the other workspace's bytes are not ours", report, err)
	}
	var otherStored int64
	if err := env.pool.QueryRow(context.Background(), `SELECT stored_storage_bytes FROM workspaces WHERE id=$1`, other.ID).Scan(&otherStored); err != nil || otherStored != 1000000 {
		t.Fatalf("other workspace stored = %d (%v), want it untouched", otherStored, err)
	}
}
