package freshdesk_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

type storedReply struct{ ID, Title, Body string }

func (e *importEnv) savedReplies(t *testing.T) []storedReply {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT id::text,title,body FROM saved_replies WHERE workspace_id=$1 ORDER BY lower(title)`, e.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []storedReply
	for rows.Next() {
		var item storedReply
		if err := rows.Scan(&item.ID, &item.Title, &item.Body); err != nil {
			t.Fatal(err)
		}
		out = append(out, item)
	}
	return out
}

func titlesOf(replies []storedReply) string {
	var titles []string
	for _, reply := range replies {
		titles = append(titles, reply.Title)
	}
	return strings.Join(titles, " | ")
}

func (e *importEnv) withCanned() *importEnv {
	e.fake.canned = true
	return e
}

const importedTitles = "Billing / Chào khách | Billing / Refund | General / Greeting | General / Order status | My drafts / Private tip"

func TestCannedResponsesBecomeSavedRepliesWithTheFolderNameAsPrefix(t *testing.T) {
	env := newImportEnv(t).withCanned()
	report := env.mustRun(t)

	if report.Status != "OK" {
		t.Fatalf("status=%s missing=%v failures=%+v warnings=%+v", report.Status, report.Missing, report.Failures, report.Warnings)
	}
	// Six responses in three folders; one has no text at all.
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 5, 5)
	if report.SavedReplies.Folders != 3 || strings.Join(report.SavedReplies.EmptySkipped, ",") != "9103" {
		t.Fatalf("saved replies report = %+v", report.SavedReplies)
	}
	got := env.savedReplies(t)
	if titlesOf(got) != importedTitles {
		t.Fatalf("titles = %s", titlesOf(got))
	}
	want := map[string]string{
		// CRLF becomes LF, a line holding only a space is blank, and the two placeholders Colliedesk has are mapped.
		"General / Greeting": "Hi {{customer.name}},\n\nThanks for writing.\n\nRegards,\n{{agent.name}}",
		// Placeholders without an equivalent stay as written.
		"General / Order status": "Hello {{ticket.requester.first_name}}, track it at {{ticket.url}}. {{ticket.url}} again.",
		// No plain text from Freshdesk: the HTML is reduced to text.
		"Billing / Refund":        "We refunded you.\nAllow 5 days & check your bank.",
		"Billing / Chào khách":    "Chào {{customer.name}} ạ, cảm ơn bạn.",
		"My drafts / Private tip": "Check the log first.",
	}
	for _, reply := range got {
		if reply.Body != want[reply.Title] {
			t.Errorf("%s body = %q, want %q", reply.Title, reply.Body, want[reply.Title])
		}
	}
}

func TestCannedResponseLossesAreReportedNeverSilent(t *testing.T) {
	env := newImportEnv(t).withCanned()
	report := env.mustRun(t)

	if got := fmt.Sprint(report.SavedReplies.UnmappedPlaceholders); got != "map[ticket.requester.first_name:[9102] ticket.url:[9102]]" {
		t.Fatalf("unmapped placeholders = %s", got)
	}
	if report.SavedReplies.AttachmentsNotImported != 1 || strings.Join(report.SavedReplies.ResponsesWithAttachments, ",") != "9102" {
		t.Fatalf("attachments = %+v", report.SavedReplies)
	}
	for _, code := range []string{"saved_reply_placeholder_not_mapped", "saved_reply_attachments_not_imported", "saved_reply_empty", "saved_reply_personal_folder"} {
		if !hasWarning(report, code) {
			t.Errorf("missing warning %q in %+v", code, report.Warnings)
		}
	}
	markdown := report.Markdown()
	for _, text := range []string{"Saved replies | 5 | 5 | 0", "{{ticket.url}}", "9103"} {
		if !strings.Contains(markdown, text) {
			t.Errorf("markdown lacks %q:\n%s", text, markdown)
		}
	}
	if !report.Verification.SavedReplies.Verified {
		t.Fatalf("verification = %+v", report.Verification.SavedReplies)
	}
	for _, line := range report.NotImported {
		if strings.Contains(line, "SLA policies, canned responses") {
			t.Errorf("not-imported list still claims canned responses are skipped: %q", line)
		}
	}
}

func TestSavedRepliesRerunIsANoOpAndKeepsEdits(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.mustRun(t)
	first := env.savedReplies(t)
	if _, err := env.pool.Exec(context.Background(), `UPDATE saved_replies SET body='Edited by an agent', updated_at=now()+interval '1 minute' WHERE workspace_id=$1 AND title='General / Greeting'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(context.Background(), `DELETE FROM saved_replies WHERE workspace_id=$1 AND title='My drafts / Private tip'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 5, 5)
	after := env.savedReplies(t)
	if len(after) != 4 || len(first) != 5 {
		t.Fatalf("rows before %d, after rerun %d, want 5 then 4: an agent's deletion is not undone", len(first), len(after))
	}
	for _, reply := range after {
		if reply.Title == "General / Greeting" && reply.Body != "Edited by an agent" {
			t.Fatalf("a rerun overwrote an agent's edit: %q", reply.Body)
		}
	}
}

func TestSavedReplyWithATakenTitleIsImportedUnderAClearlyMarkedTitle(t *testing.T) {
	env := newImportEnv(t).withCanned()
	if _, err := env.pool.Exec(context.Background(), `INSERT INTO saved_replies(workspace_id,title,body) VALUES($1,'general / greeting','Mine')`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	if report.Status != "OK" {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 5, 5)
	titles := titlesOf(env.savedReplies(t))
	if !strings.Contains(titles, "general / greeting | General / Greeting (Freshdesk 9101)") {
		t.Fatalf("titles = %s", titles)
	}
	for _, reply := range env.savedReplies(t) {
		if reply.Title == "general / greeting" && reply.Body != "Mine" {
			t.Fatalf("the existing reply was changed: %+v", reply)
		}
	}
}

func TestSavedRepliesBeyondTheWorkspaceLimitAreReportedAsFailures(t *testing.T) {
	env := newImportEnv(t).withCanned()
	if _, err := env.pool.Exec(context.Background(), `INSERT INTO saved_replies(workspace_id,title,body)
		SELECT $1, 'Existing ' || n, 'x' FROM generate_series(1,498) n`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	if report.Status != "FAILED" || report.SavedReplies.Imported != 2 || report.SavedReplies.Failed != 3 {
		t.Fatalf("status=%s saved replies=%+v", report.Status, report.SavedReplies)
	}
	if len(report.Failures) != 3 || !strings.Contains(report.Failures[0].Error, "at most 500") || report.Failures[0].Entity != "saved_reply" {
		t.Fatalf("failures = %+v", report.Failures)
	}
	if !strings.Contains(strings.Join(report.Missing, ";"), "saved replies: 3 of 5 not imported") {
		t.Fatalf("missing = %v", report.Missing)
	}
}

func TestSavedReplyTooLongIsAFailureWithItsReason(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.cannedExtra = []map[string]any{{"id": 9199, "title": "Huge", "folder_id": 801, "content": strings.Repeat("a", 20001), "content_html": "", "attachments": []any{}}}
	report := env.mustRun(t)
	if report.Status != "FAILED" || report.SavedReplies.Failed != 1 {
		t.Fatalf("status=%s saved replies=%+v", report.Status, report.SavedReplies)
	}
	if len(report.Failures) != 1 || report.Failures[0].FreshdeskID != "9199" || !strings.Contains(report.Failures[0].Error, "at most 20000 characters") {
		t.Fatalf("failures = %+v", report.Failures)
	}
	if got := len(env.savedReplies(t)); got != 5 {
		t.Fatalf("saved replies = %d, want the other 5", got)
	}
}

func TestDryRunCountsSavedRepliesAndWritesNothing(t *testing.T) {
	env := newImportEnv(t).withCanned()
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.DryRun = true })
	if err != nil {
		t.Fatal(err)
	}
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 5, 0)
	if report.SavedReplies.Folders != 3 || len(env.savedReplies(t)) != 0 {
		t.Fatalf("saved replies report = %+v, rows = %d", report.SavedReplies, len(env.savedReplies(t)))
	}
}

func TestSavedReplyTotalFromFreshdeskMustMatchWhatWasListed(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.cannedCountBoost = 2
	report := env.mustRun(t)
	if report.Status != "UNVERIFIED" || report.Verification.SavedReplies.Verified {
		t.Fatalf("status=%s verification=%+v", report.Status, report.Verification.SavedReplies)
	}
	if !strings.Contains(report.Verification.SavedReplies.Detail, "8") || !strings.Contains(report.Verification.SavedReplies.Detail, "6") {
		t.Fatalf("detail = %q", report.Verification.SavedReplies.Detail)
	}
}

func TestRefusedCannedResponseEndpointIsReportedAndTheRestStillImports(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.failPathStatus = map[string]int{"/api/v2/canned_response_folders": 403}
	report := env.mustRun(t)
	if report.Status != "UNVERIFIED" || !hasWarning(report, "saved_replies_unavailable") || report.Verification.SavedReplies.Verified {
		t.Fatalf("status=%s warnings=%+v verification=%+v", report.Status, report.Warnings, report.Verification.SavedReplies)
	}
	assertCount(t, "tickets", report.Tickets.EntityCount, 5, 5)
}

func TestUnreadableCannedFolderIsAFailureNotASkip(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.failPathStatus = map[string]int{"/api/v2/canned_response_folders/802/responses": 404}
	report := env.mustRun(t)
	if report.Status != "FAILED" {
		t.Fatalf("status=%s", report.Status)
	}
	found := false
	for _, failure := range report.Failures {
		found = found || (failure.Entity == "saved_reply_folder" && failure.FreshdeskID == "802")
	}
	if !found {
		t.Fatalf("failures = %+v", report.Failures)
	}
	// The rerun after the folder is readable again completes the import.
	env.fake.failPathStatus = nil
	again := env.mustRun(t)
	if again.Status != "OK" || len(env.savedReplies(t)) != 5 {
		t.Fatalf("rerun status=%s failures=%+v rows=%d", again.Status, again.Failures, len(env.savedReplies(t)))
	}
}

func TestUndoRemovesUntouchedSavedRepliesAndKeepsEditedOnes(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.mustRun(t)
	if _, err := env.pool.Exec(context.Background(), `UPDATE saved_replies SET body='Rewritten', updated_at=now()+interval '1 minute' WHERE workspace_id=$1 AND title='General / Greeting'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(context.Background(), `INSERT INTO saved_replies(workspace_id,title,body) VALUES($1,'Written by hand','Mine')`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SavedReplies != 4 || strings.Join(result.KeptSavedReplies, ",") != "9101" {
		t.Fatalf("undo = %+v", result)
	}
	if got := titlesOf(env.savedReplies(t)); got != "General / Greeting | Written by hand" {
		t.Fatalf("saved replies after undo = %s", got)
	}
	// Importing again adds back the removed ones and does not duplicate the kept one.
	env.mustRun(t)
	if got := titlesOf(env.savedReplies(t)); got != importedTitles+" | Written by hand" {
		t.Fatalf("saved replies after reimport = %s", got)
	}
}
