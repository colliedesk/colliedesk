package freshdesk_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

// Expected values are worked out by hand from the fixture bytes ("PNG-INLINE-1", "PNG-ACCOUNT"):
// printf 'PNG-INLINE-1' | sha256sum, | base64.
const (
	inlineDataURI   = "data:image/png;base64,UE5HLUlOTElORS0x"
	accountDataURI  = "data:image/png;base64,UE5HLUFDQ09VTlQ="
	inlineSHA256    = "ad7a68345468d1677b837f29be6ac55b1d322702e1be97371769667ac4194a80"
	inlineImageSize = 12 + 11
)

func (e *importEnv) messageHTML(t *testing.T, entity, freshdeskID string) string {
	t.Helper()
	var html string
	err := e.pool.QueryRow(context.Background(), `SELECT body_html FROM messages WHERE workspace_id=$1 AND id=$2`,
		e.workspaceID, e.target(t, entity, freshdeskID)).Scan(&html)
	if err != nil {
		t.Fatal(err)
	}
	return html
}

func (e *importEnv) inlineLedger(t *testing.T, status string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM import_items WHERE workspace_id=$1 AND entity='inline_image' AND status=$2`,
		e.workspaceID, status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestInlineImagesAreRehostedAndShownInTheStoredOriginal(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	report := env.mustRun(t)

	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s missing=%v failures=%+v", report.Status, report.Missing, report.Failures)
	}
	assertCount(t, "inline images", report.InlineImages.EntityCount, 2, 2)
	if report.InlineImages.ImportedBytes != inlineImageSize || report.InlineImages.ChecksumVerified != 2 || len(report.InlineImages.ChecksumMismatches) != 0 {
		t.Fatalf("inline images = %+v", report.InlineImages)
	}
	// The file host image (a reply) and the account host image (a ticket description) both become data: URIs.
	reply := env.messageHTML(t, "conversation", "7002")
	if !strings.Contains(reply, `<img src="`+inlineDataURI+`">`) || strings.Contains(reply, "127.0.0.1") {
		t.Fatalf("reply HTML = %q", reply)
	}
	description := env.messageHTML(t, "description", "6")
	if !strings.Contains(description, `<img src="`+accountDataURI+`">`) {
		t.Fatalf("description HTML = %q", description)
	}
	var sha, contentType string
	err := env.pool.QueryRow(context.Background(), `SELECT a.sha256,a.content_type FROM attachments a JOIN import_items i ON i.target_id=a.id
		WHERE i.workspace_id=$1 AND i.entity='inline_image' AND i.parent_source_id='1'`, env.workspaceID).Scan(&sha, &contentType)
	if err != nil || sha != inlineSHA256 || contentType != "image/png" {
		t.Fatalf("stored inline image sha=%s type=%s err=%v", sha, contentType, err)
	}
	if hasWarning(report, "inline_images_not_rehosted") {
		t.Fatalf("a rehosted image still warns: %+v", report.Warnings)
	}
	if env.fake.fileAuthSeen {
		t.Fatal("the Freshdesk API key was sent to the file host")
	}
}

func TestImagesNotHostedByFreshdeskAreLeftUntouched(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	html := env.messageHTML(t, "conversation", "7001")
	for _, src := range []string{"https://images.example.org/pixel.png", "https://freshdesk.com.evil.example/a.png", "https://evilfreshdesk.com/b.png"} {
		if !strings.Contains(html, `src="`+src+`"`) {
			t.Errorf("image %s was changed: %q", src, html)
		}
	}
	if report.InlineImages.Source != 2 {
		t.Fatalf("inline images counted %d, want only the 2 Freshdesk-hosted ones", report.InlineImages.Source)
	}
}

func TestInlineImageRerunDoesNotDuplicate(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	env.mustRun(t)
	before, downloads := env.tableCounts(t), env.fake.count("/files/inline-5001")
	if !strings.Contains(before, "attachments=5") {
		t.Fatalf("rows = %s, want 3 attachments + 2 inline images", before)
	}
	// A ticket that changed at the source is read again; its image must not be stored twice.
	env.fake.extraReplyOnTicket6 = true
	again := env.mustRun(t)
	if again.Status != freshdesk.StatusOK || again.InlineImages.Imported != 2 {
		t.Fatalf("rerun status=%s inline=%+v", again.Status, again.InlineImages)
	}
	if got := env.tableCounts(t); got != strings.Replace(before, "messages=11", "messages=12", 1) {
		t.Fatalf("rows %s -> %s", before, got)
	}
	if env.fake.count("/files/inline-5001") != downloads {
		t.Fatal("an image that was already stored was downloaded again")
	}
}

func TestInterruptedImportResumesInlineImages(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	ctx, cancel := context.WithCancel(context.Background())
	env.fake.onRequest = func(path string) {
		if path == "/files/inline-5001" {
			cancel()
		}
	}
	if _, err := env.run(t, ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run error = %v, want context.Canceled", err)
	}
	env.fake.onRequest = nil
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("resumed status=%s missing=%v failures=%+v", report.Status, report.Missing, report.Failures)
	}
	assertCount(t, "inline images", report.InlineImages.EntityCount, 2, 2)
	if env.count(t, "attachments") != 5 || !strings.Contains(env.messageHTML(t, "conversation", "7002"), inlineDataURI) {
		t.Fatalf("rows after resume = %s", env.tableCounts(t))
	}
}

func TestUndoRemovesInlineImagesAndTheirFiles(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	env.mustRun(t)
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 5 || storedFiles(t, env.storageRoot) != 0 || env.count(t, "attachments") != 0 || env.count(t, "import_items") != 0 {
		t.Fatalf("undo = %+v, files left %d, rows %s", result, storedFiles(t, env.storageRoot), env.rowCounts(t))
	}
}

func TestInlineImageThatCannotBeDownloadedFailsTheImportAndKeepsTheLink(t *testing.T) {
	cases := map[string]func(*fakeFreshdesk){
		"not found":         func(f *fakeFreshdesk) { f.fileStatus["inline-5001"] = http.StatusNotFound },
		"expired signature": func(f *fakeFreshdesk) { f.fileStatus["inline-5001"] = http.StatusForbidden },
		"not an image":      func(f *fakeFreshdesk) { f.fileType["inline-5001"] = "text/html" },
		"too large":         func(f *fakeFreshdesk) { f.fileBody["inline-5001"] = strings.Repeat("x", 5<<20+1) },
		"server error":      func(f *fakeFreshdesk) { f.failAttachment = "inline-5001" },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			env := newImportEnv(t)
			env.fake.inlineImages = true
			break_(env.fake)
			report := env.mustRun(t)

			if report.Status != freshdesk.StatusFailed {
				t.Fatalf("status=%s, want FAILED: an image that was not brought over is data loss", report.Status)
			}
			if !hasFailure(report, "inline_image", failureID(report, "inline_image"), "1") || !hasFailure(report, "ticket", "1", "1") {
				t.Fatalf("failures = %+v", report.Failures)
			}
			assertCount(t, "inline images", report.InlineImages.EntityCount, 2, 1)
			if report.InlineImages.Failed != 1 || len(report.Missing) == 0 {
				t.Fatalf("inline images = %+v missing=%v", report.InlineImages, report.Missing)
			}
			markdown := report.Markdown()
			for _, want := range []string{"inline_image", "original link is kept", "FAILED"} {
				if !strings.Contains(markdown, want) {
					t.Errorf("markdown lacks %q:\n%s", want, markdown)
				}
			}
			if strings.Contains(markdown, "Signature=") {
				t.Error("the report leaks the signed query string")
			}
			if html := env.messageHTML(t, "conversation", "7002"); !strings.Contains(html, "/inline-5001?Signature=abc") || strings.Contains(html, "data:") {
				t.Fatalf("the original link was not kept: %q", html)
			}
		})
	}
}

func TestInlineImageFailureIsRetriedOnTheNextRun(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	env.fake.failAttachment = "inline-5001"
	if report := env.mustRun(t); report.Status != freshdesk.StatusFailed {
		t.Fatalf("status=%s", report.Status)
	}
	env.fake.failAttachment = ""
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || env.inlineLedger(t, "failed") != 0 || env.count(t, "attachments") != 5 {
		t.Fatalf("retry status=%s failures=%+v rows=%s", report.Status, report.Failures, env.tableCounts(t))
	}
	if !strings.Contains(env.messageHTML(t, "conversation", "7002"), inlineDataURI) {
		t.Fatal("the retried image is not in the stored original")
	}
}

func TestInlineImagesCountInThePaidPlanSizingBeforeAnythingIsWritten(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	env.setPlan(t, env.workspaceID, "founding")
	before := env.rowCounts(t)
	// 30 bytes of attachments + 23 bytes of inline images = 53.
	report, err := env.runCapped(t, 52)
	var capErr *freshdesk.StorageCapError
	if !errors.As(err, &capErr) || report != nil || capErr.Needed != 53 {
		t.Fatalf("report=%v err=%v, want a refusal that needs 53 bytes", report, err)
	}
	if after := env.rowCounts(t); after != before || env.fake.count("/files/9003") != 0 {
		t.Fatalf("rows changed from %s to %s", before, after)
	}
	report, err = env.runCapped(t, 53)
	if err != nil || report.Status != freshdesk.StatusOK {
		t.Fatalf("report=%+v err=%v, want OK at exactly 53", report, err)
	}
	if got := env.storedBytes(t, env.workspaceID); got != 53 {
		t.Fatalf("stored bytes = %d, want 53", got)
	}
}

func TestInlineImageMarkdownReportsCountsBytesAndChecksums(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	markdown := env.mustRun(t).Markdown()
	for _, want := range []string{"| Inline images | 2 | 2 | 0 |", "Inline image bytes: 23 stored. SHA-256 re-verified from storage: 2."} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown lacks %q:\n%s", want, markdown)
		}
	}
}

func TestDamagedInlineImageFileIsDownloadedAgain(t *testing.T) {
	env := newImportEnv(t)
	env.fake.inlineImages = true
	env.mustRun(t)
	if _, err := env.pool.Exec(context.Background(), `UPDATE attachments SET sha256=repeat('0',64) WHERE id=$1`, env.target(t, "inline_image", env.inlineLedgerID(t, "1"))); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || len(report.InlineImages.ChecksumMismatches) != 1 {
		t.Fatalf("status=%s inline=%+v, want the mismatch reported", report.Status, report.InlineImages)
	}
	repaired := env.mustRun(t)
	if repaired.Status != freshdesk.StatusOK || repaired.InlineImages.ChecksumVerified != 2 || env.count(t, "attachments") != 5 {
		t.Fatalf("status=%s inline=%+v rows=%s", repaired.Status, repaired.InlineImages, env.tableCounts(t))
	}
}

func (e *importEnv) inlineLedgerID(t *testing.T, ticket string) string {
	t.Helper()
	var id string
	if err := e.pool.QueryRow(context.Background(), `SELECT source_id FROM import_items WHERE workspace_id=$1 AND entity='inline_image' AND parent_source_id=$2`,
		e.workspaceID, ticket).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func failureID(report *freshdesk.Report, entity string) string {
	for _, failure := range report.Failures {
		if failure.Entity == entity {
			return failure.FreshdeskID
		}
	}
	return ""
}
