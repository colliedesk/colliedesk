package export_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
)

const (
	workspaceID = "11111111-1111-4111-8111-111111111111"
	// sha256 of "hello" and of "world", from the SHA-256 spec test vectors.
	helloSHA = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	worldSHA = "486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7"
)

type memoryStorage map[string]string

func (m memoryStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := m[key]
	if !ok {
		return nil, errors.New("stored bytes are gone")
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func attachment(id, filename, content, sha string) export.Attachment {
	return export.Attachment{
		ID: id, Filename: filename, ContentType: "text/plain", Size: int64(len(content)), SHA256: sha,
		StorageKey: workspaceID + "/" + sha[:2] + "/" + sha,
	}
}

func conversationWith(attachments ...export.Attachment) export.Conversation {
	return export.Conversation{
		ID: "c1", ContactID: "k1", Subject: "s", Status: "open", Priority: "normal", TagIDs: []string{},
		Messages: []export.Message{{ID: "m1", ConversationID: "c1", Kind: "note", Body: "b", Attachments: attachments}},
	}
}

func newWriter(t *testing.T, buf *bytes.Buffer, storage memoryStorage) *export.Writer {
	t.Helper()
	writer := export.NewWriter(buf, storage, workspaceID, time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC))
	if err := writer.Workspace(domain.Workspace{ID: workspaceID, Slug: "acme", Name: "Acme", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	return writer
}

func names(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	result := map[string]string{}
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(handle)
		handle.Close()
		if err != nil {
			t.Fatal(err)
		}
		result[file.Name] = string(content)
	}
	return result
}

func TestEntryNamesAreSanitizedAndDuplicateFilenamesStayApart(t *testing.T) {
	storage := memoryStorage{
		workspaceID + "/2c/" + helloSHA: "hello",
		workspaceID + "/48/" + worldSHA: "world",
	}
	var buf bytes.Buffer
	writer := newWriter(t, &buf, storage)
	err := writer.Conversation(conversationWith(
		attachment("a1", "../x", "hello", helloSHA),
		attachment("a2", `..\..\evil/report.txt`, "world", worldSHA),
		attachment("a3", "report.txt", "hello", helloSHA),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := names(t, buf.Bytes())
	for path, want := range map[string]string{
		"attachments/a1/x":          "hello",
		"attachments/a2/report.txt": "world",
		"attachments/a3/report.txt": "hello",
	} {
		if got[path] != want {
			t.Errorf("%s = %q, want %q (entries: %v)", path, got[path], want, keys(got))
		}
	}
	for path := range got {
		if strings.Contains(path, "..") || strings.HasPrefix(path, "/") || strings.Contains(path, `\`) {
			t.Errorf("unsafe entry name %q", path)
		}
	}
}

func TestManifestListsCountsAndEveryAttachmentFile(t *testing.T) {
	storage := memoryStorage{workspaceID + "/2c/" + helloSHA: "hello"}
	var buf bytes.Buffer
	writer := newWriter(t, &buf, storage)
	if err := writer.Contact(export.Contact{ID: "k1", Name: "K", Email: "k@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Conversation(conversationWith(attachment("a1", "hello.txt", "hello", helloSHA))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(context.Background()); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Format     string         `json:"format"`
		Version    int            `json:"version"`
		ExportedAt string         `json:"exported_at"`
		Counts     map[string]int `json:"counts"`
		Files      []struct {
			Path   string `json:"path"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(names(t, buf.Bytes())["manifest.json"]), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != "colliedesk-export" || manifest.Version != 1 || manifest.ExportedAt != "2026-10-15T06:00:00Z" {
		t.Fatalf("manifest header = %+v", manifest)
	}
	want := map[string]int{"agents": 0, "contacts": 1, "tags": 0, "conversations": 1, "messages": 1, "attachments": 1, "time_entries": 0}
	for key, count := range want {
		if manifest.Counts[key] != count {
			t.Errorf("counts[%s] = %d, want %d", key, manifest.Counts[key], count)
		}
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Path != "attachments/a1/hello.txt" || manifest.Files[0].Size != 5 || manifest.Files[0].SHA256 != helloSHA {
		t.Fatalf("manifest files = %+v", manifest.Files)
	}
}

func TestMissingStoredFileAbortsWithoutManifest(t *testing.T) {
	var buf bytes.Buffer
	writer := newWriter(t, &buf, memoryStorage{})
	if err := writer.Conversation(conversationWith(attachment("a1", "hello.txt", "hello", helloSHA))); err != nil {
		t.Fatal(err)
	}
	err := writer.Finish(context.Background())
	if err == nil || !strings.Contains(err.Error(), "a1") {
		t.Fatalf("Finish error = %v, want one naming attachment a1", err)
	}
	if bytes.Contains(buf.Bytes(), []byte("manifest.json")) {
		t.Fatal("an aborted export must not carry a manifest")
	}
}

func TestStoredBytesThatDifferFromTheRecordedChecksumAbort(t *testing.T) {
	storage := memoryStorage{workspaceID + "/2c/" + helloSHA: "HELLO"}
	var buf bytes.Buffer
	writer := newWriter(t, &buf, storage)
	if err := writer.Conversation(conversationWith(attachment("a1", "hello.txt", "hello", helloSHA))); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(context.Background()); err == nil {
		t.Fatal("corrupted stored bytes were exported as if intact")
	}
}

func TestStorageKeyOfAnotherWorkspaceIsRefused(t *testing.T) {
	other := "22222222-2222-4222-8222-222222222222"
	item := attachment("a1", "hello.txt", "hello", helloSHA)
	item.StorageKey = other + "/2c/" + helloSHA
	storage := memoryStorage{item.StorageKey: "hello"}
	var buf bytes.Buffer
	writer := newWriter(t, &buf, storage)
	if err := writer.Conversation(conversationWith(item)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Finish(context.Background()); err == nil {
		t.Fatal("a file stored under another workspace was exported")
	}
}

func TestRecordsMustArriveInArchiveOrder(t *testing.T) {
	var buf bytes.Buffer
	writer := newWriter(t, &buf, memoryStorage{})
	if err := writer.Conversation(conversationWith()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Contact(export.Contact{ID: "k1", Email: "k@example.com"}); err == nil {
		t.Fatal("a contact after the conversations was accepted")
	}
}

func keys(m map[string]string) []string {
	result := make([]string, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	return result
}
