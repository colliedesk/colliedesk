package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestKBEditorToolStoresTheChoiceOfTheTokensMember(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Editor Tools Desk", "admin@editor-tools.example.com")
	other := e.newWorkspace("Other Editor Desk", "admin@other-editor.example.com")

	draft := e.connect(w.draft, "")
	if chosen := e.ok(draft, "colliedesk_my_kb_editor_set", map[string]any{"editor": "markdown"}); chosen["editor"] != "markdown" {
		t.Fatalf("choose markdown = %v", chosen)
	}
	_, me := e.rest(http.MethodGet, "/api/v1/auth/me", nil, w.admin)
	if me["kb_editor"] != "markdown" {
		t.Fatalf("auth/me after choosing markdown = %v", me["kb_editor"])
	}
	_, theirs := e.rest(http.MethodGet, "/api/v1/auth/me", nil, other.admin)
	if theirs["kb_editor"] != nil {
		t.Fatalf("another workspace's member got a choice: %v", theirs["kb_editor"])
	}
	if message := e.fails(draft, "colliedesk_my_kb_editor_set", map[string]any{"editor": "word"}); !strings.Contains(message, "visual") {
		t.Errorf("an unknown editor: %q", message)
	}
	refusedWithoutPermission(t, e, w.read, "colliedesk_my_kb_editor_set", map[string]any{"editor": "visual"})
}
