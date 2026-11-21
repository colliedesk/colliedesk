package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
)

type areaView struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Area      string         `json:"area"`
	Scope     string         `json:"scope"`
	Filters   map[string]any `json:"filters"`
	UpdatedAt string         `json:"updated_at"`
}

func areaViews(t *testing.T, api http.Handler, token, path string) []string {
	t.Helper()
	var list struct {
		Items []areaView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, path, nil, token, http.StatusOK, &list)
	names := []string{}
	for _, view := range list.Items {
		names = append(names, view.Area+":"+view.Name)
	}
	return names
}

// kbViewQuery writes the filters of a knowledge base view as the query of GET /api/v1/kb/articles,
// as the web app and the MCP server open one: tag repeats, author joins with commas.
func kbViewQuery(filters map[string]any) string {
	query := url.Values{}
	for key, value := range filters {
		list, isList := value.([]any)
		switch {
		case isList && key == "tag":
			for _, item := range list {
				query.Add(key, fmt.Sprint(item))
			}
		case isList:
			parts := []string{}
			for _, item := range list {
				parts = append(parts, fmt.Sprint(item))
			}
			query.Set(key, strings.Join(parts, ","))
		default:
			query.Set(key, fmt.Sprint(value))
		}
	}
	return query.Encode()
}

// A knowledge base view keeps article list filters, apart from the inbox views: the inbox list never
// shows it, a name may be used in both, and it opens on exactly the articles its filters describe.
func TestKnowledgeBaseSavedViewsKeepArticleFiltersApartFromTheInbox(t *testing.T) {
	f := newKBFilterFixture(t)
	api, admin := f.api, f.admin
	create(t, api, admin, "/api/v1/saved-views", map[string]any{"name": "Billing", "scope": "shared", "filters": map[string]any{"status": []string{"open"}}})
	var saved areaView
	kbCall(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "Billing", "area": "kb", "scope": "personal", "filters": map[string]any{
		"collection_id": f.billing, "include_subcollections": true, "tag": []string{"Billing", "refund"}, "tag_match": "all", "author": []string{f.writerID},
	}}, admin, http.StatusCreated, &saved)
	if saved.Area != "kb" || saved.Filters["tag_match"] != "all" || fmt.Sprint(saved.Filters["tag"]) != "[billing refund]" {
		t.Fatalf("saved kb view = %+v", saved)
	}
	if got := f.names(t, kbViewQuery(saved.Filters)); !slices.Equal(got, []string{"card"}) {
		t.Fatalf("the saved view opens on %v, want [card]", got)
	}
	if got := fmt.Sprint(areaViews(t, api, admin, "/api/v1/saved-views")); got != "[inbox:Billing]" {
		t.Fatalf("inbox views = %s", got)
	}
	if got := fmt.Sprint(areaViews(t, api, admin, "/api/v1/saved-views?area=inbox")); got != "[inbox:Billing]" {
		t.Fatalf("area=inbox views = %s", got)
	}
	if got := fmt.Sprint(areaViews(t, api, admin, "/api/v1/saved-views?area=kb")); got != "[kb:Billing]" {
		t.Fatalf("kb views = %s", got)
	}

	// The filters are those of the article list, held to what it accepts.
	for _, filters := range []map[string]any{
		{"priority": []string{"urgent"}}, {"status": "archived"}, {"status": []string{"draft"}}, {"tag_match": "some"},
		{"created_after": "yesterday"}, {"author": []string{"writer"}}, {"include_subcollections": true}, {"cursor": "x"}, {"limit": 5},
	} {
		body := map[string]any{"name": "Refused", "area": "kb", "scope": "shared", "filters": filters}
		if response := request(t, api, http.MethodPost, "/api/v1/saved-views", body, admin, nil); response.Code != http.StatusBadRequest {
			t.Errorf("kb view with %v = %d %s, want 400", filters, response.Code, response.Body.String())
		}
	}
	if response := request(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "Drafts", "scope": "shared", "filters": map[string]any{"has_draft": true}}, admin, nil); response.Code != http.StatusBadRequest {
		t.Errorf("inbox view with an article filter = %d, want 400", response.Code)
	}
	for _, path := range []string{"/api/v1/saved-views?area=reports", "/api/v1/saved-views/trash?area=reports"} {
		if response := request(t, api, http.MethodGet, path, nil, admin, nil); response.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", path, response.Code)
		}
	}
	if response := request(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "x", "area": "reports", "scope": "shared", "filters": map[string]any{}}, admin, nil); response.Code != http.StatusBadRequest {
		t.Errorf("area reports = %d, want 400", response.Code)
	}

	// A change is checked against the view's own area; the area itself stays.
	if response := request(t, api, http.MethodPatch, "/api/v1/saved-views/"+saved.ID, map[string]any{"updated_at": saved.UpdatedAt, "filters": map[string]any{"priority": []string{"urgent"}}}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("kb view changed to inbox filters = %d, want 400", response.Code)
	}
	var changed areaView
	kbCall(t, api, http.MethodPatch, "/api/v1/saved-views/"+saved.ID, map[string]any{"updated_at": saved.UpdatedAt, "filters": map[string]any{
		"status": "draft", "created_after": "2026-11-01", "sort": "recent",
	}}, admin, http.StatusOK, &changed)
	if changed.Area != "kb" {
		t.Fatalf("changed view area = %q", changed.Area)
	}
	if got := f.names(t, kbViewQuery(changed.Filters)); !slices.Equal(got, []string{"invoice", "label"}) {
		t.Fatalf("the changed view opens on %v, want [invoice label]", got)
	}

	// The trash keeps the areas apart too.
	kbCall(t, api, http.MethodDelete, "/api/v1/saved-views/"+saved.ID, nil, admin, http.StatusOK, nil)
	if got := fmt.Sprint(areaViews(t, api, admin, "/api/v1/saved-views/trash")); got != "[]" {
		t.Fatalf("inbox trash = %s", got)
	}
	if got := fmt.Sprint(areaViews(t, api, admin, "/api/v1/saved-views/trash?area=kb")); got != "[kb:Billing]" {
		t.Fatalf("kb trash = %s", got)
	}
}

// Another workspace's knowledge base views are not listed, and its ids in a view's filters save
// like any id but open on no article.
func TestKnowledgeBaseSavedViewsNeverCrossWorkspaces(t *testing.T) {
	f := newKBFilterFixture(t)
	var foreign areaView
	kbCall(t, f.api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "Elsewhere", "area": "kb", "scope": "shared", "filters": map[string]any{
		"collection_id": f.foreignCollectionID, "author": []string{f.foreignAgentID},
	}}, f.admin, http.StatusCreated, &foreign)
	if got := f.names(t, kbViewQuery(foreign.Filters)); len(got) != 0 {
		t.Fatalf("a view on another workspace's ids opens on %v", got)
	}

	other := f.foreignAdmin
	if got := areaViews(t, f.api, other, "/api/v1/saved-views?area=kb"); len(got) != 0 {
		t.Fatalf("another workspace lists %v", got)
	}
	if response := request(t, f.api, http.MethodPatch, "/api/v1/saved-views/"+foreign.ID, map[string]any{"name": "Stolen", "updated_at": foreign.UpdatedAt}, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another workspace changes the view = %d, want 404", response.Code)
	}
}
