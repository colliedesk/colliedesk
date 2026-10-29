package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// inlineFile is one entry of a tool's files argument.
func inlineFile(name string, data []byte) map[string]any {
	return map[string]any{"filename": name, "data_base64": base64.StdEncoding.EncodeToString(data)}
}

// storedFiles counts the attachment rows of a workspace and every object in the attachment storage.
func (e *env) storedFiles(w workspace) (rows int, objects int) {
	e.t.Helper()
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM attachments WHERE workspace_id=$1`, w.id).Scan(&rows); err != nil {
		e.t.Fatal(err)
	}
	err := filepath.WalkDir(e.files, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".incoming" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			objects++
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows, objects
}

// wantNoRequests requires that no agent request was filed in the workspace.
func (e *env) wantNoRequests(w workspace, label string) {
	e.t.Helper()
	var count int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_requests WHERE workspace_id=$1`, w.id).Scan(&count); err != nil {
		e.t.Fatal(err)
	}
	if count != 0 {
		e.t.Errorf("%s: %d agent requests filed, want none", label, count)
	}
}

func (e *env) wantNoFilesStored(w workspace, label string) {
	e.t.Helper()
	if rows, objects := e.storedFiles(w); rows != 0 || objects != 0 {
		e.t.Errorf("%s: %d attachment rows and %d stored objects, want none", label, rows, objects)
	}
}

// attachedNames lists the file names of a message in a tool result.
func attachedNames(t *testing.T, out map[string]any) []string {
	t.Helper()
	names := []string{}
	items, _ := at(t, out, "message", "attachments").([]any)
	for _, item := range items {
		names = append(names, at(t, item, "filename").(string))
	}
	return names
}

func TestNoteAddAttachesFilesUnderTheDraftTier(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	steps := []byte("1. Open the tray\n2. Pull the paper")

	// A read token's note is filed for approval, and files cannot wait for one: nothing is stored.
	refused := e.fails(e.connect(f.w.read, ""), "colliedesk_note_add", map[string]any{"conversation_id": f.c1, "body": "See the file",
		"files": []any{inlineFile("steps.txt", steps)}})
	if !strings.Contains(refused, "without files") {
		t.Errorf("a read token's note with files: %q, want the way forward without files", refused)
	}
	e.wantNoFilesStored(f.w, "a read token's note")
	e.wantNoRequests(f.w, "a read token's note with files")

	draft := e.connect(f.w.draft, "")
	if message := e.fails(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.otherConvID, "body": "See the file",
		"files": []any{inlineFile("steps.txt", steps)}}); !strings.Contains(message, "No conversation") {
		t.Errorf("another workspace's conversation: %q", message)
	}
	e.wantNoFilesStored(f.other, "a note on another workspace's conversation")
	e.wantNoFilesStored(f.w, "a note on another workspace's conversation")

	if message := e.fails(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c1, "body": "See the file",
		"files": []any{map[string]any{"filename": "bad.txt", "data_base64": "not base64!"}}}); !strings.Contains(message, "base64") {
		t.Errorf("bad base64: %q", message)
	}
	half := bytes.Repeat([]byte("x"), 5<<19+1) // two of them are one byte over 5 MiB
	if message := e.fails(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c1, "body": "See the files",
		"files": []any{inlineFile("a.log", half), inlineFile("b.log", half[1:]), inlineFile("c.log", []byte("x"))}}); !strings.Contains(message, "5 MiB") {
		t.Errorf("more than 5 MiB in one call: %q", message)
	}
	eleven := []any{}
	for range 11 {
		eleven = append(eleven, inlineFile("a.txt", []byte("a")))
	}
	for label, refusal := range map[string]struct {
		files []any
		want  string
	}{
		"11 files":                {eleven, "Invalid arguments"},
		"a name of control chars": {[]any{inlineFile("\r\n\t", steps)}, "filename"},
		"a name of spaces":        {[]any{inlineFile("   ", steps)}, "filename"},
		"an empty file":           {[]any{map[string]any{"filename": "empty.txt", "data_base64": "    "}}, "empty"},
	} {
		if message := e.fails(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c1, "body": "See the file", "files": refusal.files}); !strings.Contains(message, refusal.want) {
			t.Errorf("%s: %q, want %q", label, message, refusal.want)
		}
	}
	e.wantNoFilesStored(f.w, "refused notes")

	// 5 MiB of files fit in one call: their base64 passes the MCP request limit.
	full := e.ok(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c3, "body": "The full log",
		"files": []any{inlineFile("a.log", half[1:]), inlineFile("b.log", half[1:])}})
	if at(t, full, "message", "attachments", 1, "size") != float64(5<<19) {
		t.Errorf("a 5 MiB note = %v", full)
	}

	out := e.ok(draft, "colliedesk_note_add", map[string]any{"conversation_id": f.c1, "body": "See the files",
		"files": []any{inlineFile("steps.txt\r\nX-Evil: 1", steps), inlineFile("shot.png", kbPNG)}})
	if names := attachedNames(t, out); strings.Join(names, ",") != "steps.txtX-Evil: 1,shot.png" {
		t.Fatalf("note attachments = %v, want steps.txt and shot.png: %v", names, out)
	}
	if at(t, out, "message", "kind") != "note" || at(t, out, "message", "written_by_agent_token", "scope") != "draft" {
		t.Errorf("note = %v", out)
	}
	read := e.ok(e.connect(f.w.read, ""), "colliedesk_attachment_get", map[string]any{"attachment_id": at(t, out, "message", "attachments", 0, "id")})
	if at(t, read, "untrusted_customer_content", "text") != string(steps) {
		t.Errorf("the stored file must hold the bytes sent: %v", read)
	}
}

