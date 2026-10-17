package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// noteWithFile adds a note with one file through the REST API, as the web app does, and returns the
// attachment id.
func (e *env) noteWithFile(token, conversationID, filename string, content []byte) string {
	e.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("kind", "note")
	_ = form.WriteField("body", "See the file")
	part, err := form.CreateFormFile("files", filename)
	if err != nil {
		e.t.Fatal(err)
	}
	_, _ = part.Write(content)
	_ = form.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	e.api.ServeHTTP(recorder, request)
	var created struct {
		Attachments []struct {
			ID string `json:"id"`
		} `json:"attachments"`
	}
	if recorder.Code != http.StatusCreated || json.Unmarshal(recorder.Body.Bytes(), &created) != nil || len(created.Attachments) != 1 {
		e.t.Fatalf("note with file: %d %s", recorder.Code, recorder.Body.String())
	}
	return created.Attachments[0].ID
}

// timeEntry stores logged time the way the Freshdesk importer does; the REST API cannot create one.
func (e *env) timeEntry(w workspace, conversationID string, minutes int, note, executedAt string) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO time_entries(workspace_id,conversation_id,agent_name,minutes,note,billable,executed_at)
		VALUES($1,$2,'Imported Agent',$3,$4,true,$5)`, w.id, conversationID, minutes, note, executedAt); err != nil {
		e.t.Fatal(err)
	}
}

const missingID = "00000000-0000-4000-8000-000000000000"

func TestInboxActivityListsWhatChangedSinceTheCursor(t *testing.T) {
	e := newEnv(t)
	since := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")

	first := e.ok(session, "colliedesk_inbox_activity", nil)
	if len(at(t, first, "new_conversations").([]any)) != 0 || at(t, first, "now") == "" {
		t.Errorf("without since the lists must be empty and now the first cursor: %v", first)
	}
	out := e.ok(session, "colliedesk_inbox_activity", map[string]any{"since": since})
	created := at(t, out, "new_conversations").([]any)
	if len(created) != 3 {
		t.Fatalf("new_conversations = %v, want the 3 conversations of this workspace", created)
	}
	subjects := []string{}
	for _, item := range created {
		subjects = append(subjects, at(t, item, "untrusted_customer_content", "subject").(string))
	}
	if strings.Contains(strings.Join(subjects, ","), f.otherSubject) || !strings.Contains(strings.Join(subjects, ","), "Printer jam") {
		t.Errorf("subjects = %v, want this workspace's only", subjects)
	}
	if at(t, out, "timezone") != "UTC" || !utcSeconds.MatchString(at(t, created[0], "created_at").(string)) {
		t.Errorf("times must be UTC to the second with the workspace timezone: %v", out)
	}
}

func TestTimeEntriesListShowsLoggedTimeOfOneConversation(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.timeEntry(f.w, f.c1, 30, "Called the customer", "2026-10-01T08:30:00Z")
	e.timeEntry(f.w, f.c1, 15, "Wrote the fix", "2026-10-02T08:30:00Z")
	e.timeEntry(f.other, f.otherConvID, 99, "Bravo secret work", "2026-10-01T08:30:00Z")
	session := e.connect(f.w.read, "")

	out := e.ok(session, "colliedesk_time_entries_list", map[string]any{"conversation_id": f.c1})
	if at(t, out, "total_minutes") != float64(45) || len(at(t, out, "entries").([]any)) != 2 {
		t.Fatalf("want two entries of 45 minutes in total: %v", out)
	}
	if at(t, out, "entries", 0, "untrusted_customer_content", "note") != "Called the customer" || at(t, out, "entries", 0, "billable") != true {
		t.Errorf("first entry = %v", at(t, out, "entries", 0))
	}
	if message := e.fails(session, "colliedesk_time_entries_list", map[string]any{"conversation_id": f.otherConvID}); !strings.Contains(message, "No conversation") {
		t.Errorf("another workspace's conversation: %q", message)
	}
}

func TestAttachmentGetReturnsMetadataAndTheTextOfASmallTextFile(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	textID := e.noteWithFile(f.w.admin, f.c1, "steps.txt", []byte("1. Open the tray\n2. Pull the paper"))
	imageID := e.noteWithFile(f.w.admin, f.c1, "photo.png", append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...))
	otherID := e.noteWithFile(f.other.admin, f.otherConvID, "bravo.txt", []byte("Bravo secret file"))
	session := e.connect(f.w.read, "")

	out := e.ok(session, "colliedesk_attachment_get", map[string]any{"attachment_id": textID})
	if at(t, out, "untrusted_customer_content", "filename") != "steps.txt" || at(t, out, "untrusted_customer_content", "text") != "1. Open the tray\n2. Pull the paper" ||
		at(t, out, "size") != float64(34) || at(t, out, "is_text") != true {
		t.Errorf("text attachment = %v", out)
	}
	image := e.ok(session, "colliedesk_attachment_get", map[string]any{"attachment_id": imageID})
	if at(t, image, "is_text") != false || at(t, image, "detected_type") != "image/png" || at(t, image, "untrusted_customer_content", "text") != nil {
		t.Errorf("a binary file must come back as metadata only: %v", image)
	}
	if !strings.Contains(at(t, image, "hint").(string), "web app") {
		t.Errorf("a binary file must say where to open it: %v", image)
	}
	if message := e.fails(session, "colliedesk_attachment_get", map[string]any{"attachment_id": otherID}); !strings.Contains(message, "No attachment") {
		t.Errorf("another workspace's attachment: %q", message)
	}
}

func TestConversationGetGivesTheIDsOfAttachments(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	id := e.noteWithFile(f.w.admin, f.c1, "steps.txt", []byte("hello"))

	out := e.ok(e.connect(f.w.read, ""), "colliedesk_conversation_get", map[string]any{"conversation_id": f.c1})
	if got := at(t, out, "messages", 0, "attachments", 0, "id"); got != id {
		t.Errorf("attachment id = %v, want %s: %v", got, id, at(t, out, "messages", 0))
	}
}

func TestContactGetShowsTheCustomerAndTheirConversations(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")
	conversation := e.ok(session, "colliedesk_conversation_get", map[string]any{"conversation_id": f.c1, "detail": "full"})
	grace := at(t, conversation, "conversation", "contact_id").(string)

	out := e.ok(session, "colliedesk_contact_get", map[string]any{"contact_id": grace})
	if at(t, out, "contact", "untrusted_customer_content", "email") != "grace@cust.example.com" || at(t, out, "conversations_total") != float64(2) {
		t.Fatalf("contact = %v, want Grace with 2 conversations", out)
	}
	if at(t, out, "conversations", 0, "untrusted_customer_content", "subject") == nil {
		t.Errorf("conversation rows must carry the subject: %v", out)
	}
	_, other := e.rest(http.MethodGet, "/api/v1/conversations/"+f.otherConvID, nil, f.other.admin)
	if message := e.fails(session, "colliedesk_contact_get", map[string]any{"contact_id": other["contact_id"]}); !strings.Contains(message, "No contact") {
		t.Errorf("another workspace's contact: %q", message)
	}
}

// A file larger than one call reads is cut, and the size still says how big it is.
func TestAttachmentGetCutsALargeTextFile(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	id := e.noteWithFile(f.w.admin, f.c1, "server.log", []byte(strings.Repeat("é line of log\n", 20000)))

	out := e.ok(e.connect(f.w.read, ""), "colliedesk_attachment_get", map[string]any{"attachment_id": id})
	text := at(t, out, "untrusted_customer_content", "text").(string)
	if at(t, out, "size") != float64(300000) || at(t, out, "text_truncated") != true || !strings.HasPrefix(text, "é line of log") || !strings.Contains(text, "web app") {
		t.Errorf("large file: size=%v truncated=%v text ends %q", at(t, out, "size"), at(t, out, "text_truncated"), text[len(text)-120:])
	}
}
