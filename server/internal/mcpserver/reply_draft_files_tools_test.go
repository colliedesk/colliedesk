package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

// An AI agent leaves a draft with files for a person; nothing reaches the customer, the files are read
// back by the draft's file id, and a reply sent from the draft carries the files it keeps.
func TestReplyDraftCarriesFilesAndTheReplyKeepsThem(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	draft := e.connect(f.w.draft, "")
	out := e.ok(draft, "colliedesk_reply_draft", map[string]any{"conversation_id": f.c2, "body": "The steps are attached.",
		"files": []any{inlineFile("steps.txt", []byte("1. Open the tray")), inlineFile("photo.txt", []byte("not needed"))}})
	files, _ := at(t, out, "draft", "files").([]any)
	if len(files) != 2 || at(t, files[0], "filename") != "steps.txt" {
		t.Fatalf("draft files = %v", at(t, out, "draft"))
	}
	draftID := at(t, out, "draft", "id").(string)
	steps := at(t, files[0], "id").(string)
	if rows, _ := e.storedFiles(f.w); rows != 0 {
		t.Fatalf("a draft wrote %d attachment rows", rows)
	}
	// The file reads back like an attachment, by its draft.
	read := e.ok(draft, "colliedesk_attachment_get", map[string]any{"attachment_id": steps, "conversation_id": f.c2, "draft_id": draftID})
	if at(t, read, "untrusted_customer_content", "text") != "1. Open the tray" {
		t.Fatalf("draft file read = %v", read)
	}
	// Another workspace reads neither the draft nor its file.
	other := e.connect(f.other.draft, "")
	if message := e.fails(other, "colliedesk_attachment_get", map[string]any{"attachment_id": steps, "conversation_id": f.c2, "draft_id": draftID}); !strings.Contains(message, "No file") {
		t.Fatalf("another workspace reads the draft file: %s", message)
	}
	// The list names the files, who wrote the draft and whether it waits.
	listed := e.ok(draft, "colliedesk_ai_drafts_list", map[string]any{"conversation_id": f.c2})
	first := at(t, listed, "drafts", 0)
	if at(t, first, "source") != "written" || at(t, first, "written_by", "agent_name") == "" || len(at(t, first, "files").([]any)) != 2 {
		t.Fatalf("listed draft = %v", first)
	}
	// A reply sent from the draft keeps only the file named.
	send := e.connect(f.w.send, "")
	sent := e.ok(send, "colliedesk_reply_send", map[string]any{"conversation_id": f.c2, "body": "The steps are attached.", "seen_reply_number": 0,
		"ai_draft_id": draftID, "ai_draft_file_ids": []any{steps}})
	if names := attachedNames(t, sent); len(names) != 1 || names[0] != "steps.txt" {
		t.Fatalf("sent files = %v, want steps.txt only", names)
	}
}

// The earlier drafts of a token stay; an agent reads them and brings one back, with its files.
func TestAnAgentBringsAnEarlierDraftBack(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	draft := e.connect(f.w.draft, "")
	first := e.ok(draft, "colliedesk_reply_draft", map[string]any{"conversation_id": f.c2, "body": "First words.",
		"files": []any{inlineFile("first.txt", []byte("first"))}})
	firstID := at(t, first, "draft", "id").(string)
	e.ok(draft, "colliedesk_reply_draft", map[string]any{"conversation_id": f.c2, "body": "Second words."})
	got := e.ok(draft, "colliedesk_ai_draft_get", map[string]any{"conversation_id": f.c2, "draft_id": firstID})
	if at(t, got, "draft", "outcome") != "superseded" || at(t, got, "draft", "text") != "First words." {
		t.Fatalf("the earlier draft = %v", got)
	}
	// The list comes in pages: next_before starts the next one.
	page := e.ok(draft, "colliedesk_ai_drafts_list", map[string]any{"conversation_id": f.c2, "limit": 1})
	if len(at(t, page, "drafts").([]any)) != 1 || page["next_before"] == nil {
		t.Fatalf("first page = %v", page)
	}
	older := e.ok(draft, "colliedesk_ai_drafts_list", map[string]any{"conversation_id": f.c2, "limit": 1, "before": page["next_before"]})
	if at(t, older, "drafts", 0, "id") != firstID || older["next_before"] != nil {
		t.Fatalf("second page = %v, want the first draft and no further page", older)
	}
	refusedWithoutPermission(t, e, f.w.read, "colliedesk_reply_draft_restore", map[string]any{"conversation_id": f.c2, "draft_id": firstID})
	restored := e.ok(draft, "colliedesk_reply_draft_restore", map[string]any{"conversation_id": f.c2, "draft_id": firstID})
	if at(t, restored, "draft", "outcome") != nil || len(at(t, restored, "draft", "files").([]any)) != 1 || !strings.Contains(restored["next"].(string), "above the reply box") {
		t.Fatalf("restored = %v", restored)
	}
	// Another workspace cannot bring it back.
	if message := e.fails(e.connect(f.other.draft, ""), "colliedesk_reply_draft_restore", map[string]any{"conversation_id": f.c2, "draft_id": firstID}); !strings.Contains(message, "No") {
		t.Fatalf("another workspace restores: %s", message)
	}
	status, page := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c2+"/ai-drafts", nil, f.w.admin)
	if status != http.StatusOK || strings.Count(stringify(page), `"outcome":null`) != 1 {
		t.Fatalf("drafts after restore: %d %v, want one waiting", status, page)
	}
}
