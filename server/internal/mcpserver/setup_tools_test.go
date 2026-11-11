package mcpserver_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
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
	// A read token may not change the targets: no suggestion is read for it. The Admin tier gets one, with the
	// hours a preset applies with.
	if out["reply_target_suggestion"] != nil {
		t.Fatalf("read token suggestion = %v, want null", out["reply_target_suggestion"])
	}
	admin := e.ok(e.connect(w.adminTier, ""), "colliedesk_setup_progress_get", nil)
	if at(t, admin, "reply_target_suggestion", "preset") != "same_day" || at(t, admin, "reply_target_suggestion", "basis") != "no_history" {
		t.Fatalf("suggestion = %v", admin["reply_target_suggestion"])
	}
	if at(t, admin, "reply_target_hours", "kept") != false || at(t, admin, "reply_target_hours", "day_minutes") != float64(540) ||
		at(t, admin, "reply_target_hours", "hours", 0, "from") != "09:00" {
		t.Fatalf("hours = %v", admin["reply_target_hours"])
	}
	if at(t, out, "reply_target_presets", 1, "name") != "Same day" || at(t, out, "reply_target_presets", 1, "targets", "normal", "first_response_minutes") != float64(480) {
		t.Fatalf("presets = %v", out["reply_target_presets"])
	}
}

// The description says how to finish each new step in one call: which slot source to send, and which hours.
func TestSetupProgressToolSaysHowToTurnOnDraftsAndWhichHoursToKeep(t *testing.T) {
	for _, spec := range mcpserver.Catalog() {
		if spec.Name != "colliedesk_setup_progress_get" {
			continue
		}
		for _, want := range []string{"source server", "source provider with provider_id", "reply_target_hours", "kept"} {
			if !strings.Contains(spec.Description, want) {
				t.Errorf("description lacks %q: %s", want, spec.Description)
			}
		}
		return
	}
	t.Fatal("colliedesk_setup_progress_get is not in the catalog")
}
