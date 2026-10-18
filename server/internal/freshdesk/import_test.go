package freshdesk_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

const fullImportRows = "contacts=4 conversations=5 messages=11 attachments=3 time_entries=3 tags=4 conversation_tags=4"

func TestFullImportReconcilesEveryEntity(t *testing.T) {
	env := newImportEnv(t)
	report := env.mustRun(t)

	if report.Status != "OK" {
		t.Fatalf("status=%s missing=%v failures=%+v", report.Status, report.Missing, report.Failures)
	}
	// Fixture: 3 listed contacts + requester 299 fetched on demand.
	assertCount(t, "contacts", report.Contacts, 4, 4)
	// Listed 1,2,4,6 + archived 3 and 7 found by probing; 4 vanished at source.
	assertCount(t, "tickets", report.Tickets.EntityCount, 5, 5)
	// 5 descriptions + 3 + 1 + 1 + 1 conversations.
	assertCount(t, "messages", report.Messages, 11, 11)
	assertCount(t, "attachments", report.Attachments.EntityCount, 3, 3)
	assertCount(t, "time entries", report.TimeEntries, 3, 3)
	if report.Attachments.SourceBytes != 30 || report.Attachments.ImportedBytes != 30 || report.Attachments.ChecksumVerified != 3 {
		t.Fatalf("attachment bytes = %+v", report.Attachments)
	}
	if strings.Join(report.Tickets.DeletedAtSource, ",") != "4" || strings.Join(report.Tickets.Trashed, ",") != "5" {
		t.Fatalf("ticket exclusions = %+v", report.Tickets)
	}
	if report.Agents.Source != 2 || report.Agents.Matched != 1 || strings.Join(report.Agents.Unmatched, ",") != "bob@old.example" {
		t.Fatalf("agents = %+v", report.Agents)
	}
	if report.Companies.Source != 2 || report.Companies.Attached != 1 || strings.Join(report.Companies.Unattached, ",") != "502" {
		t.Fatalf("companies = %+v", report.Companies)
	}
	if got := env.tableCounts(t); got != fullImportRows {
		t.Fatalf("rows = %s", got)
	}
	for _, code := range []string{"custom_status_mapped", "contact_without_email", "agent_not_matched", "company_without_contacts"} {
		if !hasWarning(report, code) {
			t.Errorf("missing warning %q in %+v", code, report.Warnings)
		}
	}
	if env.fake.fileAuthSeen {
		t.Fatal("the Freshdesk API key was sent to the attachment host")
	}
}

func TestRerunIsANoOp(t *testing.T) {
	env := newImportEnv(t)
	first := env.mustRun(t)
	before := env.tableCounts(t)
	viewsBefore, filesBefore := env.fake.count("/api/v2/tickets/1"), env.fake.count("/files/9002")

	second := env.mustRun(t)
	if second.Status != "OK" || env.tableCounts(t) != before {
		t.Fatalf("re-run status=%s rows %s -> %s", second.Status, before, env.tableCounts(t))
	}
	if env.fake.count("/api/v2/tickets/1") != viewsBefore || env.fake.count("/files/9002") != filesBefore {
		t.Fatal("re-run fetched an already imported ticket again")
	}
	assertCount(t, "messages", second.Messages, first.Messages.Source, first.Messages.Imported)
	assertCount(t, "attachments", second.Attachments.EntityCount, 3, 3)
}

func TestResumeAfterInterruptionImportsTheRestWithoutDuplicates(t *testing.T) {
	env := newImportEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	env.fake.onRequest = func(path string) {
		if path == "/files/9003" {
			cancel()
		}
	}
	if _, err := env.run(t, ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run error = %v, want context.Canceled", err)
	}
	env.fake.onRequest = nil
	viewsOfDoneTicket := env.fake.count("/api/v2/tickets/1")

	report := env.mustRun(t)
	if report.Status != "OK" {
		t.Fatalf("resumed status=%s missing=%v failures=%+v", report.Status, report.Missing, report.Failures)
	}
	if got := env.tableCounts(t); got != fullImportRows {
		t.Fatalf("rows after resume = %s", got)
	}
	if env.fake.count("/api/v2/tickets/1") != viewsOfDoneTicket {
		t.Fatal("resume re-fetched a ticket finished before the interruption")
	}
}

func TestRateLimitHeadersAndRetryAfterAreRespected(t *testing.T) {
	env := newImportEnv(t)
	env.fake.throttleContacts = true
	env.fake.remainingZeroAgents = true
	report := env.mustRun(t)
	if report.Status != "OK" {
		t.Fatalf("status=%s", report.Status)
	}
	if !hasSleep(env.sleeps, 7*time.Second) {
		t.Fatalf("sleeps=%v, want Retry-After 7s", env.sleeps)
	}
	if !hasSleep(env.sleeps, 61*time.Second) {
		t.Fatalf("sleeps=%v, want pause after X-Ratelimit-Remaining: 0", env.sleeps)
	}
	if env.fake.count("/api/v2/contacts") != 3 {
		t.Fatalf("contacts requests=%d, want throttled + retried page 1 + page 2", env.fake.count("/api/v2/contacts"))
	}
}

