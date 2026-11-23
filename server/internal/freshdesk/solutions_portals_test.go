package freshdesk_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// addCategory adds a category to the fixture with one public folder; title, when set, is one
// published article in it.
func addCategory(fixture map[string]any, id float64, name string, folder float64, folderName string, article float64, title string) {
	stamp, count := "2023-04-01T08:00:00Z", 0.0
	if title != "" {
		count = 1
	}
	fixture["categories"] = append(fixture["categories"].([]any), map[string]any{"id": id, "name": name, "description": nil, "created_at": stamp, "updated_at": stamp})
	folderKey := jsonNumber(folder)
	fixture["category_folders"].(map[string]any)[jsonNumber(id)] = []any{map[string]any{"id": folder, "name": folderName, "description": nil,
		"parent_folder_id": nil, "articles_count": count, "sub_folders_count": 0, "visibility": 1.0, "created_at": stamp, "updated_at": stamp}}
	if title == "" {
		return
	}
	fixture["articles"].(map[string]any)[folderKey] = []any{map[string]any{"id": article, "type": 1.0, "category_id": id, "folder_id": folder, "agent_id": 999.0,
		"title": title, "status": 2.0, "description": "<p>" + title + ".</p>", "description_text": title + ".", "tags": []any{}, "seo_data": map[string]any{},
		"hits": 0.0, "thumbs_up": 0.0, "thumbs_down": 0.0, "created_at": stamp, "updated_at": stamp}}
}

func jsonNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// portalForms are ticket forms that name portals, which is where the API gives a portal its name.
func portalForms(names map[float64]string) []any {
	var portals []any
	for id, name := range names {
		portals = append(portals, map[string]any{"id": id, "name": name})
	}
	return []any{map[string]any{"id": 900.0, "title": "Report an issue", "portals": portals}}
}

// topCollections lists by name the top-level collections help center id shows.
func (e *importEnv) topCollections(t *testing.T, helpCenterID string) string {
	t.Helper()
	collections, err := store.New(e.pool).HelpCenterCollectionsIn(context.Background(), e.workspaceID, helpCenterID, "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, collection := range collections {
		if collection.ParentID == "" {
			names = append(names, collection.Name)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// shownTitles lists by title the articles help center id publishes (its sitemap and llms.txt).
func (e *importEnv) shownTitles(t *testing.T, helpCenterID string) string {
	t.Helper()
	articles, err := store.New(e.pool).HelpCenterIndex(context.Background(), e.workspaceID, helpCenterID)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, article := range articles {
		titles = append(titles, article.Title)
	}
	slices.Sort(titles)
	return strings.Join(titles, ", ")
}

func (e *importEnv) helpCenters(t *testing.T) []domain.HelpCenterSummary {
	t.Helper()
	list, err := store.New(e.pool).ListHelpCenters(context.Background(), e.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestSolutionsOfAnAccountWithOnePortalGoToThePrimaryHelpCenter(t *testing.T) {
	cases := []struct {
		name    string
		portals []any
	}{
		{"no category names a portal", []any{nil, nil}},
		{"every category names the same portal", []any{[]any{5.0}, []any{5.0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newImportEnv(t).withSolutions()
			env.editSolutions(func(fixture map[string]any) {
				setPortals(fixture, tc.portals...)
				fixture["ticket_forms"] = portalForms(map[float64]string{5: "Help desk"})
			})
			report := env.mustRun(t)
			if got := strings.Join(report.KnowledgeBase.Published, ","); got != "101,103,104,105,106" {
				t.Fatalf("published = %s, drafts = %v", got, report.KnowledgeBase.Drafts)
			}
			list := env.helpCenters(t)
			if len(list) != 1 || !list[0].Primary {
				t.Fatalf("help centers = %+v", list)
			}
			if got := env.topCollections(t, ""); got != "Billing, Getting started" {
				t.Fatalf("primary shows %s", got)
			}
			if got := env.shownTitles(t, ""); got != "Read your invoice, Reset your password, Use the API, Webhooks in depth" {
				t.Fatalf("primary publishes %s", got)
			}
		})
	}
}

// twoPortals is an account with two portals: Getting started on both, Billing and Account only on the
// Help desk (6), Hardware only on the Hardware store (5). The Help desk has the most categories.
func twoPortals(fixture map[string]any) {
	addCategory(fixture, 3, "Hardware", 31, "Printers", 301, "Pair the printer")
	addCategory(fixture, 4, "Account", 41, "Profile", 0, "")
	setPortals(fixture, []any{5.0, 6.0}, []any{6.0}, []any{5.0}, []any{6.0})
	fixture["ticket_forms"] = portalForms(map[float64]string{5: "Hardware store", 6: "Help desk"})
}

func TestSolutionsOfTwoPortalsBecomeTwoHelpCentersThatShareACategory(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(twoPortals)
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s failures=%+v", report.Status, report.Failures)
	}
	if got := strings.Join(report.KnowledgeBase.Published, ","); got != "101,103,104,105,106,301" {
		t.Fatalf("published = %s, drafts = %v", got, report.KnowledgeBase.Drafts)
	}
	list := env.helpCenters(t)
	if len(list) != 2 || !list[0].Primary || list[1].Name != "Hardware store" || list[1].Handle == nil || *list[1].Handle != "hardware-store" {
		t.Fatalf("help centers = %+v", list)
	}
	hardware := list[1].ID
	if got := env.topCollections(t, ""); got != "Account, Billing, Getting started" {
		t.Fatalf("primary shows %s", got)
	}
	if got := env.topCollections(t, hardware); got != "Getting started, Hardware" {
		t.Fatalf("hardware store shows %s", got)
	}
	if got := env.shownTitles(t, ""); got != "Read your invoice, Reset your password, Use the API, Webhooks in depth" {
		t.Fatalf("primary publishes %s", got)
	}
	if got := env.shownTitles(t, hardware); got != "Pair the printer, Reset your password, Use the API, Webhooks in depth" {
		t.Fatalf("hardware store publishes %s", got)
	}
}
