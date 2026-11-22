package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// TestACollectionListsItsArticlesInTheOrderItIsSetTo: a collection ordered by hand and then set to
// title order lists its articles A to Z in the help center and the API; set back to manual, the hand
// order comes back as it was, because the positions stay stored.
func TestACollectionListsItsArticlesInTheOrderItIsSetTo(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Sorted Desk", "admin@sorted-desk.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Sorted Answers", true)
	guides := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	charlie := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Charlie exports", "body_md": "c", "status": "published"})
	alpha := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Alpha accounts", "body_md": "a", "status": "published"})
	bravo := kbArticle(t, api, admin, map[string]any{"collection_id": guides.ID, "title": "Bravo billing", "body_md": "b", "status": "published"})
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(guides.ID, []string{}, []string{bravo.ID, charlie.ID, alpha.ID}), admin, http.StatusOK, nil)
	page := "/help/" + slug + "/c/guides"
	hand := []string{"Bravo billing", "Charlie exports", "Alpha accounts"}
	pageOrder(t, visit(site, page, "192.0.2.150"), hand...)

	type orderedCollection struct {
		UpdatedAt    string `json:"updated_at"`
		ArticleOrder string `json:"article_order"`
	}
	var list struct {
		Items []orderedCollection `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, admin, http.StatusOK, &list)
	current := list.Items[0]
	if current.ArticleOrder != "manual" {
		t.Fatalf("a new collection's article_order = %q, want manual", current.ArticleOrder)
	}
	if bad := request(t, api, http.MethodPatch, "/api/v1/kb/collections/"+guides.ID, map[string]any{"updated_at": current.UpdatedAt, "article_order": "random"}, admin, nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("article_order random status=%d body=%s, want 400", bad.Code, bad.Body.String())
	}

	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+guides.ID, map[string]any{"updated_at": current.UpdatedAt, "article_order": "title"}, admin, http.StatusOK, &current)
	if current.ArticleOrder != "title" {
		t.Fatalf("article_order after the change = %q, want title", current.ArticleOrder)
	}
	az := []string{"Alpha accounts", "Bravo billing", "Charlie exports"}
	pageOrder(t, visit(site, page, "192.0.2.150"), az...)
	if got := kbSearch(t, api, admin, "collection_id="+guides.ID); !slices.Equal(got, az) {
		t.Fatalf("API list in title order = %v, want %v", got, az)
	}
	if got := kbSearch(t, api, admin, "status=published&sort=manual"); !slices.Equal(got, az) {
		t.Fatalf("picker in title order = %v, want %v", got, az)
	}

	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+guides.ID, map[string]any{"updated_at": current.UpdatedAt, "article_order": "manual"}, admin, http.StatusOK, &current)
	pageOrder(t, visit(site, page, "192.0.2.150"), hand...)
	if got := kbSearch(t, api, admin, "collection_id="+guides.ID); !slices.Equal(got, hand) {
		t.Fatalf("API list back in manual order = %v, want %v", got, hand)
	}
}
