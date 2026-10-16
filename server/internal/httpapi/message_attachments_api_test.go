package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const outgoingCap = 10 << 20

type upload struct {
	name, contentType string
	data              []byte
}

func multipartMessage(t *testing.T, conversationID, token, kind, body string, files []upload) *http.Request {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err := writer.WriteField("kind", kind); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("body", body); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, file.name))
		header.Set("Content-Type", file.contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

type attachmentView struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

type postedMessage struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Attachments []attachmentView `json:"attachments"`
	Delivery    *struct {
		Status string `json:"status"`
	} `json:"delivery"`
}

func sum(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// storedObjects counts the content files below an attachments root, ignoring temp uploads.
func storedObjects(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".incoming" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

type attachFixture struct {
	api            http.Handler
	pool           *pgxpool.Pool
	root           string
	tokenA, tokenB string
	conversationA  string
	conversationB  string
}

func newAttachFixture(t *testing.T) attachFixture {
	t.Helper()
	root := t.TempDir()
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		storage, err := attachments.NewLocalDisk(root)
		if err != nil {
			t.Fatal(err)
		}
		config.Attachments = storage
	})
	f := attachFixture{api: api, pool: pool, root: root}
	f.tokenA = createSession(t, api, service, "Attach Alpha", "admin@attach-alpha.example.com")
	f.tokenB = createSession(t, api, service, "Attach Bravo", "admin@attach-bravo.example.com")
	connectEmail(t, api, f.tokenA, "support@attach-alpha.example.com")
	contactA := create(t, api, f.tokenA, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@customer.example"})
	f.conversationA = create(t, api, f.tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Invoice"})
	contactB := create(t, api, f.tokenB, "/api/v1/contacts", map[string]any{"name": "Bo", "email": "bo@customer.example"})
	f.conversationB = create(t, api, f.tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Other"})
	return f
}

func (f attachFixture) post(t *testing.T, conversationID, token, kind string, files []upload) *httptest.ResponseRecorder {
	t.Helper()
	return serve(f.api, multipartMessage(t, conversationID, token, kind, "Files attached.", files))
}

func (f attachFixture) wantNothingStored(t *testing.T) {
	t.Helper()
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM attachments`); got != "0" {
		t.Fatalf("attachment rows = %s, want 0", got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM messages`); got != "0" {
		t.Fatalf("message rows = %s, want 0", got)
	}
	if got := storedObjects(t, f.root); got != 0 {
		t.Fatalf("stored objects = %d, want 0", got)
	}
}

func TestReplyWithFilesListsThemAndDownloadsTheSameBytes(t *testing.T) {
	f := newAttachFixture(t)
	pdf := []byte("%PDF-1.7 invoice bytes\x00\x01\x02")
	png := []byte("\x89PNG\r\n\x1a\n screenshot")
	response := f.post(t, f.conversationA, f.tokenA, "outgoing", []upload{
		{"invoice é.pdf", "application/pdf", pdf}, {"shot.png", "image/png", png}})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var message postedMessage
	decode(t, response, &message)
	if len(message.Attachments) != 2 || message.Attachments[0].Filename != "invoice é.pdf" || message.Attachments[1].Filename != "shot.png" {
		t.Fatalf("attachments = %+v", message.Attachments)
	}
	for position, want := range [][]byte{pdf, png} {
		got := message.Attachments[position]
		if got.SHA256 != sum(want) || got.Size != int64(len(want)) {
			t.Fatalf("attachment %d metadata = %+v", position, got)
		}
		download := request(t, f.api, http.MethodGet, "/api/v1/attachments/"+got.ID, nil, f.tokenA, nil)
		if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), want) {
			t.Fatalf("download %d status=%d", position, download.Code)
		}
		if disposition := download.Header().Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment;") {
			t.Fatalf("Content-Disposition = %q", disposition)
		}
		if download.Header().Get("X-Content-Type-Options") != "nosniff" || download.Header().Get("Content-Type") != "application/octet-stream" {
			t.Fatalf("unsafe headers: %v", download.Header())
		}
	}
	if message.Delivery == nil || message.Delivery.Status != "queued" {
		t.Fatalf("a reply with files must be queued for email: %+v", message.Delivery)
	}
	var timeline struct {
		Items []postedMessage `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations/"+f.conversationA+"/messages", nil, f.tokenA, nil), &timeline)
	if len(timeline.Items) != 1 || len(timeline.Items[0].Attachments) != 2 {
		t.Fatalf("timeline = %+v", timeline.Items)
	}
}

func TestNoteWithFilesIsStoredAndNeverQueuedForEmail(t *testing.T) {
	f := newAttachFixture(t)
	response := f.post(t, f.conversationA, f.tokenA, "note", []upload{{"log.txt", "text/plain", []byte("trace")}})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var message postedMessage
	decode(t, response, &message)
	if message.Kind != "note" || len(message.Attachments) != 1 || message.Delivery != nil {
		t.Fatalf("note = %+v", message)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM email_outbox`); got != "0" {
		t.Fatalf("outbox rows = %s, want 0", got)
	}
}

