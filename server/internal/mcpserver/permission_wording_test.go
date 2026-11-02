package mcpserver

import (
	"regexp"
	"testing"
)

// An agent token acts at once with what it holds, or is refused with the permission it lacks: nothing
// waits for a person's approval and there are no tiers. A workspace tool, its guide sentence and the
// server's instructions never tell a model otherwise. Operator tools still file operator requests.
func TestNoWorkspaceToolTalksAboutRequestsOrTiers(t *testing.T) {
	stale := regexp.MustCompile(`(?i)\bagent[ _]requests?\b|request_get|colliedesk_[a-z_]*_request\b|\(Requests\)|\btiers?\b|token_tier|waits? for (a person's )?approval|a person approves|needs? a person's approval`)
	check := func(where, text string) {
		t.Helper()
		if found := stale.FindString(text); found != "" {
			t.Errorf("%s says %q: agent requests and tiers were replaced by roles and permissions", where, found)
		}
	}
	check("instructions", instructions)
	for _, spec := range Catalog() {
		if spec.Operator {
			continue
		}
		check(spec.Name, spec.Name)
		check(spec.Name+" title", spec.Title)
		check(spec.Name+" description", spec.Description)
		check(spec.Name+" doc", spec.Doc)
		check(spec.Name+" input", spec.Input)
		check(spec.Name+" output", spec.Output)
	}
	for _, message := range []string{permissionsMeaning, legacyMeaning} {
		check("whoami permissions_meaning", message)
	}
}
