package freshdesk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
		o.PaidStorageBytes = cap
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

func TestTrialImportIgnoresThePaidCap(t *testing.T) {
	env := newImportEnv(t)
	report, err := env.runCapped(t, 1)
	if err != nil || report.Status != "OK" {
		t.Fatalf("trial: report=%+v err=%v, want an import the paid cap does not touch", report, err)
	}
}

func TestTeamPlansCountImportedFilesTowardTheirCap(t *testing.T) {
	for _, plan := range []string{"team", "team25"} {
		t.Run(plan+" over the cap", func(t *testing.T) {
			env := newImportEnv(t)
			env.setPlan(t, env.workspaceID, plan)
			before := env.rowCounts(t)
			report, err := env.runCapped(t, 29)
			var capErr *freshdesk.StorageCapError
			if !errors.As(err, &capErr) || report != nil {
				t.Fatalf("report=%v err=%v, want a *StorageCapError and no report", report, err)
			}
			if strings.Contains(err.Error(), "Founding") {
				t.Fatalf("error %q names the Founding plan", err)
			}
			if after := env.rowCounts(t); after != before {
				t.Fatalf("rows changed from %s to %s", before, after)
			}
		})
		t.Run(plan+" at exactly the cap", func(t *testing.T) {
			env := newImportEnv(t)
			env.setPlan(t, env.workspaceID, plan)
			if report, err := env.runCapped(t, 30); err != nil || report.Status != "OK" {
				t.Fatalf("report=%+v err=%v, want it imported", report, err)
			}
		})
	}
}

func TestSelfHostedImportIgnoresTheFoundingCap(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.PaidStorageBytes = 1 })
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

// An import that passed the sizing check is admitted: bytes that arrive afterwards (customer mail,
// a file added at the source) may push usage a little past the cap, and the import still finishes.
func TestAdmittedFoundingImportIsNotStoppedByBytesThatArriveAfterSizing(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	var once sync.Once
	env.fake.onRequest = func(path string) {
		if !strings.HasPrefix(path, "/files/") {
			return
		}
		once.Do(func() {
			if _, err := env.pool.Exec(context.Background(), `UPDATE workspaces SET stored_storage_bytes=stored_storage_bytes+10 WHERE id=$1`, env.workspaceID); err != nil {
				t.Error(err)
			}
		})
	}
	report, err := env.runCapped(t, 30)
	if err != nil || report.Status != "OK" {
		t.Fatalf("report=%+v err=%v, want OK: an admitted import is not stopped by the cap", report, err)
	}
	if report.Attachments.EntityCount.Imported != 3 || env.count(t, "attachments") != 3 {
		t.Fatalf("attachments imported=%d rows=%d, want all 3", report.Attachments.EntityCount.Imported, env.count(t, "attachments"))
	}
}

func TestSizingRetriesATransientReadErrorOfOneTicket(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	// The client already retries a 5xx four times; five failures in a row exhaust it once.
	env.fake.failPathTimes = map[string]int{"/api/v2/tickets/6": 5}
	report, err := env.runCapped(t, 30)
	if err != nil || report.Status != "OK" {
		t.Fatalf("report=%+v err=%v, want OK after the sizing pass retried the ticket", report, err)
	}
}

func TestSizingStopsWithAClearReasonOnAPersistentError(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	env.fake.failPathTimes = map[string]int{"/api/v2/tickets/6": 1000}
	before := env.rowCounts(t)
	_, err := env.runCapped(t, 30)
	var capErr *freshdesk.StorageCapError
	if err == nil || errors.As(err, &capErr) {
		t.Fatalf("err=%v, want a sizing failure that is not a cap refusal", err)
	}
	for _, want := range []string{"ticket 6", "Nothing was imported"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q lacks %q", err, want)
		}
	}
	if after := env.rowCounts(t); after != before {
		t.Fatalf("rows changed from %s to %s", before, after)
	}
}