func TestMessageWithoutFilesStillAcceptsMultipartAndJSON(t *testing.T) {
	f := newAttachFixture(t)
	if response := f.post(t, f.conversationA, f.tokenA, "note", nil); response.Code != http.StatusCreated {
		t.Fatalf("multipart without files status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages",
		map[string]any{"kind": "note", "body": "json"}, f.tokenA, nil); response.Code != http.StatusCreated {
		t.Fatalf("json status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTooManyFilesIs413AndStoresNothing(t *testing.T) {
	f := newAttachFixture(t)
	files := make([]upload, 11)
	for i := range files {
		files[i] = upload{fmt.Sprintf("f%d.txt", i), "text/plain", []byte(fmt.Sprintf("content %d", i))}
	}
	response := f.post(t, f.conversationA, f.tokenA, "outgoing", files)
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "10 files") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	f.wantNothingStored(t)
	ten := files[:10]
	if response := f.post(t, f.conversationA, f.tokenA, "note", ten); response.Code != http.StatusCreated {
		t.Fatalf("exactly 10 files must pass: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestFilesOverTheTotalSizeIs413AndStoresNothing(t *testing.T) {
	f := newAttachFixture(t)
	half := bytes.Repeat([]byte("a"), outgoingCap/2)
	rest := bytes.Repeat([]byte("b"), outgoingCap/2+1)
	response := f.post(t, f.conversationA, f.tokenA, "outgoing", []upload{
		{"one.bin", "application/octet-stream", half}, {"two.bin", "application/octet-stream", rest}})
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "10 MiB") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	f.wantNothingStored(t)
	exact := bytes.Repeat([]byte("c"), outgoingCap)
	if response := f.post(t, f.conversationA, f.tokenA, "note", []upload{{"exact.bin", "application/octet-stream", exact}}); response.Code != http.StatusCreated {
		t.Fatalf("exactly 10 MiB must pass: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAnotherWorkspaceConversationIs404AndStoresNothing(t *testing.T) {
	f := newAttachFixture(t)
	response := f.post(t, f.conversationA, f.tokenB, "outgoing", []upload{{"x.txt", "text/plain", []byte("from B")}})
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	f.wantNothingStored(t)
}

func TestAttachmentOfAnotherWorkspaceCannotBeDownloaded(t *testing.T) {
	f := newAttachFixture(t)
	var message postedMessage
	decode(t, f.post(t, f.conversationA, f.tokenA, "outgoing", []upload{{"secret.txt", "text/plain", []byte("alpha only")}}), &message)
	response := request(t, f.api, http.MethodGet, "/api/v1/attachments/"+message.Attachments[0].ID, nil, f.tokenB, nil)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "alpha only") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRefusedReplyRemovesItsStoredFilesButKeepsSharedOnes(t *testing.T) {
	root := t.TempDir()
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		storage, err := attachments.NewLocalDisk(root)
		if err != nil {
			t.Fatal(err)
		}
		config.Attachments = storage
	})
	conversation := f.agentStarted(t)
	body := func(files []upload) *httptest.ResponseRecorder {
		return serve(f.api, multipartMessage(t, conversation, f.adminA, "outgoing", "Hello", files))
	}
	// The trial refuses a reply in a conversation the customer did not start.
	if got := body([]upload{{"a.txt", "text/plain", []byte("unique bytes")}}); got.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
	}
	if got := storedObjects(t, root); got != 0 {
		t.Fatalf("a refused reply left %d stored objects", got)
	}
	// Bytes another message already references survive a refused reply with the same content.
	if got := serve(f.api, multipartMessage(t, conversation, f.adminA, "note", "Keep", []upload{{"a.txt", "text/plain", []byte("shared bytes")}})); got.Code != http.StatusCreated {
		t.Fatalf("note status=%d body=%s", got.Code, got.Body.String())
	}
	if got := body([]upload{{"b.txt", "text/plain", []byte("shared bytes")}}); got.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", got.Code, got.Body.String())
	}
	if got := storedObjects(t, root); got != 1 {
		t.Fatalf("stored objects = %d, want the note's one", got)
	}
}

func TestConfigExposesTheOutgoingAttachmentLimits(t *testing.T) {
	api, _ := testAPI(t)
	response := request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil)
	var config struct {
		Limits struct {
			Bytes int64 `json:"outgoing_attachments_bytes"`
			Files int   `json:"outgoing_attachments_files"`
		} `json:"limits"`
	}
	decode(t, response, &config)
	if config.Limits.Bytes != 10*1024*1024 || config.Limits.Files != 10 {
		t.Fatalf("limits = %+v (%s)", config.Limits, response.Body.String())
	}
}

// lowDisk is attachment storage whose volume reports almost no free space.
type lowDisk struct{ attachments.Storage }

func (lowDisk) Available() (int64, error) { return 1 << 20, nil }

func TestFileIsRefusedWhileTheAttachmentsVolumeIsAlmostFull(t *testing.T) {
	root := t.TempDir()
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		storage, err := attachments.NewLocalDisk(root)
		if err != nil {
			t.Fatal(err)
		}
		config.Attachments = lowDisk{storage}
	})
	token := createSession(t, api, service, "Disk Desk", "admin@disk.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@customer.example"})
	conversation := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Disk"})
	response := serve(api, multipartMessage(t, conversation, token, "note", "Log", []upload{{"a.log", "text/plain", []byte("x")}}))
	if response.Code != http.StatusInsufficientStorage {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM messages`); got != "0" || storedObjects(t, root) != 0 {
		t.Fatalf("a refused upload stored something: messages=%s", got)
	}
}
