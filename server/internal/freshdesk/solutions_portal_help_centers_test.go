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

func TestPreviewNamesTheHelpCenterEachPortalWouldBecome(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(twoPortals)
	preview, err := freshdesk.Preview(t.Context(), env.options())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, portal := range preview.HelpCenters {
		got = append(got, fmt.Sprintf("%d %s %d %s", portal.PortalID, portal.PortalName, portal.Categories, portal.Status))
	}
	if strings.Join(got, "; ") != "6 Help desk 3 primary; 5 Hardware store 2 planned" || !preview.HelpCenters[0].Primary || preview.HelpCenters[1].Primary {
		t.Fatalf("preview help centers = %+v", preview.HelpCenters)
	}
	if n := len(env.helpCenters(t)); n != 1 {
		t.Fatalf("the preview made help centers: %d", n)
	}
}

// Freshdesk names no portal on the categories of an account with one portal.
func TestPreviewOfAnAccountWhoseCategoriesNameNoPortalListsNoHelpCenter(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	preview, err := freshdesk.Preview(t.Context(), env.options())
	if err != nil {
		t.Fatal(err)
	}
	if preview.HelpCenters == nil || len(preview.HelpCenters) != 0 {
		t.Fatalf("preview help centers = %#v", preview.HelpCenters)
	}
}

func TestTheReportSaysWhichHelpCenterEachPortalBecame(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(twoPortals)
	report := env.mustRun(t)
	primary := env.helpCenters(t)[0].Name
	markdown := report.Markdown()
	for _, line := range []string{
		"\n## Help centers made from Freshdesk portals\n",
		"\n- Help desk (Freshdesk portal 6, 3 categories): on the primary help center, " + primary + "\n",
		"\n- Hardware store (Freshdesk portal 5, 2 categories): made the help center Hardware store at /hardware-store\n",
	} {
		if !strings.Contains(markdown, line) {
			t.Fatalf("report lacks %q:\n%s", line, markdown)
		}
	}
	if again := env.mustRun(t).Markdown(); !strings.Contains(again, "\n- Hardware store (Freshdesk portal 5, 2 categories): on the help center Hardware store at /hardware-store, made by an earlier run\n") {
		t.Fatalf("second report:\n%s", again)
	}
}

func TestAPortalNameWithAControlCharacterStillNamesItsHelpCenter(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		twoPortals(fixture)
		fixture["ticket_forms"] = portalForms(map[float64]string{5: "Hardware\u0000 store\t", 6: "Help desk"})
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	if got := env.handlesByName(t); got != "Hardware store=hardware-store" {
		t.Fatalf("help centers = %s", got)
	}
}

func TestAPortalHelpCenterThatCannotBeMadeFailsTheReport(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	if _, err := env.pool.Exec(context.Background(), `ALTER TABLE help_centers ADD CONSTRAINT refuse_hardware_store CHECK (name <> 'Hardware store')`); err != nil {
		t.Fatal(err)
	}
	env.editSolutions(twoPortals)
	report := env.mustRun(t)
	if report.Status == freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	if got := fmt.Sprint(report.Failures); !strings.Contains(got, "kb_help_center 5 ") {
		t.Fatalf("failures = %s", got)
	}
	if got := reportedPortals(report); got != "6:primary, 5:failed" {
		t.Fatalf("report portals = %s", got)
	}
}

func TestAPortalKeepsItsOwnHelpCenterWhenThePrimaryPortalHasNoCategoryLeft(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(twoPortals)
	env.mustRun(t)
	// Freshdesk now shows every category on the Hardware store only, and a new one there.
	env.editSolutions(func(fixture map[string]any) {
		twoPortals(fixture)
		addCategory(fixture, 7, "Returns", 71, "Returns", 701, "Return a printer")
		setPortals(fixture, []any{5.0}, []any{5.0}, []any{5.0}, []any{5.0}, []any{5.0})
	})
	report := env.mustRun(t)
	if got := reportedPortals(report); got != "5:existing" {
		t.Fatalf("report portals = %s", got)
	}
	if got := env.handlesByName(t); got != "Hardware store=hardware-store" {
		t.Fatalf("help centers = %s", got)
	}
	hardware := env.helpCenters(t)[1].ID
	if got := env.topCollections(t, hardware); !strings.Contains(got, "Returns") {
		t.Fatalf("hardware store shows %s", got)
	}
	if got := env.topCollections(t, ""); strings.Contains(got, "Returns") {
		t.Fatalf("primary shows %s", got)
	}
}

func TestACategoryMovedToAnotherPortalKeepsItsArticlesOffThePrimaryHelpCenter(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		twoPortals(fixture)
		setPortals(fixture, []any{5.0, 6.0}, []any{6.0}, []any{6.0}, []any{6.0})
	})
	env.mustRun(t)
	if got := env.shownTitles(t, ""); !strings.Contains(got, "Pair the printer") {
		t.Fatalf("primary publishes %s", got)
	}
	// Freshdesk now shows Hardware on the Hardware store only; one article changed and one is new.
	env.editSolutions(func(fixture map[string]any) {
		twoPortals(fixture)
		article := fixtureArticle(fixture, "31", 301)
		article["description"], article["updated_at"] = "<p>Pair the printer again.</p>", "2024-01-01T08:00:00Z"
		added := map[string]any{}
		for field, value := range article {
			added[field] = value
		}
		added["id"], added["title"], added["description"] = 302.0, "Clean the printer", "<p>Clean the printer.</p>"
		fixture["articles"].(map[string]any)["31"] = append(fixture["articles"].(map[string]any)["31"].([]any), added)
		fixture["category_folders"].(map[string]any)["3"].([]any)[0].(map[string]any)["articles_count"] = 2.0
	})
	report := env.mustRun(t)
	if got := strings.Join(report.KnowledgeBase.Drafts["category_in_another_portal"], ","); got != "301,302" {
		t.Fatalf("drafts = %v, published = %v", report.KnowledgeBase.Drafts, report.KnowledgeBase.Published)
	}
	if got := env.shownTitles(t, ""); strings.Contains(got, "printer") {
		t.Fatalf("primary publishes %s", got)
	}
	if notes := strings.Join(report.KnowledgeBase.Notes, "\n"); !strings.Contains(notes, "Hardware") {
		t.Fatalf("notes = %s", notes)
	}
}

func TestArticlesAnEarlierImportKeptAsDraftsForAnotherPortalArePublishedOnTheirHelpCenter(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(twoPortals)
	env.mustRun(t)
	// An import from before each portal had a help center left the Hardware store's articles drafts.
	if _, err := env.pool.Exec(context.Background(), `UPDATE kb_articles SET status='draft', published_at=NULL,
		source_meta=source_meta||'{"draft_reason":"category_in_another_portal"}' WHERE workspace_id=$1 AND title='Pair the printer'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	if got := strings.Join(report.KnowledgeBase.Published, ","); got != "101,103,104,105,106,301" {
		t.Fatalf("published = %s, drafts = %v", got, report.KnowledgeBase.Drafts)
	}
	if got := env.shownTitles(t, env.helpCenters(t)[1].ID); !strings.Contains(got, "Pair the printer") {
		t.Fatalf("hardware store publishes %s", got)
	}
}