func TestReplySendAttachesFilesAndNamesTheAIDraft(t *testing.T) {
	e := newEnvWith(t, withSMTP)
	f := seedInbox(t, e)
	if status, body := e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@inbox.example.com"}, f.w.admin); status != http.StatusOK {
		t.Fatalf("connect email: %d %v", status, body)
	}
	if status, body := e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@other.example.com"}, f.other.admin); status != http.StatusOK {
		t.Fatalf("connect email: %d %v", status, body)
	}
	readyDraft := func(w workspace, conversationID, text string) string {
		t.Helper()
		var id string
		if err := e.pool.QueryRow(context.Background(), `INSERT INTO ai_drafts(workspace_id,conversation_id,status,text,ready_at) VALUES($1,$2,'ready',$3,now()) RETURNING id`,
			w.id, conversationID, text).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	invoice := []byte("%PDF-1.7 invoice")
	text := "Your refund is on its way; the invoice is attached."
	ours := readyDraft(f.w, f.c2, text)
	theirs := readyDraft(f.other, f.otherConvID, text)

	// Below send a reply is filed for a person, and files cannot wait for one.
	refused := e.fails(e.connect(f.w.draft, ""), "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": text, "seen_reply_number": 0,
		"files": []any{inlineFile("invoice.pdf", invoice)}})
	if !strings.Contains(refused, "without files") {
		t.Errorf("a draft token's reply with files: %q", refused)
	}
	if message := e.fails(e.connect(f.w.draft, ""), "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": text, "seen_reply_number": 0,
		"ai_draft_id": ours}); !strings.Contains(message, "without ai_draft_id") {
		t.Errorf("a draft token's reply with ai_draft_id: %q", message)
	}
	e.wantNoFilesStored(f.w, "a draft token's reply")
	e.wantNoRequests(f.w, "a draft token's reply with files or ai_draft_id")
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 0 {
		t.Fatalf("refused replies left messages: %v", kinds)
	}

	send := e.connect(f.w.send, "")
	if message := e.fails(send, "colliedesk_reply_send", map[string]any{"conversation_id": f.otherConvID, "body": text, "seen_reply_number": 0,
		"files": []any{inlineFile("invoice.pdf", invoice)}}); !strings.Contains(message, "No conversation") {
		t.Errorf("another workspace's conversation: %q", message)
	}
	e.wantNoFilesStored(f.other, "a reply on another workspace's conversation")
	for _, withFiles := range []bool{false, true} {
		args := map[string]any{"conversation_id": f.c2, "body": text, "seen_reply_number": 0, "ai_draft_id": theirs}
		if withFiles {
			args["files"] = []any{inlineFile("invoice.pdf", invoice)}
		}
		if message := e.fails(send, "colliedesk_reply_send", args); !strings.Contains(message, "AI draft") {
			t.Errorf("another workspace's AI draft (files %v): %q", withFiles, message)
		}
	}
	e.wantNoFilesStored(f.w, "a reply naming another workspace's AI draft")
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 0 {
		t.Fatalf("refused replies left messages: %v", kinds)
	}

	out := e.ok(send, "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": text, "seen_reply_number": 0, "ai_draft_id": ours,
		"cc": []any{"boss@cust.example.com"}, "files": []any{inlineFile("invoice.pdf", invoice)}})
	if names := attachedNames(t, out); strings.Join(names, ",") != "invoice.pdf" || at(t, out, "message", "delivery") != "queued" ||
		at(t, out, "message", "cc", 0) != "boss@cust.example.com" {
		t.Errorf("reply = %v, want a queued reply with the invoice and the cc", out)
	}
	var outcome string
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE(outcome,'') FROM ai_drafts WHERE id=$1`, ours).Scan(&outcome); err != nil || outcome != "sent_unchanged" {
		t.Errorf("the AI draft's outcome = %q (%v), want sent_unchanged", outcome, err)
	}

	// The reply number the agent read still guards a reply with files.
	stale := e.fails(send, "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": "And another thing", "seen_reply_number": 0,
		"files": []any{inlineFile("more.pdf", invoice)}})
	if !strings.Contains(stale, "Nothing was sent") {
		t.Errorf("a stale reply with files: %q", stale)
	}
	if rows, objects := e.storedFiles(f.w); rows != 1 || objects != 1 {
		t.Errorf("attachment rows = %d and stored objects = %d, want only the first reply's", rows, objects)
	}
}

func (e *env) emailChannelID(w workspace) string {
	e.t.Helper()
	_, channels := e.rest(http.MethodGet, "/api/v1/channels", nil, w.admin)
	for _, item := range channels["items"].([]any) {
		if channel := item.(map[string]any); channel["type"] == "email" {
			return channel["id"].(string)
		}
	}
	e.t.Fatal("no email channel")
	return ""
}

func (e *env) conversationCount(w workspace) int {
	e.t.Helper()
	var count int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM conversations WHERE workspace_id=$1`, w.id).Scan(&count); err != nil {
		e.t.Fatal(err)
	}
	return count
}

