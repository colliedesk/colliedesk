package mcpserver_test

import (
	"slices"
	"testing"
)

// An agent reads the whole setup in one call: every step by its name, and the reply targets presets with the
// one that fits, ready for colliedesk_response_targets_set.
func TestSetupProgressToolNamesEveryStepAndOffersTheReplyTargetsPresets(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Setup Tool Desk", "admin@setup-tool.example.com")
	session := e.connect(w.read, "")
	out := e.ok(session, "colliedesk_setup_progress_get", nil)
	var keys, names []string
	for _, step := range out["steps"].([]any) {
		keys = append(keys, at(t, step, "key").(string))
		names = append(names, at(t, step, "name").(string))
	}
	if want := []string{"email", "import", "ai_drafts", "reply_targets", "invite", "first_reply"}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if want := []string{"Receive customer email", "Import from Freshdesk", "Turn on AI drafts", "Set reply targets", "Invite your team", "Answer your first email"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if at(t, out, "reply_target_suggestion", "preset") != "same_day" || at(t, out, "reply_target_suggestion", "basis") != "no_history" {
		t.Fatalf("suggestion = %v", out["reply_target_suggestion"])
	}
	if at(t, out, "reply_target_presets", 1, "name") != "Same day" || at(t, out, "reply_target_presets", 1, "targets", "normal", "first_response_minutes") != float64(480) {
		t.Fatalf("presets = %v", out["reply_target_presets"])
	}
}
