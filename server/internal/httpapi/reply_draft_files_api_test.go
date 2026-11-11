package httpapi_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// multipartDraft builds a reply draft with files: the text and copies first, then the files.
func multipartDraft(t *testing.T, conversationID, token, text string, files []upload, cc ...string) *http.Request {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	if err := writer.WriteField("text", text); err != nil {
		t.Fatal(err)
	}
	for _, address := range cc {
		if err := writer.WriteField("cc", address); err != nil {
			t.Fatal(err)
		}
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+conversationID+"/reply-drafts", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

type draftFileView struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type draftView struct {
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	Source     string          `json:"source"`
	Outcome    *string         `json:"outcome"`
	Text       *string         `json:"text"`
	Files      []draftFileView `json:"files"`
	RestoredAt *string         `json:"restored_at"`
	DecidedBy  *string         `json:"decided_by_name"`
}

func postDraft(t *testing.T, f attachFixture, conversation, token, text string, files []upload) draftView {
	t.Helper()
	response := serve(f.api, multipartDraft(t, conversation, token, text, files))
	if response.Code != http.StatusCreated {
		t.Fatalf("reply draft with files: %d %s", response.Code, response.Body.String())
	}
	var draft draftView
	decode(t, response, &draft)
	return draft
}

func listDrafts(t *testing.T, f attachFixture, conversation, token string) []draftView {
	t.Helper()
	var list struct {
		Items []draftView `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/ai-drafts", nil, token, nil), &list)
	return list.Items
}

func draftFilePath(conversation, draft, file string) string {
	return "/api/v1/conversations/" + conversation + "/ai-drafts/" + draft + "/files/" + file
}

func TestAReplyDraftCarriesFilesOnlyTheTeamSeesUntilAPersonSendsThem(t *testing.T) {
	f := newAttachFixture(t)
	writer := issueToken(t, f.api, f.tokenA, "Writer", "draft").Token
	invoice, photo := []byte("%PDF invoice for Ada"), []byte("\x89PNG photo of the printer")
	draft := postDraft(t, f, f.conversationA, writer, "Here is your invoice.", []upload{
		{"invoice.pdf", "application/pdf", invoice}, {"printer.png", "image/png", photo}})
	if draft.Status != "ready" || len(draft.Files) != 2 || draft.Files[0].Filename != "invoice.pdf" || draft.Files[1].Size != int64(len(photo)) {
		t.Fatalf("draft = %+v", draft)
	}
	// Nothing reached the timeline or the customer: no message, no attachment row.
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM messages`); got != "0" {
		t.Fatalf("a draft wrote %s messages", got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM attachments`); got != "0" {
		t.Fatalf("a draft wrote %s attachment rows", got)
	}
	// The team downloads the draft's file with the same safe headers as an attachment.
	download := request(t, f.api, http.MethodGet, draftFilePath(f.conversationA, draft.ID, draft.Files[0].ID), nil, f.tokenA, nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), invoice) ||
		download.Header().Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") ||
		download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("download: %d %v", download.Code, download.Header())
	}
	counted := queryString(t, f.pool, `SELECT counted_storage_bytes::text FROM workspaces WHERE id=(SELECT workspace_id FROM ai_drafts WHERE id=$1)`, draft.ID)
	if counted != fmt.Sprint(len(invoice)+len(photo)) {
		t.Fatalf("counted storage = %s, want the draft's files", counted)
	}
	// The person keeps the invoice and drops the photo: the reply carries only the invoice.
	var sent postedMessage
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages", map[string]any{"kind": "outgoing", "body": "Here is your invoice.",
		"ai_draft_id": draft.ID, "ai_draft_file_ids": []string{draft.Files[0].ID}}, f.tokenA, http.StatusCreated, &sent)
	if len(sent.Attachments) != 1 || sent.Attachments[0].Filename != "invoice.pdf" || sent.Attachments[0].SHA256 != sum(invoice) {
		t.Fatalf("sent attachments = %+v", sent.Attachments)
	}
	if sent.Delivery == nil || sent.Delivery.Status != "queued" {
		t.Fatalf("the reply with the draft's file was not queued: %+v", sent.Delivery)
	}
	// The draft's own rows go with the send; the dropped photo leaves no stored bytes behind.
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM ai_draft_files`); got != "0" {
		t.Fatalf("draft file rows after send = %s", got)
	}
	if got := storedObjects(t, f.root); got != 1 {
		t.Fatalf("stored objects after send = %d, want the invoice only", got)
	}
	if got := queryString(t, f.pool, `SELECT counted_storage_bytes::text FROM workspaces WHERE id=(SELECT workspace_id FROM ai_drafts WHERE id=$1)`, draft.ID); got != fmt.Sprint(len(invoice)) {
		t.Fatalf("counted storage after send = %s, want the invoice once", got)
	}
	reply := request(t, f.api, http.MethodGet, "/api/v1/attachments/"+sent.Attachments[0].ID, nil, f.tokenA, nil)
	if reply.Code != http.StatusOK || !bytes.Equal(reply.Body.Bytes(), invoice) {
		t.Fatalf("the sent file downloads as %d", reply.Code)
	}
}

func TestSendingADraftWithoutChoosingFilesSendsAllOfThem(t *testing.T) {
	f := newAttachFixture(t)
	draft := postDraft(t, f, f.conversationA, f.tokenA, "Both files.", []upload{{"a.txt", "text/plain", []byte("one")}, {"b.txt", "text/plain", []byte("two")}})
	var sent postedMessage
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages", map[string]any{"kind": "outgoing", "body": "Both files.", "ai_draft_id": draft.ID},
		f.tokenA, http.StatusCreated, &sent)
	if len(sent.Attachments) != 2 {
		t.Fatalf("sent attachments = %+v, want both of the draft's files", sent.Attachments)
	}
	// An empty list sends none of them.
	other := postDraft(t, f, f.conversationA, f.tokenA, "No files.", []upload{{"c.txt", "text/plain", []byte("three")}})
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages", map[string]any{"kind": "outgoing", "body": "No files.", "ai_draft_id": other.ID,
		"ai_draft_file_ids": []string{}}, f.tokenA, http.StatusCreated, &sent)
	if len(sent.Attachments) != 0 {
		t.Fatalf("an empty ai_draft_file_ids sent %+v", sent.Attachments)
	}
}

