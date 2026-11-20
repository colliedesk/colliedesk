package mcpserver_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// An AI agent puts a new version of a file under every article that lists it in one call: the file
// keeps its id, the answer says it was replaced in place, and undo brings the earlier version back.
func TestSendTokenReplacesAFileInPlaceAndUndoes(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB File Replace Tools", "admin@kb-file-replace-tools.example.com")
	session := e.connect(w.adminTier, "")

	first, second := pdfsample.PDF("First edition", 2048), pdfsample.PDF("Second edition", 6000)
	oldID := at(t, e.ok(session, "colliedesk_kb_file_upload", map[string]any{"name": "Warranty form.pdf", "data_base64": base64Of(first)}), "media", "id").(string)
	newID := at(t, e.ok(session, "colliedesk_kb_file_upload", map[string]any{"name": "Warranty form v2.pdf", "data_base64": base64Of(second)}), "media", "id").(string)

	replaced := e.ok(session, "colliedesk_kb_media_replace", map[string]any{"media_id": oldID, "with_media_id": newID})
	if at(t, replaced, "in_place") != true || at(t, replaced, "from_media_id") != oldID || len(at(t, replaced, "article_ids").([]any)) != 0 {
		t.Fatalf("replace = %v, want the file replaced in place", replaced)
	}
	got := e.ok(session, "colliedesk_kb_media_get", map[string]any{"media_id": oldID})
	if at(t, got, "media", "size") != float64(len(second)) || at(t, got, "media", "team_text", "name") != "Warranty form v2.pdf" {
		t.Fatalf("replaced file = %v, want the new bytes (%d) under the old id", got, len(second))
	}

	e.ok(session, "colliedesk_kb_media_replace_undo", map[string]any{"replacement_id": at(t, replaced, "replacement_id")})
	if back := e.ok(session, "colliedesk_kb_media_get", map[string]any{"media_id": oldID}); at(t, back, "media", "size") != float64(len(first)) {
		t.Fatalf("after undo the file = %v, want the earlier bytes (%d)", back, len(first))
	}

	reader := e.connect(w.read, "")
	e.refused(reader, "colliedesk_kb_media_replace", map[string]any{"media_id": oldID, "with_media_id": newID})
}
