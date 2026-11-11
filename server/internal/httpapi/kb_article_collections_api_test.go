package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// kbListedArticle is an article answer with the collections it is listed in, its home first.
type kbListedArticle struct {
	ID            string   `json:"id"`
	Number        int64    `json:"number"`
	CollectionID  string   `json:"collection_id"`
	CollectionIDs []string `json:"collection_ids"`
	Slug          string   `json:"slug"`
	URL           string   `json:"url"`
	UpdatedAt     string   `json:"updated_at"`
}

func patchKBCollections(t *testing.T, api http.Handler, token string, article kbListedArticle, body map[string]any) kbListedArticle {
	t.Helper()
	body["updated_at"] = article.UpdatedAt
	var updated kbListedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, body, token, http.StatusOK, &updated)
	return updated
}

// One article, two collections: the help center lists it in both, at one address; search, the sitemap
// and llms.txt name it once; taking the second collection away keeps the article and its address.
func TestAnArticleListedInTwoCollectionsShowsInBothAtOneAddress(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Listed Twice", "admin@listed-twice.example.com")
	enableHelpCenter(t, api, admin, "Listed Answers", true)
	base := "/help/" + workspaceSlug(t, api, admin)

	billing := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	bookings := kbCollection(t, api, admin, map[string]any{"name": "Bookings"})
	kbArticle(t, api, admin, map[string]any{"collection_id": billing.ID, "title": "Change the card you pay with", "status": "published", "body_md": "Cards."})
	kbArticle(t, api, admin, map[string]any{"collection_id": bookings.ID, "title": "Cancel a booking", "status": "published", "body_md": "Cancel."})

	var created kbListedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_ids": []string{billing.ID, bookings.ID},
		"title": "Get a VAT invoice", "status": "published", "body_md": "Ask within 30 days: quillwort."}, admin, http.StatusCreated, &created)
	if created.CollectionID != billing.ID || !slices.Equal(created.CollectionIDs, []string{billing.ID, bookings.ID}) {
		t.Fatalf("created collection_id=%s collection_ids=%v, want home %s then %s", created.CollectionID, created.CollectionIDs, billing.ID, bookings.ID)
	}
	var read kbListedArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+created.ID, nil, admin, http.StatusOK, &read)
	if !slices.Equal(read.CollectionIDs, created.CollectionIDs) {
		t.Fatalf("read collection_ids=%v, want %v", read.CollectionIDs, created.CollectionIDs)
	}

	articlePath := fmt.Sprintf("%s/a/%d-%s", base, created.Number, created.Slug)
	link := `href="` + articlePath + `"`
	home := visit(site, base, "192.0.2.30")
	if got := strings.Count(home.Body.String(), link); home.Code != http.StatusOK || got != 2 {
		t.Fatalf("home status=%d lists the article %d times, want 2 (Billing and Bookings):\n%s", home.Code, got, home.Body.String())
	}
	wantPage(t, visit(site, base+"/c/bookings", "192.0.2.30"), http.StatusOK, []string{link, "Cancel a booking"}, nil)
	wantPage(t, visit(site, base+"/c/billing", "192.0.2.30"), http.StatusOK, []string{link, "Change the card you pay with"}, nil)
	// The article's own page keeps its home: the breadcrumb leads to Billing.
	wantPage(t, visit(site, articlePath, "192.0.2.30"), http.StatusOK, []string{`href="` + base + `/c/billing"`}, nil)
	wantPage(t, visit(site, base+"/search?q=quillwort", "192.0.2.30"), http.StatusOK, []string{"1 result"}, nil)
	for _, file := range []string{"/sitemap.xml", "/llms.txt"} {
		body := visit(site, base+file, "192.0.2.30").Body.String()
		if got := strings.Count(body, fmt.Sprintf("/a/%d-%s", created.Number, created.Slug)); got != 1 {
			t.Errorf("%s names the article %d times, want 1:\n%s", file, got, body)
		}
	}

	// Taking Bookings away keeps the article, its number and its address.
	kept := patchKBCollections(t, api, admin, read, map[string]any{"collection_ids": []string{billing.ID}})
	if kept.CollectionID != billing.ID || !slices.Equal(kept.CollectionIDs, []string{billing.ID}) || kept.Number != created.Number || kept.URL != created.URL {
		t.Fatalf("after removing Bookings: %+v, want only Billing and the URL %s", kept, created.URL)
	}
	wantPage(t, visit(site, base+"/c/bookings", "192.0.2.31"), http.StatusOK, []string{"Cancel a booking"}, []string{link})
	wantPage(t, visit(site, articlePath, "192.0.2.31"), http.StatusOK, []string{"Get a VAT invoice"}, nil)

	// Moving the home with collection_id alone keeps the other collections, except the new home itself.
	again := patchKBCollections(t, api, admin, kept, map[string]any{"collection_ids": []string{billing.ID, bookings.ID}})
	moved := patchKBCollections(t, api, admin, again, map[string]any{"collection_id": bookings.ID})
	if moved.CollectionID != bookings.ID || !slices.Equal(moved.CollectionIDs, []string{bookings.ID}) || moved.URL != created.URL {
		t.Fatalf("after moving home to Bookings: collection_id=%s collection_ids=%v url=%s", moved.CollectionID, moved.CollectionIDs, moved.URL)
	}

	// What cannot be stored is refused and says how to fix it.
	for _, bad := range []map[string]any{
		{"collection_ids": []string{}},
		{"collection_ids": []string{bookings.ID, bookings.ID}},
		{"collection_ids": []string{billing.ID}, "collection_id": bookings.ID},
		{"collection_ids": []string{bookings.ID, "not-an-id"}},
	} {
		bad["updated_at"] = moved.UpdatedAt
		response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+moved.ID, bad, admin, nil)
		if response.Code != http.StatusBadRequest || errorCode(t, response) != "invalid_request" {
			t.Errorf("PATCH %v status=%d body=%s, want 400 invalid_request", bad, response.Code, response.Body.String())
		}
	}
}