func TestDraftFilesNeverCrossWorkspacesOrDrafts(t *testing.T) {
	f := newAttachFixture(t)
	draft := postDraft(t, f, f.conversationA, f.tokenA, "Alpha's file.", []upload{{"alpha.txt", "text/plain", []byte("alpha only")}})
	file := draft.Files[0].ID
	// Workspace B reads no file, draft or restore of A, by A's ids or by its own conversation.
	for _, path := range []string{draftFilePath(f.conversationA, draft.ID, file), draftFilePath(f.conversationB, draft.ID, file)} {
		if response := request(t, f.api, http.MethodGet, path, nil, f.tokenB, nil); response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "alpha only") {
			t.Fatalf("B reads A's draft file at %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	if response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+draft.ID+"/restore", nil, f.tokenB, nil); response.Code != http.StatusNotFound {
		t.Fatalf("B restores A's draft: %d %s", response.Code, response.Body.String())
	}
	before := storedObjects(t, f.root)
	if response := serve(f.api, multipartDraft(t, f.conversationA, f.tokenB, "Into A", []upload{{"b.txt", "text/plain", []byte("from B")}})); response.Code != http.StatusNotFound {
		t.Fatalf("B drafts with files into A's conversation: %d %s", response.Code, response.Body.String())
	}
	if storedObjects(t, f.root) != before {
		t.Fatal("a refused draft into another workspace stored its file")
	}
	// B's own draft cannot pull A's file into B's reply.
	own := postDraft(t, f, f.conversationB, f.tokenB, "Bravo.", []upload{{"bravo.txt", "text/plain", []byte("bravo")}})
	response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationB+"/messages", map[string]any{"kind": "outgoing", "body": "Bravo.",
		"ai_draft_id": own.ID, "ai_draft_file_ids": []string{file}}, f.tokenB, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "ai_draft_file_ids") {
		t.Fatalf("B sends A's file with its own draft: %d %s", response.Code, response.Body.String())
	}
	// Nor does another draft's file of the same workspace go with this draft.
	second := postDraft(t, f, f.conversationA, issueToken(t, f.api, f.tokenA, "Other", "draft").Token, "Another.", []upload{{"other.txt", "text/plain", []byte("other")}})
	response = request(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages", map[string]any{"kind": "outgoing", "body": "Alpha's file.",
		"ai_draft_id": draft.ID, "ai_draft_file_ids": []string{second.Files[0].ID}}, f.tokenA, nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("another draft's file: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM messages`); got != "0" {
		t.Fatalf("a refused send stored %s messages", got)
	}
}

