package freshdesk_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

func storedPath(t *testing.T, env *importEnv, attachmentID string) string {
	t.Helper()
	var key string
	if err := env.pool.QueryRow(context.Background(), `SELECT storage_key FROM attachments WHERE workspace_id=$1 AND id=$2`,
		env.workspaceID, env.target(t, "attachment", attachmentID)).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(env.storageRoot, filepath.FromSlash(key))
}

func TestCorruptedOrMissingStoredFileIsDownloadedAgain(t *testing.T) {
	env := newImportEnv(t)
	env.mustRun(t)
	if err := os.WriteFile(storedPath(t, env, "9001"), []byte("bit rot"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(storedPath(t, env, "9003")); err != nil {
		t.Fatal(err)
	}
	broken := env.mustRun(t)
	if broken.Status != freshdesk.StatusFailed || strings.Join(broken.Attachments.ChecksumMismatches, ",") != "9001,9003" {
		t.Fatalf("status=%s mismatches=%v", broken.Status, broken.Attachments.ChecksumMismatches)
	}
	if !hasFailure(broken, "attachment", "9001", "1") || !hasFailure(broken, "attachment", "9003", "2") {
		t.Fatalf("failures = %+v", broken.Failures)
	}
	repaired := env.mustRun(t)
	if repaired.Status != freshdesk.StatusOK || repaired.Attachments.ChecksumVerified != 3 || env.tableCounts(t) != fullImportRows {
		t.Fatalf("status=%s attachments=%+v rows=%s", repaired.Status, repaired.Attachments, env.tableCounts(t))
	}
	if data, err := os.ReadFile(storedPath(t, env, "9001")); err != nil || string(data) != "PDF-BYTES-1" {
		t.Fatalf("repaired file = %q, %v", data, err)
	}
}

func TestDownloadThatDisagreesWithTheReportedSizeLeavesNoFile(t *testing.T) {
	env := newImportEnv(t)
	env.fake.fileBody["9003"] = "log line 1 and much more"              // longer than the reported 10 bytes
	env.fake.fileBody["9002"], env.fake.fileLength["9002"] = "PNG", "9" // shorter than its Content-Length
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || !hasFailure(report, "attachment", "9003", "2") || !hasFailure(report, "attachment", "9002", "1") {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	files := 0
	_ = filepath.WalkDir(env.storageRoot, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files++
		}
		return err
	})
	if files != 1 {
		t.Fatalf("stored files = %d, want only the one valid attachment", files)
	}
}

type nearlyFullDisk struct{ *attachments.LocalDisk }

func (nearlyFullDisk) Available() (int64, error) { return 4 << 10, nil }

func TestImportStopsBeforeDownloadingWhenDiskIsNearlyFull(t *testing.T) {
	env := newImportEnv(t)
	_, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.Storage = nearlyFullDisk{env.storage} })
	if err == nil || !strings.Contains(err.Error(), "free disk space") {
		t.Fatalf("error = %v, want a free disk space stop", err)
	}
	if env.fake.count("/files/9001") != 0 {
		t.Fatal("a download started although the disk was nearly full")
	}
}
