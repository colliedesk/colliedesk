package mcpserver_test

import (
	"slices"
	"strings"
	"testing"
)

// An agent turns on conversations from not-helpful votes and reads the setting back; a token that may
// change the help center but not the channels is refused, and the setting belongs to the whole
// knowledge base, so naming one help center is refused with how to fix it.
func TestHelpCenterUpdateTurnsOnConversationsFromFeedback(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Feedback Tools", "admin@feedback-tools.example.com")
	admin := e.connect(w.adminTier, "")
	turnOn := map[string]any{"enabled": true, "title": "", "locale": "en", "feedback_conversations": true}

	if got := at(t, e.ok(admin, "colliedesk_help_center_get", nil), "help_center", "feedback_conversations"); got != false {
		t.Fatalf("feedback_conversations of a new workspace = %v, want false", got)
	}
	updated := e.ok(admin, "colliedesk_help_center_update", turnOn)
	e.requireMatchesOutputSchema(admin, "colliedesk_help_center_update", updated)
	if got := at(t, updated, "help_center", "feedback_conversations"); got != true {
		t.Fatalf("feedback_conversations after the update = %v, want true", got)
	}
	read := e.ok(admin, "colliedesk_help_center_get", nil)
	e.requireMatchesOutputSchema(admin, "colliedesk_help_center_get", read)
	if got := at(t, read, "help_center", "feedback_conversations"); got != true {
		t.Fatalf("feedback_conversations read back = %v, want true", got)
	}

	editor := e.token(w.admin, "help center editor", "", append(slices.Clone(sendPermissions), "help_center:update"))
	if message := e.fails(e.connect(editor, ""), "colliedesk_help_center_update", turnOn); !strings.Contains(message, "channels:update") {
		t.Errorf("feedback_conversations without channels:update: %q, want a refusal naming channels:update", message)
	}

	created := e.ok(admin, "colliedesk_help_center_create", map[string]any{"name": "Hardware", "handle": "hardware", "locale": "en"})
	hardware, _ := at(t, created, "help_center", "id").(string)
	perCenter := map[string]any{"help_center_id": hardware, "enabled": true, "title": "", "locale": "en", "feedback_conversations": false}
	if message := e.fails(admin, "colliedesk_help_center_update", perCenter); !strings.Contains(message, "feedback_conversations") {
		t.Errorf("feedback_conversations on one help center: %q, want a refusal naming feedback_conversations", message)
	}
}
