package attachments_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

const workspaceID = "6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b"

func TestLocalDiskStoresStreamWithSHA256AndReadsItBack(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	object, err := storage.Put(context.Background(), workspaceID, strings.NewReader("hello world"), 11)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("hello world") from coreutils sha256sum.
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if object.SHA256 != want || object.Size != 11 {
		t.Fatalf("object = %+v", object)
	}
	if !strings.HasPrefix(object.Key, workspaceID+"/") {
		t.Fatalf("key %q is not scoped to the workspace", object.Key)
	}
	reader, err := storage.Open(context.Background(), object.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "hello world" {
		t.Fatalf("read back %q: %v", data, err)
	}
}

func TestLocalDiskRejectsKeysOutsideItsLayout(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../etc/passwd", workspaceID + "/../../x", "/etc/passwd", ""} {
		if _, err := storage.Open(context.Background(), key); err == nil {
			t.Errorf("Open(%q) succeeded", key)
		}
	}
	if _, err := storage.Put(context.Background(), "../escape", strings.NewReader("x"), -1); err == nil {
		t.Error("Put accepted a non-UUID workspace id")
	}
}

func TestLocalDiskLeavesNoPartialFileWhenStreamFails(t *testing.T) {
	root := t.TempDir()
	storage, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Put(context.Background(), workspaceID, io.MultiReader(strings.NewReader("partial"), failingReader{}), -1); err == nil {
		t.Fatal("Put succeeded on a broken stream")
	}
	count := 0
	err = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return err
	})
	if err != nil || count != 0 {
		t.Fatalf("files after failed put = %d, %v", count, err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestLocalDiskRejectsBytesThatDoNotMatchTheExpectedSize(t *testing.T) {
	root := t.TempDir()
	storage, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []int64{5, 20} {
		if _, err := storage.Put(context.Background(), workspaceID, strings.NewReader("hello world"), expected); err == nil {
			t.Errorf("Put accepted 11 bytes when %d were expected", expected)
		}
	}
	if files := countFiles(t, root); files != 0 {
		t.Fatalf("files after rejected puts = %d", files)
	}
}

func TestLocalDiskDeletesObjectsAndReportsFreeSpace(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	object, err := storage.Put(context.Background(), workspaceID, strings.NewReader("bye"), 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Delete(context.Background(), object.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.Open(context.Background(), object.Key); err == nil {
		t.Fatal("deleted object can still be opened")
	}
	if free, err := storage.Available(); err != nil || free <= 0 {
		t.Fatalf("Available() = %d, %v", free, err)
	}
}

func TestNewLocalDiskRemovesStaleIncomingFilesOnly(t *testing.T) {
	root := t.TempDir()
	if _, err := attachments.NewLocalDisk(root); err != nil {
		t.Fatal(err)
	}
	stale, fresh := filepath.Join(root, ".incoming", "upload-stale"), filepath.Join(root, ".incoming", "upload-fresh")
	for _, path := range []string{stale, fresh} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := attachments.NewLocalDisk(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale temp file was kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a temp file that may still be in use was removed")
	}
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestTrashMovesAWorkspaceAsideAndCanBeUndone(t *testing.T) {
	root := t.TempDir()
	disk, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	const workspace = "11111111-2222-3333-4444-555555555555"
	other := "66666666-2222-3333-4444-555555555555"
	object, err := disk.Put(context.Background(), workspace, strings.NewReader("bytes"), -1)
	if err != nil {
		t.Fatal(err)
	}
	otherObject, err := disk.Put(context.Background(), other, strings.NewReader("keep"), -1)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2027, 11, 1, 0, 0, 0, 0, time.UTC)
	undo, err := disk.Trash(context.Background(), workspace, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disk.Open(context.Background(), object.Key); err == nil {
		t.Fatal("file still readable at its old key after Trash")
	}
	if _, err := os.Stat(filepath.Join(root, ".trash", workspace+"-1825027200")); err != nil {
		t.Fatalf("trash entry missing: %v", err)
	}
	if _, err := disk.Open(context.Background(), otherObject.Key); err != nil {
		t.Fatalf("another workspace was touched: %v", err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	reader, err := disk.Open(context.Background(), object.Key)
	if err != nil {
		t.Fatalf("file not back after undo: %v", err)
	}
	reader.Close()
}

func TestTrashOfAWorkspaceWithoutFilesIsANoOp(t *testing.T) {
	disk, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	undo, err := disk.Trash(context.Background(), "11111111-2222-3333-4444-555555555555", time.Now())
	if err != nil || undo() != nil {
		t.Fatalf("trash/undo of nothing: %v", err)
	}
}

func TestPurgeTrashRemovesOnlyEntriesOlderThanTheCutoff(t *testing.T) {
	root := t.TempDir()
	disk, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	for index, workspace := range []string{"11111111-2222-3333-4444-555555555555", "66666666-2222-3333-4444-555555555555"} {
		if _, err := disk.Put(context.Background(), workspace, strings.NewReader("x"), -1); err != nil {
			t.Fatal(err)
		}
		// Trashed on 2027-11-01 and 2027-11-20.
		if _, err := disk.Trash(context.Background(), workspace, time.Date(2027, 11, 1+19*index, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	purged, err := disk.PurgeTrash(context.Background(), time.Date(2027, 11, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || purged != 1 {
		t.Fatalf("PurgeTrash = %d, %v; want 1 (the 11-01 entry only)", purged, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".trash"))
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "66666666") {
		t.Fatalf("remaining trash: %v", entries)
	}
}