func TestADraftTakesAtMostTenFilesAndAReplyNoMoreThanAMessage(t *testing.T) {
	f := newAttachFixture(t)
	files := make([]upload, 11)
	for index := range files {
		files[index] = upload{fmt.Sprintf("f%d.txt", index), "text/plain", []byte(fmt.Sprintf("file %d", index))}
	}
	response := serve(f.api, multipartDraft(t, f.conversationA, f.tokenA, "Eleven", files))
	if response.Code != http.StatusRequestEntityTooLarge || !strings.Contains(response.Body.String(), "attachments_too_many") {
		t.Fatalf("eleven files: %d %s", response.Code, response.Body.String())
	}
	if got := storedObjects(t, f.root); got != 0 {
		t.Fatalf("a refused draft left %d stored objects", got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM ai_drafts`); got != "0" {
		t.Fatalf("a refused draft left %s drafts", got)
	}
	// Ten on the draft and one more uploaded with the send are eleven on one reply: refused, nothing sent.
	draft := postDraft(t, f, f.conversationA, f.tokenA, "Ten", files[:10])
	req := multipartMessage(t, f.conversationA, f.tokenA, "outgoing", "Ten", []upload{{"extra.txt", "text/plain", []byte("extra")}})
	withDraft := addDraftField(t, req, draft.ID)
	if response := serve(f.api, withDraft); response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("ten draft files plus one: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM messages`); got != "0" {
		t.Fatalf("an oversized send stored %s messages", got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM ai_draft_files`); got != "10" {
		t.Fatalf("an oversized send left %s of the draft's ten files", got)
	}
}

// addDraftField rebuilds a multipart message request with an ai_draft_id field ahead of its files.
func addDraftField(t *testing.T, req *http.Request, draftID string) *http.Request {
	t.Helper()
	reader, err := req.MultipartReader()
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	added := false
	for {
		part, err := reader.NextPart()
		if err != nil {
			break
		}
		if part.FileName() != "" && !added {
			if err := writer.WriteField("ai_draft_id", draftID); err != nil {
				t.Fatal(err)
			}
			added = true
		}
		copied, err := writer.CreatePart(part.Header)
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if _, err := data.ReadFrom(part); err != nil {
			t.Fatal(err)
		}
		if _, err := copied.Write(data.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	out := httptest.NewRequest(req.Method, req.URL.Path, &buffer)
	out.Header.Set("Content-Type", writer.FormDataContentType())
	out.Header.Set("Authorization", req.Header.Get("Authorization"))
	return out
}

func TestEarlierDraftsStayWithTheirFilesAndComeBack(t *testing.T) {
	f := newAttachFixture(t)
	writer := issueToken(t, f.api, f.tokenA, "Writer", "draft").Token
	first := postDraft(t, f, f.conversationA, writer, "First try.", []upload{{"first.txt", "text/plain", []byte("first file")}})
	second := postDraft(t, f, f.conversationA, writer, "Second try.", nil)
	drafts := listDrafts(t, f, f.conversationA, f.tokenA)
	if len(drafts) != 2 || drafts[0].ID != second.ID || drafts[1].ID != first.ID {
		t.Fatalf("drafts = %+v, want the new one first and the earlier one kept", drafts)
	}
	if drafts[1].Outcome == nil || *drafts[1].Outcome != "superseded" || drafts[1].Text == nil || *drafts[1].Text != "First try." || len(drafts[1].Files) != 1 {
		t.Fatalf("the earlier draft = %+v, want superseded with its text and file", drafts[1])
	}
	// A person brings the first one back: it waits again and the second is the earlier one now.
	var restored draftView
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+first.ID+"/restore", nil, f.tokenA, http.StatusOK, &restored)
	if restored.ID != first.ID || restored.Outcome != nil || restored.RestoredAt == nil || len(restored.Files) != 1 {
		t.Fatalf("restored = %+v", restored)
	}
	drafts = listDrafts(t, f, f.conversationA, f.tokenA)
	if waiting := waitingDrafts(drafts); len(waiting) != 1 || waiting[0] != first.ID {
		t.Fatalf("waiting drafts = %v, want only the restored one", waiting)
	}
	// Discarded is not a one-way door: the draft and its file stay and come back.
	var discarded draftView
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+first.ID+"/discard", nil, f.tokenA, http.StatusOK, &discarded)
	if discarded.Outcome == nil || *discarded.Outcome != "discarded" || discarded.DecidedBy == nil || *discarded.DecidedBy == "" || len(discarded.Files) != 1 {
		t.Fatalf("discarded = %+v, want who discarded it and its file kept", discarded)
	}
	if got := storedObjects(t, f.root); got != 1 {
		t.Fatalf("a discarded draft lost its stored file: %d objects", got)
	}
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+first.ID+"/restore", nil, f.tokenA, http.StatusOK, &restored)
	if restored.Outcome != nil || len(restored.Files) != 1 {
		t.Fatalf("restored after discard = %+v", restored)
	}
	// Restoring the draft that already waits changes nothing.
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+first.ID+"/restore", nil, f.tokenA, http.StatusOK, &restored)
	// A sent draft cannot be brought back, and neither can a draft the built-in AI wrote.
	kbCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/messages", map[string]any{"kind": "outgoing", "body": "First try.", "ai_draft_id": first.ID},
		f.tokenA, http.StatusCreated, nil)
	if response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+first.ID+"/restore", nil, f.tokenA, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "ai_draft_not_restorable") {
		t.Fatalf("restore a sent draft: %d %s", response.Code, response.Body.String())
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE ai_drafts SET source='ai' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	if response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+second.ID+"/restore", nil, f.tokenA, nil); response.Code != http.StatusConflict {
		t.Fatalf("restore an AI draft: %d %s", response.Code, response.Body.String())
	}
	// Restoring needs ai_assist:create.
	reader := customToken(t, f.api, f.tokenA, "Reader", "conversations:read", "ai_assist:read")
	refused(t, f.api, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+first.ID+"/restore", nil, reader, "ai_assist:create")
}

func waitingDrafts(drafts []draftView) []string {
	var ids []string
	for _, draft := range drafts {
		if draft.Source == "written" && draft.Status == "ready" && draft.Outcome == nil {
			ids = append(ids, draft.ID)
		}
	}
	return ids
}

// No lock-in: a draft's files leave with the export, bytes and all.
func TestExportCarriesADraftsFiles(t *testing.T) {
	f := newAttachFixture(t)
	draft := postDraft(t, f, f.conversationA, f.tokenA, "With a file.", []upload{{"plan.txt", "text/plain", []byte("the plan")}})
	response := request(t, f.api, http.MethodGet, "/api/v1/export", nil, f.tokenA, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export: %d %s", response.Code, response.Body.String())
	}
	files := unzip(t, response.Body.Bytes())
	for _, line := range ndjsonLines(t, files["ai-drafts.ndjson"]) {
		if line["id"] != draft.ID {
			continue
		}
		listed, _ := line["files"].([]any)
		if len(listed) != 1 {
			t.Fatalf("exported draft files = %v", line["files"])
		}
		path, _ := listed[0].(map[string]any)["path"].(string)
		if string(files[path]) != "the plan" {
			t.Fatalf("the archive holds %q at %q", files[path], path)
		}
		return
	}
	t.Fatal("the draft is not in ai-drafts.ndjson")
}

// A draft's files go when the draft can no longer be used: once its text was erased (90 days after it
// ended). An earlier draft keeps them until then, and bytes another row uses stay.
func TestTheSweepRemovesTheFilesOfDraftsThatCannotComeBack(t *testing.T) {
	f := newAttachFixture(t)
	old := postDraft(t, f, f.conversationA, f.tokenA, "Old.", []upload{{"old.txt", "text/plain", []byte("old bytes")}, {"shared.txt", "text/plain", []byte("shared bytes")}})
	kept := postDraft(t, f, f.conversationA, issueToken(t, f.api, f.tokenA, "Other", "draft").Token, "Kept.", []upload{{"shared.txt", "text/plain", []byte("shared bytes")}})
	if _, err := f.pool.Exec(context.Background(), `UPDATE ai_drafts SET outcome='discarded',decided_at=now(),text=NULL,text_cleared_at=now() WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	storage, err := attachments.NewLocalDisk(f.root)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.NewWithDB(database.New(f.pool)).WithObjects(storage).SweepDraftFiles(context.Background())
	if err != nil || removed != 2 {
		t.Fatalf("swept %d files (%v), want the erased draft's two", removed, err)
	}
	if got := queryString(t, f.pool, `SELECT string_agg(draft_id::text, ',') FROM ai_draft_files`); got != kept.ID {
		t.Fatalf("draft file rows left belong to %s, want only the kept draft's", got)
	}
	if got := storedObjects(t, f.root); got != 1 {
		t.Fatalf("stored objects = %d, want the shared file the other draft still holds", got)
	}
}

// A draft's files already count toward storage: sending them adds nothing, so a workspace near its
// cap still sends the draft with its files.
func TestSendingADraftsFilesNearTheStorageCapAddsNothing(t *testing.T) {
	root := t.TempDir()
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) {
		storage, err := attachments.NewLocalDisk(root)
		if err != nil {
			t.Fatal(err)
		}
		config.Attachments = storage
	})
	conversation := *f.receive(t, false, 1, "Invoice").ConversationID
	file := bytes.Repeat([]byte("i"), 400)
	response := serve(f.api, multipartDraft(t, conversation, f.adminA, "Here it is.", []upload{{"invoice.pdf", "application/pdf", file}}))
	if response.Code != http.StatusCreated {
		t.Fatalf("draft: %d %s", response.Code, response.Body.String())
	}
	var draft draftView
	decode(t, response, &draft)
	// Attachments fill all but 500 bytes; with the draft's 400 the workspace is 100 bytes under its cap.
	f.setStorage(t, f.workspaceA, storageCap-500)
	sent := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Here it is.", "ai_draft_id": draft.ID}, f.adminA, nil)
	if sent.Code != http.StatusCreated {
		t.Fatalf("sending the draft's file near the cap: %d %s, want 201", sent.Code, sent.Body.String())
	}
	if got := f.ownStorage(t, f.workspaceA); got != storageCap-100 {
		t.Fatalf("storage after the send = %d, want %d: the file counted once", got, storageCap-100)
	}
}

// Every earlier draft stays reachable: the list comes in pages, newest first, and says where the next
// one starts.
func TestTheDraftListComesInPages(t *testing.T) {
	f := newAttachFixture(t)
	writer := issueToken(t, f.api, f.tokenA, "Writer", "draft").Token
	var ids []string
	for index := range 3 {
		ids = append(ids, postDraft(t, f, f.conversationA, writer, fmt.Sprintf("Try %d.", index), nil).ID)
	}
	type page struct {
		Items      []draftView `json:"items"`
		NextBefore *string     `json:"next_before"`
	}
	base := "/api/v1/conversations/" + f.conversationA + "/ai-drafts"
	var first page
	kbCall(t, f.api, http.MethodGet, base+"?limit=2", nil, f.tokenA, http.StatusOK, &first)
	if len(first.Items) != 2 || first.Items[0].ID != ids[2] || first.Items[1].ID != ids[1] || first.NextBefore == nil {
		t.Fatalf("first page = %+v", first)
	}
	var second page
	kbCall(t, f.api, http.MethodGet, base+"?limit=2&before="+*first.NextBefore, nil, f.tokenA, http.StatusOK, &second)
	if len(second.Items) != 1 || second.Items[0].ID != ids[0] || second.NextBefore != nil {
		t.Fatalf("second page = %+v", second)
	}
	for _, bad := range []string{"?limit=51", "?limit=0", "?before=not-an-id"} {
		if response := request(t, f.api, http.MethodGet, base+bad, nil, f.tokenA, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("GET %s: %d %s, want 400", bad, response.Code, response.Body.String())
		}
	}
	// A draft of another workspace is no place to start a page.
	other := postDraft(t, f, f.conversationB, f.tokenB, "Bravo.", nil)
	if response := request(t, f.api, http.MethodGet, base+"?before="+other.ID, nil, f.tokenA, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("before naming another workspace's draft: %d %s, want 400", response.Code, response.Body.String())
	}
}

// One author has one draft waiting on a conversation, however many writes and restores race.
func TestRacingWritesAndRestoresLeaveOneDraftWaiting(t *testing.T) {
	f := newAttachFixture(t)
	writer := issueToken(t, f.api, f.tokenA, "Writer", "draft").Token
	var earlier []string
	for index := range 4 {
		earlier = append(earlier, postDraft(t, f, f.conversationA, writer, fmt.Sprintf("Try %d.", index), nil).ID)
	}
	for round := range 8 {
		var group sync.WaitGroup
		for _, id := range earlier[:3] {
			group.Add(1)
			go func() {
				defer group.Done()
				serve(f.api, newRequest(t, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/ai-drafts/"+id+"/restore", nil, f.tokenA, nil))
			}()
		}
		group.Add(1)
		go func() {
			defer group.Done()
			serve(f.api, newRequest(t, http.MethodPost, "/api/v1/conversations/"+f.conversationA+"/reply-drafts", map[string]any{"text": fmt.Sprintf("Racing %d.", round)}, writer, nil))
		}()
		group.Wait()
		if waiting := queryString(t, f.pool, `SELECT count(*)::text FROM ai_drafts WHERE source='written' AND status='ready' AND outcome IS NULL AND conversation_id=$1`, f.conversationA); waiting != "1" {
			t.Fatalf("round %d: %s drafts of one author wait, want 1", round, waiting)
		}
	}
}

// A repeat of a send from a draft under the same Idempotency-Key must be the same send: another
// choice of the draft's files is another message.
func TestARepeatedDraftSendWithOtherFilesIsAKeyReused(t *testing.T) {
	f := newAttachFixture(t)
	draft := postDraft(t, f, f.conversationA, f.tokenA, "Files.", []upload{{"a.txt", "text/plain", []byte("one")}, {"b.txt", "text/plain", []byte("two")}})
	path := "/api/v1/conversations/" + f.conversationA + "/messages"
	send := func(files []string, key string) *httptest.ResponseRecorder {
		body := map[string]any{"kind": "outgoing", "body": "Files.", "ai_draft_id": draft.ID}
		if files != nil {
			body["ai_draft_file_ids"] = files
		}
		req := newRequest(t, http.MethodPost, path, body, f.tokenA, nil)
		req.Header.Set("Idempotency-Key", key)
		return serve(f.api, req)
	}
	a := draft.Files[0].ID
	if got := send([]string{a}, "send-1"); got.Code != http.StatusCreated {
		t.Fatalf("first send: %d %s", got.Code, got.Body.String())
	}
	if got := send([]string{a}, "send-1"); got.Code != http.StatusOK {
		t.Fatalf("the same send again: %d %s, want the 200 replay", got.Code, got.Body.String())
	}
	for _, other := range [][]string{{draft.Files[1].ID}, nil, {}} {
		if got := send(other, "send-1"); got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "idempotency_key_reused") {
			t.Fatalf("the key again with files %v: %d %s, want 422", other, got.Code, got.Body.String())
		}
	}
	// A repeat with fewer uploaded files is another message too.
	second := postDraft(t, f, f.conversationA, f.tokenA, "More.", []upload{{"c.txt", "text/plain", []byte("three")}})
	sendWith := func(files []upload) *httptest.ResponseRecorder {
		req := addDraftField(t, multipartMessage(t, f.conversationA, f.tokenA, "outgoing", "More.", files), second.ID)
		req.Header.Set("Idempotency-Key", "send-2")
		return serve(f.api, req)
	}
	both := []upload{{"d.txt", "text/plain", []byte("four")}, {"e.txt", "text/plain", []byte("five")}}
	if got := sendWith(both); got.Code != http.StatusCreated {
		t.Fatalf("send with uploads: %d %s", got.Code, got.Body.String())
	}
	if got := sendWith(both); got.Code != http.StatusOK {
		t.Fatalf("the same uploads again: %d %s, want 200", got.Code, got.Body.String())
	}
	if got := sendWith(both[:1]); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("fewer uploads under the key: %d %s, want 422", got.Code, got.Body.String())
	}
}
