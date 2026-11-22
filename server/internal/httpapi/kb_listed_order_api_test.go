package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// TestAnArticleListedInAnotherCollectionSortsWhereTheHelpCenterListsIt: across a collection and the
// collections inside it (sort=manual), the API lists an article from another home where the help
// center lists it, after the collection's own articles and in that collection's order, not at its
// home's place in its home's order.
func TestAnArticleListedInAnotherCollectionSortsWhereTheHelpCenterListsIt(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Listed Desk", "admin@listed-desk.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Listed Answers", true)
	archive := kbCollection(t, api, admin, map[string]any{"name": "Archive"})
	help := kbCollection(t, api, admin, map[string]any{"name": "Help"})
	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing", "parent_id": help.ID})
	published := func(title string, collections ...string) string {
		body := map[string]any{"collection_ids": collections, "title": title, "body_md": title, "status": "published"}
		return kbArticle(t, api, admin, body).ID
	}
	published("Zulu refunds", billing.ID)
	published("Bravo invoices", billing.ID)
	charlie := published("Charlie cards", archive.ID, billing.ID)
	delta := published("Delta disputes", archive.ID, billing.ID)
	// The home lists them by hand, Delta first; Billing lists its articles by title.
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(archive.ID, []string{}, []string{delta, charlie}), admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+billing.ID,
		map[string]any{"updated_at": billing.UpdatedAt, "article_order": "title"}, admin, http.StatusOK, nil)

	want := []string{"Bravo invoices", "Zulu refunds", "Charlie cards", "Delta disputes"}
	pageOrder(t, visit(site, "/help/"+slug+"/c/help/billing", "192.0.2.151"), want...)
	if got := kbSearch(t, api, admin, "collection_id="+help.ID+"&include_subcollections=true&sort=manual"); !slices.Equal(got, want) {
		t.Fatalf("API list of Help and its collections = %v, want the help center's order %v", got, want)
	}
	// Across the whole knowledge base the two stay at their home, where the help center lists them first.
	all := []string{"Delta disputes", "Charlie cards", "Bravo invoices", "Zulu refunds"}
	if got := kbSearch(t, api, admin, "sort=manual"); !slices.Equal(got, all) {
		t.Fatalf("API list of every article = %v, want %v", got, all)
	}
}
