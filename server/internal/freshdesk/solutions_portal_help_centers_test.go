package freshdesk_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// addWorkspaceHelpCenter is a help center the team made in Colliedesk before the import.
func (e *importEnv) addWorkspaceHelpCenter(t *testing.T, name, handle string) domain.HelpCenterSummary {
	t.Helper()
	created, err := store.New(e.pool).CreateHelpCenter(context.Background(), e.workspaceID, domain.NewHelpCenter{Name: name, Handle: handle, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

// handlesByName is name=handle for every help center but the primary, by handle.
func (e *importEnv) handlesByName(t *testing.T) string {
	t.Helper()
	var pairs []string
	for _, center := range e.helpCenters(t) {
		if !center.Primary && center.Handle != nil {
			pairs = append(pairs, center.Name+"="+*center.Handle)
		}
	}
	return strings.Join(pairs, ", ")
}

// reportedPortals is portal:status for each portal of the report, the primary first.
func reportedPortals(report *freshdesk.Report) string {
	var parts []string
	for _, portal := range report.KnowledgeBase.HelpCenters {
		parts = append(parts, fmt.Sprintf("%d:%s", portal.PortalID, portal.Status))
	}
	return strings.Join(parts, ", ")
}

func TestPortalHelpCenterHandlesAreValidAndUniqueWhateverThePortalIsCalled(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.addWorkspaceHelpCenter(t, "Support desk", "support-desk")
	env.editSolutions(func(fixture map[string]any) {
		addCategory(fixture, 3, "Accounts", 31, "Profile", 0, "")
		addCategory(fixture, 4, "Desk", 41, "Desk", 0, "")
		addCategory(fixture, 5, "Japan", 51, "Japan", 0, "")
		addCategory(fixture, 6, "Routes", 61, "Routes", 0, "")
		addCategory(fixture, 7, "Unnamed", 71, "Unnamed", 0, "")
		setPortals(fixture, []any{9.0}, []any{9.0}, []any{9.0}, []any{5.0}, []any{6.0}, []any{7.0}, []any{8.0})
		fixture["ticket_forms"] = portalForms(map[float64]string{9: "Help desk", 5: "Support desk", 6: "サポート", 7: "Support"})
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	if got := env.handlesByName(t); got != "サポート=portal-6, Portal 8=portal-8, Support desk=support-desk, Support desk=support-desk-2, Support=support-help" {
		t.Fatalf("help centers = %s", got)
	}
	if got := reportedPortals(report); got != "9:primary, 5:created, 6:created, 7:created, 8:created" {
		t.Fatalf("report portals = %s", got)
	}
	unnamed := report.KnowledgeBase.HelpCenters[4]
	if unnamed.PortalName != "Portal 8" || unnamed.NameFromFreshdesk {
		t.Fatalf("unnamed portal = %+v", unnamed)
	}
	if notes := strings.Join(report.KnowledgeBase.Notes, "\n"); !strings.Contains(notes, "Freshdesk gave no name for portal 8") {
		t.Fatalf("notes = %s", notes)
	}
}

func TestUndoTrashesTheHelpCentersTheImportMadeAndKeepsTheOthers(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.addWorkspaceHelpCenter(t, "Partners", "partners")
	env.editSolutions(twoPortals)
	env.mustRun(t)
	if got := env.handlesByName(t); got != "Hardware store=hardware-store, Partners=partners" {
		t.Fatalf("after the import: %s", got)
	}
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.KBHelpCenters != 1 {
		t.Fatalf("help centers removed = %d", result.KBHelpCenters)
	}
	list := env.helpCenters(t)
	if len(list) != 2 || !list[0].Primary || env.handlesByName(t) != "Partners=partners" {
		t.Fatalf("after undo: %+v", list)
	}
	if !strings.Contains(result.Markdown(), "Help centers removed: 1") {
		t.Fatalf("undo report:\n%s", result.Markdown())
	}
	// The handle is free again: a second import makes the help center once more under the same handle.
	env.mustRun(t)
	if got := env.handlesByName(t); got != "Hardware store=hardware-store, Partners=partners" {
		t.Fatalf("after importing again: %s", got)
	}
}

func TestAnImportRunAgainMakesNoSecondHelpCenterForAPortal(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(twoPortals)
	env.mustRun(t)
	// Freshdesk now gives the Hardware store the most categories; the mapping stays as it was.
	env.editSolutions(func(fixture map[string]any) {
		twoPortals(fixture)
		addCategory(fixture, 5, "Scanners", 51, "Scanners", 0, "")
		addCategory(fixture, 6, "Cables", 61, "Cables", 0, "")
		setPortals(fixture, []any{5.0, 6.0}, []any{6.0}, []any{5.0}, []any{6.0}, []any{5.0}, []any{5.0})
	})
	report := env.mustRun(t)
	if got := env.handlesByName(t); got != "Hardware store=hardware-store" {
		t.Fatalf("help centers = %s", got)
	}
	if got := reportedPortals(report); got != "6:primary, 5:existing" {
		t.Fatalf("report portals = %s", got)
	}
	hardware := env.helpCenters(t)[1].ID
	if got := env.topCollections(t, hardware); got != "Cables, Getting started, Hardware, Scanners" {
		t.Fatalf("hardware store shows %s", got)
	}
}

func TestAPortalOverTheHelpCenterLimitIsReportedAndItsCategoriesGoToThePrimary(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	for n := range domain.MaxHelpCenters - 2 {
		env.addWorkspaceHelpCenter(t, fmt.Sprintf("Team %d", n+1), fmt.Sprintf("team-%d", n+1))
	}
	env.editSolutions(func(fixture map[string]any) {
		twoPortals(fixture)
		addCategory(fixture, 5, "Partners", 51, "Partners", 501, "Partner terms")
		setPortals(fixture, []any{5.0, 6.0}, []any{6.0}, []any{5.0}, []any{6.0}, []any{7.0})
		fixture["ticket_forms"] = portalForms(map[float64]string{5: "Hardware store", 6: "Help desk", 7: "Partner portal"})
	})
	report := env.mustRun(t)
	if got := reportedPortals(report); got != "6:primary, 5:created, 7:over_limit" {
		t.Fatalf("report portals = %s", got)
	}
	if len(env.helpCenters(t)) != domain.MaxHelpCenters {
		t.Fatalf("help centers = %d", len(env.helpCenters(t)))
	}
	if got := env.topCollections(t, ""); got != "Account, Billing, Getting started, Partners" {
		t.Fatalf("primary shows %s", got)
	}
	if !strings.Contains(fmt.Sprint(report.Warnings), "kb_portal_over_help_center_limit") {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
}