// An extra collection belongs to the article's workspace; one in the trash is not a place either.
// Deleting a collection an article is only listed in keeps the article; restoring it lists it again.
// A home collection with articles still cannot be deleted.
func TestExtraCollectionsStayInTheWorkspaceAndSurviveTheTrash(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "Extra Alpha", "admin@extra-alpha.example.com")
	beta := createSession(t, api, service, "Extra Beta", "admin@extra-beta.example.com")
	alphaShelf := kbCollection(t, api, alpha, map[string]any{"name": "Alpha shelf"})
	betaHome := kbCollection(t, api, beta, map[string]any{"name": "Beta home"})
	betaExtra := kbCollection(t, api, beta, map[string]any{"name": "Beta extra"})

	refused := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_ids": []string{betaHome.ID, alphaShelf.ID},
		"title": "Smuggled", "body_md": "x"}, beta, http.StatusBadRequest, nil)
	if !strings.Contains(refused.Body.String(), alphaShelf.ID) {
		t.Errorf("refusal does not name the collection it cannot use: %s", refused.Body.String())
	}
	var article kbListedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_ids": []string{betaHome.ID, betaExtra.ID},
		"title": "Shared answer", "body_md": "x"}, beta, http.StatusCreated, &article)
	body := map[string]any{"updated_at": article.UpdatedAt, "collection_ids": []string{betaHome.ID, alphaShelf.ID}}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, body, beta, http.StatusBadRequest, nil)
	// Alpha cannot list beta's article in its own collection either: the article is not found.
	if response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID,
		map[string]any{"updated_at": article.UpdatedAt, "collection_ids": []string{alphaShelf.ID}}, alpha, nil); response.Code != http.StatusNotFound {
		t.Fatalf("alpha PATCH beta's article status=%d, want 404", response.Code)
	}

	// The extra collection goes to the trash: the article stays, listed in its home only, and comes back.
	extra := betaExtra
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+extra.ID+"?updated_at="+url.QueryEscape(extra.UpdatedAt), nil, beta, http.StatusNoContent, nil)
	var read kbListedArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, beta, http.StatusOK, &read)
	if !slices.Equal(read.CollectionIDs, []string{betaHome.ID}) {
		t.Fatalf("with the extra collection in the trash collection_ids=%v, want only the home", read.CollectionIDs)
	}
	// A collection in the trash is not a place for an article.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID,
		map[string]any{"updated_at": read.UpdatedAt, "collection_ids": []string{betaHome.ID, extra.ID}}, beta, http.StatusBadRequest, nil)
	var trash struct {
		Items []kbCollectionView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections/trash", nil, beta, http.StatusOK, &trash)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections/"+extra.ID+"/restore", map[string]any{"updated_at": trash.Items[0].UpdatedAt}, beta, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, beta, http.StatusOK, &read)
	if !slices.Equal(read.CollectionIDs, []string{betaHome.ID, betaExtra.ID}) {
		t.Fatalf("after restoring the extra collection collection_ids=%v, want home then extra", read.CollectionIDs)
	}

	// The export carries every collection the article is listed in, its home first.
	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, beta, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	records := ndjsonLines(t, unzip(t, exported.Body.Bytes())["kb-articles.ndjson"])
	if ids, _ := records[0]["collection_ids"].([]any); len(records) != 1 || len(ids) != 2 || ids[0] != betaHome.ID || ids[1] != betaExtra.ID {
		t.Fatalf("kb-articles.ndjson collection_ids = %v, want home then extra", records)
	}

	// The home collection still holds the article: deleting it is refused, as before.
	var collections struct {
		Items []kbCollectionView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, beta, http.StatusOK, &collections)
	for _, collection := range collections.Items {
		if collection.ID == betaHome.ID {
			response := kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+collection.ID+"?updated_at="+url.QueryEscape(collection.UpdatedAt), nil, beta, http.StatusConflict, nil)
			if code := errorCode(t, response); code != "collection_not_empty" {
				t.Fatalf("delete home collection error=%s, want collection_not_empty", code)
			}
		}
	}
}
