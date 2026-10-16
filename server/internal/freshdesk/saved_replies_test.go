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

const importedTitles = "Billing / Chào khách | Billing / Refund | General / Greeting | General / Order status"

func TestCannedResponsesBecomeSavedRepliesWithTheFolderNameAsPrefix(t *testing.T) {
	env := newImportEnv(t).withCanned()
	report := env.mustRun(t)

	if report.Status != "OK" {
		t.Fatalf("status=%s missing=%v failures=%+v warnings=%+v", report.Status, report.Missing, report.Failures, report.Warnings)
	}
	// Six responses in three folders: one has no text at all and one sits in a personal folder.
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 4, 4)
	if strings.Join(report.SavedReplies.PersonalNotImported, ",") != "9106" {
		t.Fatalf("personal not imported = %v", report.SavedReplies.PersonalNotImported)
	}
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
		"Billing / Refund":     "We refunded you.\nAllow 5 days & check your bank.",
		"Billing / Chào khách": "Chào {{customer.name}} ạ, cảm ơn bạn.",
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
	for _, text := range []string{"Canned responses in personal folders, not imported", "9106"} {
		if !strings.Contains(report.Markdown(), text) {
			t.Errorf("markdown lacks %q", text)
		}
	}
	for _, code := range []string{"saved_reply_placeholder_not_mapped", "saved_reply_attachments_not_imported", "saved_reply_empty", "saved_reply_personal_folder"} {
		if !hasWarning(report, code) {
			t.Errorf("missing warning %q in %+v", code, report.Warnings)
		}
	}
	markdown := report.Markdown()
	for _, text := range []string{"Saved replies | 4 | 4 | 0", "{{ticket.url}}", "9103"} {
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
	if _, err := env.pool.Exec(context.Background(), `DELETE FROM saved_replies WHERE workspace_id=$1 AND title='Billing / Refund'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 4, 4)
	after := env.savedReplies(t)
	if len(after) != 3 || len(first) != 4 {
		t.Fatalf("rows before %d, after rerun %d, want 4 then 3: an agent's deletion is not undone", len(first), len(after))
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
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 4, 4)
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
	if report.Status != "FAILED" || report.SavedReplies.Imported != 2 || report.SavedReplies.Failed != 2 {
		t.Fatalf("status=%s saved replies=%+v", report.Status, report.SavedReplies)
	}
	if len(report.Failures) != 2 || !strings.Contains(report.Failures[0].Error, "at most 500") || report.Failures[0].Entity != "saved_reply" {
		t.Fatalf("failures = %+v", report.Failures)
	}
	if !strings.Contains(strings.Join(report.Missing, ";"), "saved replies: 2 of 4 not imported") {
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
	if got := len(env.savedReplies(t)); got != 4 {
		t.Fatalf("saved replies = %d, want the other 4", got)
	}
}

func TestDryRunCountsSavedRepliesAndWritesNothing(t *testing.T) {
	env := newImportEnv(t).withCanned()
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.DryRun = true })
	if err != nil {
		t.Fatal(err)
	}
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 4, 0)
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

func TestAccountWithoutCannedResponsesImportsNormallyAndSaysSo(t *testing.T) {
	for _, status := range []int{403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) { accountWithoutCanned(t, status) })
	}
}

func accountWithoutCanned(t *testing.T, status int) {
	{
		env := newImportEnv(t).withCanned()
		env.fake.failPathStatus = map[string]int{"/api/v2/canned_response_folders": status}
		report := env.mustRun(t)
		if report.Status != "OK" || !report.Verification.SavedReplies.Verified {
			t.Fatalf("HTTP %d: status=%s verification=%+v failures=%+v", status, report.Status, report.Verification.SavedReplies, report.Failures)
		}
		if !report.SavedReplies.NotAvailable || !strings.Contains(report.Markdown(), "Canned responses are not available on this Freshdesk account") {
			t.Fatalf("HTTP %d: the report does not say canned responses are unavailable: %+v", status, report.SavedReplies)
		}
		assertCount(t, "tickets", report.Tickets.EntityCount, 5, 5)
		assertCount(t, "saved replies", report.SavedReplies.EntityCount, 0, 0)
	}
}

func TestServerErrorsListingCannedFoldersAreNotTreatedAsUnavailable(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.failPathStatus = map[string]int{"/api/v2/canned_response_folders": 500}
	report := env.mustRun(t)
	if report.SavedReplies.NotAvailable || report.Verification.SavedReplies.Verified || report.Status == "OK" {
		t.Fatalf("status=%s saved replies=%+v verification=%+v", report.Status, report.SavedReplies, report.Verification.SavedReplies)
	}
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
	if again.Status != "OK" || len(env.savedReplies(t)) != 4 {
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
	if result.SavedReplies != 3 || strings.Join(result.KeptSavedReplies, ",") != "9101" {
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

func TestPersonalFoldersAreImportedOnlyWhenAsked(t *testing.T) {
	env := newImportEnv(t).withCanned()
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.IncludePersonalCannedResponses = true })
	if err != nil {
		t.Fatal(err)
	}
	assertCount(t, "saved replies", report.SavedReplies.EntityCount, 5, 5)
	if len(report.SavedReplies.PersonalNotImported) != 0 || report.Status != "OK" {
		t.Fatalf("status=%s personal not imported=%v", report.Status, report.SavedReplies.PersonalNotImported)
	}
	if got := titlesOf(env.savedReplies(t)); got != importedTitles+" | My drafts / Private tip" && !strings.Contains(got, "My drafts / Private tip") {
		t.Fatalf("titles = %s", got)
	}
	if !hasWarning(report, "saved_reply_personal_folder") {
		t.Fatal("importing a personal folder must warn that it is now shared")
	}
}

func TestCannedHTMLKeepsLinksAndReportsDroppedImages(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.cannedCountBoost = 0
	env.fake.cannedExtra = []map[string]any{{"id": 9150, "title": "Links", "folder_id": 801, "content": "",
		"content_html": `<div>Read <a href="https://help.example.com/refunds">our refund policy</a>, or <a href="https://example.com">https://example.com</a>.<br><img src="https://cdn.example.com/logo.png"> Thanks</div>`, "attachments": []any{}}}
	report := env.mustRun(t)
	var body string
	for _, reply := range env.savedReplies(t) {
		if reply.Title == "General / Links" {
			body = reply.Body
		}
	}
	if want := "Read our refund policy (https://help.example.com/refunds), or https://example.com.\n Thanks"; body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	found := false
	for _, warning := range report.Warnings {
		found = found || (warning.Code == "saved_reply_images_dropped" && strings.Join(warning.FreshdeskIDs, ",") == "9150")
	}
	if !found {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}

func TestOnlyWebAndMailLinksKeepTheirAddress(t *testing.T) {
	env := newImportEnv(t).withCanned()
	links := []string{
		`<a href="javascript:alert(1)">one</a>`, `<a href="JavaScript:alert(1)">two</a>`, `<a href="data:text/html;base64,AAAA">three</a>`,
		`<a href="vbscript:msgbox">four</a>`, `<a href=" javascript:alert(1)">five</a>`, `<a href="/relative/path">six</a>`,
		`<a href="HTTPS://Example.com/x">seven</a>`, `<a href="mailto:help@example.com">mail us</a>`, `<a href="java&#10;script:alert(1)">nine</a>`,
		"<a href=\"java\tscript:alert(1)\">ten</a>", `<a href="//evil.example/x">eleven</a>`, `<a href="http://example.com">http://example.com</a>`,
		`<a href="javascript:alert(2)">javascript:alert(2)</a>`,
	}
	env.fake.cannedExtra = []map[string]any{{"id": 9160, "title": "Schemes", "folder_id": 801, "content": "",
		"content_html": "<div>" + strings.Join(links, "<br>") + "</div>", "attachments": []any{}}}
	env.mustRun(t)
	var body string
	for _, reply := range env.savedReplies(t) {
		if reply.Title == "General / Schemes" {
			body = reply.Body
		}
	}
	want := strings.Join([]string{"one", "two", "three", "four", "five", "six", "seven (HTTPS://Example.com/x)", "mail us (mailto:help@example.com)",
		"nine", "ten", "eleven", "http://example.com", "javascript:alert(2)"}, "\n")
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestFailureListingCannedFoldersDoesNotStopTheTicketImport(t *testing.T) {
	env := newImportEnv(t).withCanned()
	env.fake.failPathStatus = map[string]int{"/api/v2/canned_response_folders": 500}
	report, err := env.run(t, context.Background(), nil)
	if err != nil {
		t.Fatalf("a failed canned response listing stopped the run: %v", err)
	}
	assertCount(t, "tickets", report.Tickets.EntityCount, 5, 5)
	if report.Verification.SavedReplies.Verified || report.Status != "FAILED" {
		t.Fatalf("status=%s verification=%+v", report.Status, report.Verification.SavedReplies)
	}
	found := false
	for _, failure := range report.Failures {
		found = found || (failure.Entity == "saved_reply_folder" && failure.FreshdeskID == "all")
	}
	if !found || len(env.savedReplies(t)) != 0 {
		t.Fatalf("failures = %+v", report.Failures)
	}
	// The next run, with the endpoint back, completes the saved replies.
	env.fake.failPathStatus = nil
	again := env.mustRun(t)
	if again.Status != "OK" || len(env.savedReplies(t)) != 4 {
		t.Fatalf("rerun status=%s failures=%+v rows=%d", again.Status, again.Failures, len(env.savedReplies(t)))
	}
}
