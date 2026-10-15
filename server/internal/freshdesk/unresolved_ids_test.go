package freshdesk_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

func TestRefusedArchiveListsEveryIDThatAnswered404(t *testing.T) {
	env := newImportEnv(t)
	env.fake.archiveForbidden = true
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusUnverified {
		t.Fatalf("status = %s, want UNVERIFIED while the archive cannot be checked", report.Status)
	}
	// Ticket 3 sits below the highest existing id (6) and is missing from the list and from
	// /tickets; with the archive refused nothing can say whether it was archived or deleted.
	if want := []string{"3"}; !reflect.DeepEqual(report.Tickets.UnresolvedIDs, want) {
		t.Fatalf("unresolved ids = %v, want %v", report.Tickets.UnresolvedIDs, want)
	}
	markdown := report.Markdown()
	if !strings.Contains(markdown, "Ticket ids that could not be resolved (archive unavailable)") || !strings.Contains(markdown, "- 3\n") {
		t.Fatalf("markdown does not list the unresolved id:\n%s", markdown)
	}
	if strings.Contains(markdown, "do not exist in Freshdesk: ") {
		t.Fatal("markdown claims 0 absent ids while ids are unresolved")
	}
}

func TestReachableArchiveLeavesNoUnresolvedIDs(t *testing.T) {
	env := newImportEnv(t)
	report := env.mustRun(t)
	if report.Tickets.UnresolvedIDs == nil || len(report.Tickets.UnresolvedIDs) != 0 {
		t.Fatalf("unresolved ids = %#v, want an empty list", report.Tickets.UnresolvedIDs)
	}
	if strings.Contains(report.Markdown(), "could not be resolved") {
		t.Fatal("markdown has an unresolved section although the archive was reachable")
	}
}