func TestFoundingDryRunSaysWhetherTheImportWouldBeRefused(t *testing.T) {
	env := newImportEnv(t)
	env.setPlan(t, env.workspaceID, "founding")
	before := env.rowCounts(t)
	dry := func(cap int64) *freshdesk.Report {
		report, err := env.run(t, context.Background(), func(o *freshdesk.Options) {
			o.DryRun, o.Cloud, o.PaidStorageBytes = true, true, cap
		})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	refused := dry(29)
	check := refused.StorageCap
	if check == nil || !check.WouldRefuse || check.NeededBytes != 30 || check.FreeBytes != 29 || check.LimitBytes != 29 {
		t.Fatalf("dry run over the cap: %+v, want WouldRefuse with 30 needed and 29 free", check)
	}
	if !strings.Contains(refused.Markdown(), "would be refused") {
		t.Fatalf("report does not say the import would be refused:\n%s", refused.Markdown())
	}
	if fits := dry(30).StorageCap; fits == nil || fits.WouldRefuse {
		t.Fatalf("dry run that fits: %+v, want WouldRefuse false", fits)
	}
	if after := env.rowCounts(t); after != before {
		t.Fatalf("a dry run wrote rows: %s -> %s", before, after)
	}
}

func TestStorageCapErrorOnSoloAndSmallPointsToTheNextPlan(t *testing.T) {
	for plan, want := range map[string]string{"solo": "of the Solo plan. Nothing was imported. Upgrade to the Small plan", "small": "of the Small plan. Nothing was imported. Upgrade to the Team plan"} {
		if message := (&freshdesk.StorageCapError{Plan: plan, Needed: 2 << 30, Used: 9 << 30, Limit: 10 << 30}).Error(); !strings.Contains(message, want) {
			t.Errorf("%s: %q, want %q", plan, message, want)
		}
	}
}

func TestStorageCapErrorUsesReadableSizes(t *testing.T) {
	message := (&freshdesk.StorageCapError{Needed: 3 << 30, Used: 19 << 30, Limit: 20 << 30}).Error()
	for _, want := range []string{"3 GiB", "1 GiB", "20 GiB"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error %q lacks %q", message, want)
		}
	}
	if strings.Contains(message, "21474836480") {
		t.Fatalf("error %q shows raw bytes", message)
	}
}

// A ticket whose conversations cannot be read (deleted between the two reads, or forbidden) cannot be
// sized and cannot be retried into success. The real import marks such a ticket failed and goes on,
// so the sizing skips it and counts it; it does not stop the whole import.
func TestSizingSkipsATicketWhoseConversationsAreGoneOrForbidden(t *testing.T) {
	for _, status := range []int{404, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			env := newImportEnv(t)
			env.setPlan(t, env.workspaceID, "founding")
			const path = "/api/v2/tickets/6/conversations"
			env.fake.failPathStatus = map[string]int{path: status}

			dry, err := env.run(t, context.Background(), func(o *freshdesk.Options) {
				o.DryRun, o.Cloud, o.PaidStorageBytes = true, true, 1000
			})
			if err != nil || dry.StorageCap == nil {
				t.Fatalf("status %d: dry run err=%v check=%+v, want a storage check", status, err, dry.StorageCap)
			}
			if got := strings.Join(dry.StorageCap.SkippedTickets, ","); got != "6" {
				t.Fatalf("status %d: skipped tickets = %q, want 6", status, got)
			}
			// One read by the dry run, not three: nothing that cannot succeed is retried.
			if hits := env.fake.count(path); hits != 1 {
				t.Fatalf("status %d: conversations of ticket 6 read %d times by the dry run, want 1", status, hits)
			}

			report, err := env.runCapped(t, 1000)
			if err != nil {
				t.Fatalf("status %d: the import stopped at sizing: %v", status, err)
			}
			if report.Status == "OK" || env.count(t, "conversations") == 0 {
				t.Fatalf("status %d: report=%s conversations=%d, want the other tickets imported and ticket 6 reported failed", status, report.Status, env.count(t, "conversations"))
			}
		})
	}
}

func TestReportNamesThePlanAsAPersonReadsIt(t *testing.T) {
	for plan, want := range map[string]string{"team25": "Team 25 plan", "team": "Team plan", "founding": "Founding plan"} {
		for _, refuse := range []bool{false, true} {
			report := &freshdesk.Report{StorageCap: &freshdesk.StorageCapCheck{Plan: plan, LimitBytes: 1 << 30, WouldRefuse: refuse}}
			markdown := report.Markdown()
			if !strings.Contains(markdown, want) || strings.Contains(markdown, "team25") {
				t.Errorf("plan %s refuse=%v: report lacks %q or shows the raw id:\n%s", plan, refuse, want, markdown)
			}
		}
	}
}
