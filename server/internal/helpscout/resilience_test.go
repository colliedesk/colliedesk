package helpscout_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/helpscout"
)

func TestRerunImportsNothingTwice(t *testing.T) {
	env := newImportEnv(t)
	first := env.mustRun(t)
	before := env.tableCounts(t)
	downloads := env.fake.count("/v2/conversations/101/attachments/3001/data")
	second := env.mustRun(t)
	if first.Status != helpscout.StatusOK || second.Status != helpscout.StatusOK {
		t.Fatalf("statuses %s, %s", first.Status, second.Status)
	}
	if after := env.tableCounts(t); after != before {
		t.Fatalf("rerun changed the workspace:\n before %s\n after  %s", before, after)
	}
	if second.Messages.Imported != 7 || second.Attachments.Imported != 2 {
		t.Fatalf("rerun report messages=%d attachments=%d", second.Messages.Imported, second.Attachments.Imported)
	}
	if env.fake.count("/v2/conversations/101/attachments/3001/data") != downloads {
		t.Fatal("rerun downloaded an attachment that was already imported")
	}
}

func TestExpiredAccessTokenIsRequestedAgain(t *testing.T) {
	env := newImportEnv(t)
	env.fake.expireTokensAfter = 3
	report := env.mustRun(t)
	if report.Status != helpscout.StatusOK {
		t.Fatalf("status = %s, failures %+v", report.Status, report.Failures)
	}
	if env.fake.tokensIssued != 2 || report.RateLimit.TokenRefreshes != 1 {
		t.Fatalf("tokens issued = %d, refreshes reported = %d; want 2 and 1", env.fake.tokensIssued, report.RateLimit.TokenRefreshes)
	}
}

func TestWrongAppSecretStopsBeforeWritingAndNeverPrintsIt(t *testing.T) {
	env := newImportEnv(t)
	_, err := env.run(t, func(o *helpscout.Options) { o.AppSecret = "wrong-secret-NEVER-PRINT" })
	if err == nil || !strings.Contains(err.Error(), "App ID or App Secret") || strings.Contains(err.Error(), "NEVER-PRINT") {
		t.Fatalf("error = %v", err)
	}
	if got := env.tableCounts(t); got != "contacts=0 conversations=0 messages=0 attachments=0 tags=0 conversation_tags=0" {
		t.Fatalf("wrote with bad credentials: %s", got)
	}
}

func TestTooManyRequestsWaitsForRetryAfter(t *testing.T) {
	env := newImportEnv(t)
	env.fake.throttle["/v2/conversations"] = 2
	report := env.mustRun(t)
	if report.Status != helpscout.StatusOK || report.RateLimit.Throttled != 2 || report.RateLimit.WaitSeconds != 14 {
		t.Fatalf("status=%s rate limit=%+v", report.Status, report.RateLimit)
	}
	if !reflect.DeepEqual(env.sleeps, []time.Duration{7 * time.Second, 7 * time.Second}) {
		t.Fatalf("sleeps = %v, want two waits of 7s", env.sleeps)
	}
}

func TestSeveralInboxesNeedAnExplicitChoice(t *testing.T) {
	env := newImportEnv(t)
	env.fake.twoMailboxes = true
	_, err := env.run(t, nil)
	if err == nil || !strings.Contains(err.Error(), "501 (Support)") || !strings.Contains(err.Error(), "502 (Billing)") {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.run(t, func(o *helpscout.Options) { o.MailboxID = 999 }); err == nil || !strings.Contains(err.Error(), "999") {
		t.Fatalf("unknown inbox error = %v", err)
	}
	report, err := env.run(t, func(o *helpscout.Options) { o.MailboxID = 501 })
	if err != nil || report.Status != helpscout.StatusOK {
		t.Fatalf("chosen inbox: %v %v", report, err)
	}
}

func TestAttachmentWithWrongSizeFailsUntilHelpScoutServesIt(t *testing.T) {
	env := newImportEnv(t)
	env.fake.dataBody["3001"] = "PDF-BYTES-1-TRUNCATED?"
	report := env.mustRun(t)
	if report.Status != helpscout.StatusFailed || report.Attachments.Imported != 1 || report.Attachments.Failed != 1 {
		t.Fatalf("status=%s attachments=%+v", report.Status, report.Attachments.EntityCount)
	}
	// The attachment fails, and so does its conversation until every item of it is in.
	if len(report.Failures) != 2 || report.Failures[0].Entity != "attachment" || report.Failures[0].HelpScoutID != "3001" ||
		report.Failures[0].ConversationID != "101" || !strings.Contains(report.Failures[0].Error, "size mismatch") ||
		report.Failures[1].Entity != "conversation" || report.Failures[1].HelpScoutID != "101" {
		t.Fatalf("failures = %+v", report.Failures)
	}
	delete(env.fake.dataBody, "3001")
	if again := env.mustRun(t); again.Status != helpscout.StatusOK || again.Attachments.Imported != 2 {
		t.Fatalf("rerun status=%s attachments=%+v failures=%+v", again.Status, again.Attachments.EntityCount, again.Failures)
	}
	if got := env.count(t, "attachments"); got != 3 {
		t.Fatalf("attachments = %d, want 3 (no duplicate)", got)
	}
}

func TestDamagedStoredFileIsReportedAndDownloadedAgain(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	var key string
	if err := env.pool.QueryRow(t.Context(), `SELECT storage_key FROM attachments WHERE workspace_id=$1 AND id=$2`,
		env.workspaceID, env.target(t, "attachment", "3001")).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.storageRoot, filepath.FromSlash(key)), []byte("PDF-BYTES-X"), 0o600); err != nil {
		t.Fatal(err)
	}
	damaged := env.mustRun(t)
	if damaged.Status != helpscout.StatusFailed || !reflect.DeepEqual(damaged.Attachments.ChecksumMismatches, []string{"3001"}) {
		t.Fatalf("status=%s mismatches=%v", damaged.Status, damaged.Attachments.ChecksumMismatches)
	}
	repaired := env.mustRun(t)
	if repaired.Status != helpscout.StatusOK || len(repaired.Attachments.ChecksumMismatches) != 0 || env.count(t, "attachments") != 3 {
		t.Fatalf("status=%s mismatches=%v attachments=%d", repaired.Status, repaired.Attachments.ChecksumMismatches, env.count(t, "attachments"))
	}
}

func TestFewerThreadsThanTheConversationListsIsUnverified(t *testing.T) {
	env := newImportEnv(t)
	env.fake.shortThreads = true
	report := env.mustRun(t)
	if report.Status != helpscout.StatusUnverified || report.Verification.Conversations.Verified ||
		!strings.Contains(report.Verification.Conversations.Detail, "103") {
		t.Fatalf("status=%s verification=%+v", report.Status, report.Verification.Conversations)
	}
}
