package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// lockObject takes the lock that serializes deleting an object against referencing it, the way an
// in-flight request does between writing the bytes and committing its row.
func lockObject(t *testing.T, f attachFixture, key string) (release func(), insert func(workspaceID, messageID string)) {
	t.Helper()
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		t.Fatal(err)
	}
	return func() { _ = tx.Rollback(context.Background()) }, func(workspaceID, messageID string) {
		if _, err := store.InsertAttachment(context.Background(), tx, workspaceID, store.NewAttachment{MessageID: messageID, Filename: "shared.txt",
			Object: attachments.Object{Key: key, Size: 6, SHA256: sum([]byte("shared"))}}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiscardWaitsForAnInFlightReferenceAndThenKeepsTheObject(t *testing.T) {
	f := newAttachFixture(t)
	storage, err := attachments.NewLocalDisk(f.root)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaceID(t, f.api, f.tokenA)
	messageID := create(t, f.api, f.tokenA, "/api/v1/conversations/"+f.conversationA+"/messages", map[string]any{"kind": "note", "body": "holder"})
	object, err := storage.Put(context.Background(), workspace, strings.NewReader("shared"), 6)
	if err != nil {
		t.Fatal(err)
	}
	release, insert := lockObject(t, f, object.Key)
	defer release()
	done := make(chan error, 1)
	go func() {
		done <- store.New(f.pool).WithObjects(storage).DiscardObject(context.Background(), workspace, object.Key)
	}()
	select {
	case err := <-done:
		t.Fatalf("discard finished (%v) while another request was about to reference the object", err)
	case <-time.After(300 * time.Millisecond):
	}
	insert(workspace, messageID)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	body, err := storage.Open(context.Background(), object.Key)
	if err != nil {
		t.Fatalf("an object a committed row references was deleted: %v", err)
	}
	_ = body.Close()
}

func TestMessageNeverReferencesAnObjectThatIsGone(t *testing.T) {
	f := newAttachFixture(t)
	storage, err := attachments.NewLocalDisk(f.root)
	if err != nil {
		t.Fatal(err)
	}
	workspace := workspaceID(t, f.api, f.tokenA)
	object, err := storage.Put(context.Background(), workspace, strings.NewReader("vanishing"), 9)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Delete(context.Background(), object.Key); err != nil {
		t.Fatal(err)
	}
	agent := queryString(t, f.pool, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace)
	_, err = store.New(f.pool).WithObjects(storage).CreateMessage(context.Background(), workspace, f.conversationA, store.Actor{AgentID: agent}, "note", "x",
		store.NewAttachment{Filename: "v.txt", Object: object})
	if !errors.Is(err, store.ErrObjectMissing) {
		t.Fatalf("err = %v, want ErrObjectMissing", err)
	}
	f.wantNothingStored(t)
}

// countingStorage counts the files written through it.
type countingStorage struct {
	attachments.Storage
	puts atomic.Int32
}

func (c *countingStorage) Put(ctx context.Context, workspaceID string, body io.Reader, size int64) (attachments.Object, error) {
	c.puts.Add(1)
	return c.Storage.Put(ctx, workspaceID, body, size)
}

func TestRefusedReplyWritesNoBytesToStorage(t *testing.T) {
	var counting *countingStorage
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		disk, err := attachments.NewLocalDisk(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		counting = &countingStorage{Storage: disk}
		config.Attachments = counting
	})
	conversation := f.agentStarted(t)
	got := serve(f.api, multipartMessage(t, conversation, f.adminA, "outgoing", "Hello", []upload{{"a.txt", "text/plain", []byte("never written")}}))
	if got.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
	}
	if counting.puts.Load() != 0 {
		t.Fatalf("a reply the trial refuses wrote %d files before refusing", counting.puts.Load())
	}
}

func TestFilesBeforeKindAndBodyAreRefusedBeforeAnyByteIsWritten(t *testing.T) {
	f := newAttachFixture(t)
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="files"; filename="early.txt"`)
	part, _ := writer.CreatePart(header)
	_, _ = part.Write([]byte("early bytes"))
	_ = writer.WriteField("kind", "note")
	_ = writer.WriteField("body", "late")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+f.tokenA)
	got := serve(f.api, req)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
	}
	f.wantNothingStored(t)
	// An invalid kind is refused before the first file too.
	bad := serve(f.api, multipartMessage(t, f.conversationA, f.tokenA, "shout", "x", []upload{{"a.txt", "text/plain", []byte("a")}}))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind status=%d", bad.Code)
	}
	f.wantNothingStored(t)
}

func TestStalledUploadIsCutOffAndLeavesNothing(t *testing.T) {
	root := t.TempDir()
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		storage, err := attachments.NewLocalDisk(root)
		if err != nil {
			t.Fatal(err)
		}
		config.Attachments = storage
		config.UploadTimeout = 400 * time.Millisecond
	})
	token := createSession(t, api, service, "Stall Desk", "admin@stall.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@customer.example"})
	conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Stall"})
	server := httptest.NewServer(api)
	defer server.Close()
	reader, pipe := io.Pipe()
	writer := multipart.NewWriter(pipe)
	go func() {
		_ = writer.WriteField("kind", "note")
		_ = writer.WriteField("body", "slow")
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="files"; filename="slow.bin"`)
		part, _ := writer.CreatePart(header)
		_, _ = part.Write([]byte("first bytes then silence"))
		time.Sleep(3 * time.Second) // never finishes within the deadline
		_ = pipe.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/conversations/"+conversation+"/messages", reader)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	started := time.Now()
	response, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = response.Body.Close()
		if response.StatusCode == http.StatusCreated {
			t.Fatal("a stalled upload must not create the message")
		}
	}
	if time.Since(started) > 2500*time.Millisecond {
		t.Fatalf("the stalled upload was held for %v", time.Since(started))
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM messages`); got != "0" || storedObjects(t, root) != 0 {
		t.Fatalf("a stalled upload left messages=%s objects=%d", got, storedObjects(t, root))
	}
}