func TestConversationStartAttachesFiles(t *testing.T) {
	e := newEnvWith(t, withSMTP)
	f := seedInbox(t, e)
	for _, w := range []workspace{f.w, f.other} {
		if status, body := e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@" + w.id[:8] + ".example.com"}, w.admin); status != http.StatusOK {
			t.Fatalf("connect email: %d %v", status, body)
		}
	}
	_, conversation := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c1, nil, f.w.admin)
	grace := conversation["contact_id"].(string)
	_, other := e.rest(http.MethodGet, "/api/v1/conversations/"+f.otherConvID, nil, f.other.admin)
	quote := []byte("%PDF-1.7 quote")
	files := []any{inlineFile("quote.pdf", quote)}
	before := e.conversationCount(f.w)

	// Below send the start is filed for a person, and files cannot wait for one.
	if message := e.fails(e.connect(f.w.draft, ""), "colliedesk_conversation_start", map[string]any{"contact_id": grace, "subject": "Your quote",
		"body": "Here it is.", "files": files}); !strings.Contains(message, "without files") {
		t.Errorf("a draft token's start with files: %q", message)
	}
	e.wantNoRequests(f.w, "a draft token's start with files")
	send := e.connect(f.w.send, "")
	for label, args := range map[string]map[string]any{
		"another workspace's customer": {"contact_id": other["contact_id"], "subject": "Your quote", "body": "Here it is.", "files": files},
		"another workspace's channel":  {"contact_id": grace, "channel_id": e.emailChannelID(f.other), "subject": "Your quote", "body": "Here it is.", "files": files},
	} {
		if message := e.fails(send, "colliedesk_conversation_start", args); !strings.HasPrefix(message, "No contact") {
			t.Errorf("%s: %q", label, message)
		} else if args["channel_id"] != nil && !strings.Contains(message, "email channel") {
			t.Errorf("%s must name the channel: %q", label, message)
		}
	}
	e.wantNoFilesStored(f.w, "refused starts")
	e.wantNoFilesStored(f.other, "refused starts")
	if got := e.conversationCount(f.w); got != before {
		t.Fatalf("refused starts opened %d conversations", got-before)
	}

	out := e.ok(send, "colliedesk_conversation_start", map[string]any{"contact_id": grace, "channel_id": e.emailChannelID(f.w), "subject": "Your quote",
		"body": "Here it is.", "cc": []any{"boss@cust.example.com"}, "files": files})
	if names := attachedNames(t, out); strings.Join(names, ",") != "quote.pdf" || at(t, out, "message", "delivery") != "queued" {
		t.Errorf("start = %v, want the first reply queued with the quote", out)
	}
	added := e.ok(send, "colliedesk_conversation_start", map[string]any{"new_contact": map[string]any{"name": "Nia New", "email": "nia@new.example"},
		"subject": "Welcome", "body": "Hello Nia.", "files": []any{inlineFile("guide.txt", []byte("Welcome guide"))}})
	if names := attachedNames(t, added); strings.Join(names, ",") != "guide.txt" {
		t.Errorf("start with a new customer = %v", added)
	}
	if rows, _ := e.storedFiles(f.w); rows != 2 {
		t.Errorf("attachment rows = %d, want 2", rows)
	}
}
