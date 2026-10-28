package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

type kbOrderView struct {
	ParentID      *string  `json:"parent_id"`
	CollectionIDs []string `json:"collection_ids"`
	ArticleIDs    []string `json:"article_ids"`
}

type kbOrderRefusal struct {
	Error struct {
		Code          string   `json:"code"`
		CollectionIDs []string `json:"collection_ids"`
		ArticleIDs    []string `json:"article_ids"`
	} `json:"error"`
}

func kbOrderBody(parent any, collections, articles []string) map[string]any {
	return map[string]any{"parent_id": parent, "collection_ids": collections, "article_ids": articles}
}

// pageOrder fails unless every title shows on the page, in the order given.
func pageOrder(t *testing.T, response *httptest.ResponseRecorder, titles ...string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("page status=%d body=%s", response.Code, response.Body.String())
	}
	body, last := response.Body.String(), -1
	for _, title := range titles {
		at := strings.Index(body, ">"+title+"<")
		if at < 0 {
			t.Fatalf("page lacks %q:\n%s", title, body)
		}
		if at < last {
			t.Fatalf("%q shows before the title listed ahead of it; want the order %v", title, titles)
		}
		last = at
	}
}

func TestKnowledgeBaseOrderIsTheOrderOfTheHelpCenterTheCollectionListAndTheReplyPicker(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Order Desk", "admin@order-desk.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Order Answers", true)
	accounts := kbCollection(t, api, admin, map[string]any{"name": "Accounts"})
	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	cards := kbCollection(t, api, admin, map[string]any{"name": "Cards", "parent_id": billing.ID})
	bravo := kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Bravo refunds", "body_md": "b", "status": "published"})
	alpha := kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Alpha invoices", "body_md": "a", "status": "published"})
	charlie := kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Charlie taxes", "body_md": "c", "status": "published"})
	delta := kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Delta draft", "body_md": "d"})
	kbArticle(t, api, admin, map[string]any{"collection_id": accounts.ID, "title": "Aardvark sign-in", "body_md": "s", "status": "published"})
	kbArticle(t, api, admin, map[string]any{"collection_id": cards.ID, "title": "Card fees", "body_md": "f", "status": "published"})
	base := "/help/" + slug

	// Nothing ordered yet: the help center and the collection list keep the title order they always had.
	pageOrder(t, visit(site, base+"/c/billing", "192.0.2.30"), "Alpha invoices", "Bravo refunds", "Charlie taxes")
	if got := kbSearch(t, api, admin, "collection_id="+billing.ID); !slices.Equal(got, []string{"Alpha invoices", "Bravo refunds", "Charlie taxes", "Delta draft"}) {
		t.Fatalf("collection list before ordering = %v", got)
	}
	pageOrder(t, visit(site, base, "192.0.2.30"), "Accounts", "Billing")

	// A list that misses an article (the draft) is stale: nothing changes, and the answer holds the lists as they are.
	stale := request(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(billing.ID, []string{cards.ID}, []string{charlie.ID, alpha.ID, bravo.ID}), admin, nil)
	var refusal kbOrderRefusal
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale order status=%d body=%s, want 409", stale.Code, stale.Body.String())
	}
	decode(t, stale, &refusal)
	if refusal.Error.Code != "order_stale" || !slices.Equal(refusal.Error.ArticleIDs, []string{alpha.ID, bravo.ID, charlie.ID, delta.ID}) ||
		!slices.Equal(refusal.Error.CollectionIDs, []string{cards.ID}) {
		t.Fatalf("stale order refusal = %+v", refusal.Error)
	}
	validateContractResponse(t, newRequest(t, http.MethodPut, "/api/v1/kb/order", kbOrderBody(billing.ID, []string{cards.ID}, []string{charlie.ID}), admin, nil), stale)
	pageOrder(t, visit(site, base+"/c/billing", "192.0.2.30"), "Alpha invoices", "Bravo refunds", "Charlie taxes")

	before := visit(site, base+"/c/billing", "192.0.2.30").Header().Get("ETag")
	var ordered kbOrderView
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(billing.ID, []string{cards.ID}, []string{charlie.ID, delta.ID, alpha.ID, bravo.ID}), admin, http.StatusOK, &ordered)
	if ordered.ParentID == nil || *ordered.ParentID != billing.ID || !slices.Equal(ordered.ArticleIDs, []string{charlie.ID, delta.ID, alpha.ID, bravo.ID}) {
		t.Fatalf("order answer = %+v", ordered)
	}
	after := visit(site, base+"/c/billing", "192.0.2.30")
	if tag := after.Header().Get("ETag"); tag == "" || tag == before {
		t.Fatalf("ETag after reordering = %q, before %q: a cached page would keep the old order", tag, before)
	}
	pageOrder(t, after, "Charlie taxes", "Alpha invoices", "Bravo refunds")
	if got := kbSearch(t, api, admin, "collection_id="+billing.ID); !slices.Equal(got, []string{"Charlie taxes", "Delta draft", "Alpha invoices", "Bravo refunds"}) {
		t.Fatalf("collection list after ordering = %v", got)
	}
	// The article page's neighbours follow the same order.
	pageOrder(t, visit(site, fmt.Sprintf("%s/a/%d-%s", base, bravo.Number, bravo.Slug), "192.0.2.30"), "Charlie taxes", "Alpha invoices")

	// The top level holds collections only.
	if response := request(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(nil, []string{billing.ID, accounts.ID}, []string{alpha.ID}), admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("articles at the top status=%d body=%s, want 400", response.Code, response.Body.String())
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(nil, []string{billing.ID, accounts.ID}, []string{}), admin, http.StatusOK, &ordered)
	pageOrder(t, visit(site, base, "192.0.2.30"), "Billing", "Accounts")
	var collections struct {
		Items []kbCollectionView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, admin, http.StatusOK, &collections)
	if collections.Items[0].ID != billing.ID || collections.Items[1].ID != accounts.ID {
		t.Fatalf("collections after ordering the top = %+v", collections.Items)
	}

	// The reply box's picker lists published articles as the help center does: collection by collection,
	// a collection's own articles before those of its collections.
	want := []string{"Charlie taxes", "Alpha invoices", "Bravo refunds", "Card fees", "Aardvark sign-in"}
	if got := kbSearch(t, api, admin, "status=published&sort=manual"); !slices.Equal(got, want) {
		t.Fatalf("picker order = %v, want %v", got, want)
	}
	// Without sort, all articles still come most recently updated first.
	if got := kbSearch(t, api, admin, "status=published"); got[0] != "Card fees" {
		t.Fatalf("recent order = %v", got)
	}

	// After an order by hand, a new or moved article goes last instead of jumping to the top.
	echo := kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Aaa newest", "body_md": "e", "status": "published"})
	moved := kbArticle(t, api, admin, map[string]any{"collection_id": accounts.ID, "title": "Aab moved", "body_md": "m", "status": "published"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+moved.ID, map[string]any{"updated_at": moved.UpdatedAt, "collection_id": billing.ID}, admin, http.StatusOK, nil)
	if got := kbSearch(t, api, admin, "collection_id="+billing.ID); !slices.Equal(got, []string{"Charlie taxes", "Delta draft", "Alpha invoices", "Bravo refunds", echo.Title, "Aab moved"}) {
		t.Fatalf("collection list after a new and a moved article = %v", got)
	}
	var page struct {
		Items []struct {
			Title    string `json:"title"`
			Position int    `json:"position"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?collection_id="+billing.ID, nil, admin, http.StatusOK, &page)
	if page.Items[0].Position != 1 || page.Items[3].Position != 4 || page.Items[5].Position != 6 {
		t.Fatalf("positions = %+v", page.Items)
	}
}

func TestKnowledgeBaseOrderRefusesBadLists(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Order Checks", "admin@order-checks.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	one := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "One", "body_md": "1"})
	for name, body := range map[string]any{
		"not an id":         kbOrderBody(collection.ID, []string{}, []string{"article-one"}),
		"twice":             kbOrderBody(collection.ID, []string{}, []string{one.ID, one.ID}),
		"bad parent":        kbOrderBody("guides", []string{}, []string{one.ID}),
		"no lists":          map[string]any{"parent_id": collection.ID},
		"articles not list": map[string]any{"parent_id": collection.ID, "collection_ids": []string{}, "article_ids": one.ID},
	} {
		if response := request(t, api, http.MethodPut, "/api/v1/kb/order", body, admin, nil); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s, want 400", name, response.Code, response.Body.String())
		}
	}
}

func TestKnowledgeBaseOrderNeverReordersAnotherWorkspace(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Order Alpha", "admin@order-alpha.example.com")
	adminB := createSession(t, api, service, "Order Bravo", "admin@order-bravo.example.com")
	topA1 := kbCollection(t, api, adminA, map[string]any{"name": "A first"})
	topA2 := kbCollection(t, api, adminA, map[string]any{"name": "A second"})
	firstA := kbArticle(t, api, adminA, map[string]any{"collection_id": topA1.ID, "title": "A one", "body_md": "1"})
	secondA := kbArticle(t, api, adminA, map[string]any{"collection_id": topA1.ID, "title": "A two", "body_md": "2"})
	topB := kbCollection(t, api, adminB, map[string]any{"name": "B only"})
	articleB := kbArticle(t, api, adminB, map[string]any{"collection_id": topB.ID, "title": "B one", "body_md": "1"})

	for name, call := range map[string]struct {
		body map[string]any
		want int
	}{
		"A's collections at B's top":       {kbOrderBody(nil, []string{topA2.ID, topA1.ID}, []string{}), http.StatusConflict},
		"A's articles in B's collection":   {kbOrderBody(topB.ID, []string{}, []string{secondA.ID, firstA.ID}), http.StatusConflict},
		"B's article with one of A's":      {kbOrderBody(topB.ID, []string{}, []string{articleB.ID, firstA.ID}), http.StatusConflict},
		"A's collection as the parent":     {kbOrderBody(topA1.ID, []string{}, []string{secondA.ID, firstA.ID}), http.StatusNotFound},
		"B's top with one of A's on it":    {kbOrderBody(nil, []string{topB.ID, topA1.ID}, []string{}), http.StatusConflict},
		"A's collection, nothing to order": {kbOrderBody(topA1.ID, []string{}, []string{}), http.StatusNotFound},
	} {
		response := request(t, api, http.MethodPut, "/api/v1/kb/order", call.body, adminB, nil)
		if response.Code != call.want {
			t.Fatalf("%s: status=%d body=%s, want %d", name, response.Code, response.Body.String(), call.want)
		}
		if call.want == http.StatusConflict {
			var refusal kbOrderRefusal
			decode(t, response, &refusal)
			if refusal.Error.Code != "order_stale" || slices.Contains(refusal.Error.CollectionIDs, topA1.ID) || slices.Contains(refusal.Error.ArticleIDs, firstA.ID) {
				t.Fatalf("%s: refusal = %+v; it must name only B's own items", name, refusal.Error)
			}
		}
	}
	if got := kbSearch(t, api, adminA, "collection_id="+topA1.ID); !slices.Equal(got, []string{"A one", "A two"}) {
		t.Fatalf("A's articles after B's calls = %v", got)
	}
	var collections struct {
		Items []kbCollectionView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, adminA, http.StatusOK, &collections)
	if collections.Items[0].ID != topA1.ID || collections.Items[1].ID != topA2.ID {
		t.Fatalf("A's collections after B's calls = %+v", collections.Items)
	}
}

func TestKnowledgeBaseOrderTokenTiers(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Order Tokens", "admin@order-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	first := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "First", "body_md": "1"})
	second := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Second", "body_md": "2"})

	filed := accepted(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(collection.ID, []string{}, []string{second.ID, first.ID}), read)
	if got := kbSearch(t, api, admin, "collection_id="+collection.ID); !slices.Equal(got, []string{"First", "Second"}) {
		t.Fatalf("a proposal reordered before approval: %v", got)
	}
	if response := approve(t, api, admin, filed.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve order status=%d body=%s", response.Code, response.Body.String())
	}
	if got := kbSearch(t, api, admin, "collection_id="+collection.ID); !slices.Equal(got, []string{"Second", "First"}) {
		t.Fatalf("order after approval = %v", got)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(collection.ID, []string{}, []string{first.ID, second.ID}), send, http.StatusOK, nil)
	if got := kbSearch(t, api, admin, "collection_id="+collection.ID); !slices.Equal(got, []string{"First", "Second"}) {
		t.Fatalf("order set by a send token = %v", got)
	}
}
