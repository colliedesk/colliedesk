package attachments

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutSyncsTheDirectoryAfterRename(t *testing.T) {
	root := t.TempDir()
	storage, err := NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	var synced []string
	storage.syncDir = func(dir string) error {
		synced = append(synced, dir)
		return nil
	}
	object, err := storage.Put(context.Background(), "6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b", strings.NewReader("hello world"), 11)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(filepath.Join(root, filepath.FromSlash(object.Key)))
	if len(synced) == 0 || synced[0] != parent {
		t.Fatalf("synced directories = %v, want %s first", synced, parent)
	}
	storage.syncDir = func(string) error { return errors.New("disk gone") }
	if _, err := storage.Put(context.Background(), "6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b", strings.NewReader("other"), 5); err == nil {
		t.Fatal("Put reported success although the directory could not be synced")
	}
}
