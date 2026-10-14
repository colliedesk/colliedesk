package freshdesk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

func TestArchivedTicketAboveHighestListedIDIsImported(t *testing.T) {
	env := newImportEnv(t)
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s missing=%v failures=%+v verification=%+v", report.Status, report.Missing, report.Failures, report.Verification)
	}
	env.target(t, "ticket", "7")
	if report.Tickets.Archived != 2 || report.Tickets.HighestID != 7 || report.Tickets.HighestProbedID != 10 {
		t.Fatalf("tickets = %+v, want archived 3 and 7, highest 7, probed to 10", report.Tickets)
	}
	if !report.Verification.Tickets.Verified || !strings.Contains(report.Verification.Tickets.Method, "ticket id from 1 to 7") {
		t.Fatalf("ticket verification = %+v", report.Verification.Tickets)
	}
	if !report.Verification.Contacts.Verified {
		t.Fatalf("contact verification = %+v", report.Verification.Contacts)
	}
}

func TestAccountWithOnlyArchivedTicketsImportsThemInsteadOfReportingZero(t *testing.T) {
	env := newImportEnv(t)
	env.fake.allArchived = true
	report := env.mustRun(t)
	if report.Tickets.Source == 0 || (report.Status == freshdesk.StatusOK && report.Tickets.Imported == 0) {
		t.Fatalf("all-archived account reported %s with tickets %+v", report.Status, report.Tickets.EntityCount)
	}
	assertCount(t, "tickets", report.Tickets.EntityCount, 5, 5)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s missing=%v failures=%+v verification=%+v", report.Status, report.Missing, report.Failures, report.Verification)
	}
}

func TestReportIsUnverifiedWhenSourceTotalsCannotBeRead(t *testing.T) {
	env := newImportEnv(t)
	env.fake.failSearch = true
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || report.Verification.Contacts.Verified || len(report.Failures) != 0 {
		t.Fatalf("status=%s verification=%+v failures=%+v", report.Status, report.Verification, report.Failures)
	}
	if !strings.Contains(report.Markdown(), "UNVERIFIED") {
		t.Fatal("markdown does not say UNVERIFIED")
	}
}

func TestSearchTotalAboveAccountedTicketsIsUnverified(t *testing.T) {
	env := newImportEnv(t)
	env.fake.searchTicketTotal = 9
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || report.Verification.Tickets.Verified || !strings.Contains(report.Verification.Tickets.Detail, "9") {
		t.Fatalf("status=%s ticket verification=%+v", report.Status, report.Verification.Tickets)
	}
}

func TestArchiveEndpointForbiddenWarnsContinuesAndIsUnverified(t *testing.T) {
	env := newImportEnv(t)
	env.fake.archiveForbidden = true
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || !hasWarning(report, "archive_unavailable") || report.Verification.Tickets.Verified {
		t.Fatalf("status=%s warnings=%+v verification=%+v", report.Status, report.Warnings, report.Verification.Tickets)
	}
	if report.Tickets.Imported != 3 {
		t.Fatalf("live tickets imported = %d, want 3", report.Tickets.Imported)
	}
}

func TestArchiveProbeErrorFailsOnlyThatTicketID(t *testing.T) {
	env := newImportEnv(t)
	env.fake.archiveError = "3"
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || !hasFailure(report, "ticket", "3", "3") || report.Tickets.Imported != 4 {
		t.Fatalf("status=%s tickets=%+v failures=%+v", report.Status, report.Tickets, report.Failures)
	}
	env.fake.archiveError = ""
	if retry := env.mustRun(t); retry.Status != freshdesk.StatusOK || retry.Tickets.Imported != 5 {
		t.Fatalf("retry status=%s tickets=%+v", retry.Status, retry.Tickets)
	}
}

func TestLastTicketIDExtendsTheProbe(t *testing.T) {
	env := newImportEnv(t)
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.LastTicketID = 12 })
	if err != nil {
		t.Fatal(err)
	}
	if report.Tickets.HighestProbedID != 15 || !strings.Contains(report.Verification.Tickets.Method, "12") {
		t.Fatalf("tickets=%+v method=%q", report.Tickets, report.Verification.Tickets.Method)
	}
}