func TestPartialFailureIsReportedAsFailedWithFreshdeskIDs(t *testing.T) {
	env := newImportEnv(t)
	env.fake.failAttachment = "9002"
	report := env.mustRun(t)
	if report.Status != "FAILED" {
		t.Fatalf("status=%s, want FAILED", report.Status)
	}
	if !hasFailure(report, "attachment", "9002", "1") || !hasFailure(report, "ticket", "1", "1") {
		t.Fatalf("failures=%+v", report.Failures)
	}
	if report.Attachments.Imported != 2 || len(report.Missing) == 0 {
		t.Fatalf("attachments=%+v missing=%v", report.Attachments, report.Missing)
	}
	markdown := report.Markdown()
	if !strings.Contains(markdown, "FAILED") || !strings.Contains(markdown, "9002") {
		t.Fatalf("markdown lacks failure detail:\n%s", markdown)
	}

	env.fake.failAttachment = ""
	retry := env.mustRun(t)
	if retry.Status != "OK" || env.count(t, "messages") != 11 || env.count(t, "attachments") != 3 {
		t.Fatalf("retry status=%s rows=%s", retry.Status, env.tableCounts(t))
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	env := newImportEnv(t)
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.DryRun = true })
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "DRY_RUN" || report.Contacts.Source != 3 || report.Tickets.Source != 6 || report.Tickets.ProbedIDs == 0 {
		t.Fatalf("dry run report = %+v", report)
	}
	if env.count(t, "conversations") != 0 || env.count(t, "import_items") != 0 || env.fake.count("/files/9003") != 0 {
		t.Fatalf("dry run wrote data: %s", env.tableCounts(t))
	}
}

func TestAPIKeyNeverAppearsInErrorsOrReports(t *testing.T) {
	env := newImportEnv(t)
	_, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.APIKey = testAPIKey + "-wrong" })
	if err == nil || strings.Contains(err.Error(), testAPIKey) {
		t.Fatalf("bad key error = %v", err)
	}
	report := env.mustRun(t)
	data, jsonErr := report.JSON()
	if jsonErr != nil || strings.Contains(string(data), testAPIKey) || strings.Contains(report.Markdown(), testAPIKey) {
		t.Fatal("report contains the API key")
	}
}

func assertCount(t *testing.T, name string, got freshdesk.EntityCount, source, imported int) {
	t.Helper()
	if got.Source != source || got.Imported != imported {
		t.Fatalf("%s = %+v, want source=%d imported=%d", name, got, source, imported)
	}
}

func hasWarning(report *freshdesk.Report, code string) bool {
	for _, warning := range report.Warnings {
		if warning.Code == code {
			return true
		}
	}
	return false
}

func hasFailure(report *freshdesk.Report, entity, id, ticket string) bool {
	for _, failure := range report.Failures {
		if failure.Entity == entity && failure.FreshdeskID == id && failure.TicketID == ticket {
			return true
		}
	}
	return false
}

func hasSleep(sleeps []time.Duration, want time.Duration) bool {
	for _, d := range sleeps {
		if d == want {
			return true
		}
	}
	return false
}

func TestTicketsOfADeactivatedAgentStayUnassignedAndAreReported(t *testing.T) {
	env := newImportEnv(t)
	if _, err := env.pool.Exec(context.Background(), `UPDATE agents SET deactivated_at=now() WHERE workspace_id=$1 AND email='alice@acme.example'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	if report.Status != "OK" {
		t.Fatalf("status=%s missing=%v failures=%+v", report.Status, report.Missing, report.Failures)
	}
	var assigned int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND assignee_id IS NOT NULL`, env.workspaceID).Scan(&assigned); err != nil {
		t.Fatal(err)
	}
	if assigned != 0 {
		t.Fatalf("%d conversations assigned; a deactivated agent must not receive any", assigned)
	}
	if got := env.count(t, "conversations"); got != 5 {
		t.Fatalf("conversations=%d, want all 5 imported", got)
	}
	var found *freshdesk.Warning
	for index := range report.Warnings {
		if report.Warnings[index].Code == "assignee_deactivated" {
			found = &report.Warnings[index]
		}
	}
	if found == nil || len(found.FreshdeskIDs) == 0 {
		t.Fatalf("no assignee_deactivated warning listing the tickets in %+v", report.Warnings)
	}
}
