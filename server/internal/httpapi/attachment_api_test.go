package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestTimelineListsAttachmentsAndDownloadStreamsRecordedBytes(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Files Alpha", "admin@files-alpha.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@files.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Invoice"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "See file"})
	attachmentID := seedAttachment(t, pool, storage, token, api, messageID, `in"voice.txt`, "hello world")

	timeline := request(t, api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, token, nil)
	var list struct {
		Items []struct {
			ID          string `json:"id"`
			Attachments []struct {
				ID       string `json:"id"`
				Filename string `json:"filename"`
				Size     int64  `json:"size"`
				SHA256   string `json:"sha256"`
			} `json:"attachments"`
		} `json:"items"`
	}
	decode(t, timeline, &list)
	if len(list.Items) != 1 || len(list.Items[0].Attachments) != 1 {
		t.Fatalf("timeline = %s", timeline.Body.String())
	}
	got := list.Items[0].Attachments[0]
	const helloSHA = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got.ID != attachmentID || got.Filename != `in"voice.txt` || got.Size != 11 || got.SHA256 != helloSHA {
		t.Fatalf("attachment metadata = %+v", got)
	}

	download := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID, nil, token, nil)
	if download.Code != http.StatusOK || download.Body.String() != "hello world" {
		t.Fatalf("download status=%d body=%q", download.Code, download.Body.String())
	}
	if download.Header().Get("Content-Type") != "application/octet-stream" || download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unsafe download headers: %v", download.Header())
	}
	disposition := download.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, "filename*=UTF-8''in%22voice.txt") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}
	if download.Header().Get("X-Content-SHA256") != helloSHA {
		t.Fatalf("X-Content-SHA256 = %q", download.Header().Get("X-Content-SHA256"))
	}
}

func TestAttachmentDownloadIsWorkspaceScoped(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	tokenA := createSession(t, api, service, "Files A", "admin@files-a.example.com")
	tokenB := createSession(t, api, service, "Files B", "admin@files-b.example.com")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "A", "email": "a@files.example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Secret"})
	messageA := create(t, api, tokenA, "/api/v1/conversations/"+conversationA+"/messages", map[string]any{"kind": "note", "body": "x"})
	attachmentA := seedAttachment(t, pool, storage, tokenA, api, messageA, "secret.txt", "workspace-a-secret")

	for _, path := range []string{"/api/v1/attachments/" + attachmentA, "/api/v1/attachments/00000000-0000-4000-8000-000000000000", "/api/v1/attachments/not-a-uuid"} {
		response := request(t, api, http.MethodGet, path, nil, tokenB, nil)
		if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "workspace-a-secret") {
			t.Fatalf("GET %s from workspace B: status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
	if response := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentA, nil, tokenA, nil); response.Code != http.StatusOK {
		t.Fatalf("owner download status=%d", response.Code)
	}
}

func TestDownloadFilenameUsesOnlyRFC5987AttrChars(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	token := createSession(t, api, service, "Names Desk", "admin@names.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "N", "email": "n@names.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Names"})
	messageID := create(t, api, token, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "note", "body": "x"})
	attachmentID := seedAttachment(t, pool, storage, token, api, messageID, "a;b,c=d é.txt", "names")
	disposition := request(t, api, http.MethodGet, "/api/v1/attachments/"+attachmentID, nil, token, nil).Header().Get("Content-Disposition")
	if !strings.Contains(disposition, "filename*=UTF-8''a%3Bb%2Cc%3Dd%20%C3%A9.txt") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}
}

func TestDownloadRefusesAStorageKeyOfAnotherWorkspace(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	tokenA := createSession(t, api, service, "Keys A", "admin@keys-a.example.com")
	tokenB := createSession(t, api, service, "Keys B", "admin@keys-b.example.com")
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "B", "email": "b@keys.example.com"})
	conversationB := create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "B"})
	messageB := create(t, api, tokenB, "/api/v1/conversations/"+conversationB+"/messages", map[string]any{"kind": "note", "body": "b"})
	attachmentB := seedAttachment(t, pool, storage, tokenB, api, messageB, "b.txt", "workspace-b-bytes")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "A", "email": "a@keys.example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "A"})
	messageA := create(t, api, tokenA, "/api/v1/conversations/"+conversationA+"/messages", map[string]any{"kind": "note", "body": "a"})
	var rowA string
	// A corrupted row in workspace A that points at workspace B's stored bytes.
	err := pool.QueryRow(t.Context(), `INSERT INTO attachments(workspace_id,message_id,filename,content_type,size,sha256,storage_key)
		SELECT $1,$2,'stolen.txt','text/plain',size,sha256,storage_key FROM attachments WHERE id=$3 RETURNING id::text`,
		workspaceID(t, api, tokenA), messageA, attachmentB).Scan(&rowA)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, api, http.MethodGet, "/api/v1/attachments/"+rowA, nil, tokenA, nil)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "workspace-b-bytes") {
		t.Fatalf("download of another workspace's stored key: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestSingleMessageResponsesIncludeTheirAttachments(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Attachments = storage })
	token := createSession(t, api, service, "Single Message", "admin@single.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "S", "email": "s@single.example.com"})
	conversationID := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Retry"})
	api, _, replyID := withFailedEmailReply(t, api, pool, token, conversationID)
	attachmentID := seedAttachment(t, pool, storage, token, api, replyID, "report.pdf", "pdf bytes")
	response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages/"+replyID+"/retry", nil, token, nil)
	var message struct {
		Attachments []struct {
			ID string `json:"id"`
		} `json:"attachments"`
	}
	decode(t, response, &message)
	if response.Code != http.StatusOK || len(message.Attachments) != 1 || message.Attachments[0].ID != attachmentID {
		t.Fatalf("retry response status=%d body=%s", response.Code, response.Body.String())
	}
}
