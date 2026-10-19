package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type savedView struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Scope        string         `json:"scope"`
	Filters      map[string]any `json:"filters"`
	OwnerAgentID *string        `json:"owner_agent_id"`
	UpdatedAt    string         `json:"updated_at"`
	DeletedAt    *string        `json:"deleted_at"`
	PurgesAt     *string        `json:"purges_at"`
}

func savedViewList(t *testing.T, api http.Handler, token, path string) []savedView {
	t.Helper()
	response := request(t, api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
	}
	var list struct {
		Items []savedView `json:"items"`
	}
	decode(t, response, &list)
	return list.Items
}

// viewSeen is the updated_at of a live view as the caller reads it, which a change must send back.
func viewSeen(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	for _, view := range savedViewList(t, api, token, "/api/v1/saved-views") {
		if view.ID == id {
			return view.UpdatedAt
		}
	}
	t.Fatalf("view %s is not listed", id)
	return ""
}

// stale is an updated_at no view has; it gets a request past the body check to the lookup.
const stale = "2026-01-01T00:00:00Z"

func viewNames(views []savedView) []string {
	names := make([]string, len(views))
	for index, view := range views {
		names[index] = view.Scope + ":" + view.Name
	}
	return names
}

func TestSavedViewsArePersonalOrSharedAndComeBackFromTheTrash(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "View Desk", "admin@views.example.com")
	teammate := joinAsAgent(t, api, admin, "agent@views.example.com")

	created := request(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{
		"name": "My urgent", "scope": "personal", "filters": map[string]any{"priority": []string{"urgent", "high"}, "assignee": []string{"me"}, "sort": "waiting_longest"},
	}, admin, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create personal = %d %s", created.Code, created.Body.String())
	}
	var mine savedView
	decode(t, created, &mine)
	if mine.Scope != "personal" || mine.OwnerAgentID == nil || mine.DeletedAt != nil || fmt.Sprint(mine.Filters["priority"]) != "[urgent high]" || mine.Filters["sort"] != "waiting_longest" {
		t.Fatalf("created view = %+v", mine)
	}
	shared := create(t, api, admin, "/api/v1/saved-views", map[string]any{"name": "Billing queue", "scope": "shared", "filters": map[string]any{"status": []string{"open"}, "search": "invoice"}})

	if got := fmt.Sprint(viewNames(savedViewList(t, api, admin, "/api/v1/saved-views"))); got != "[shared:Billing queue personal:My urgent]" {
		t.Fatalf("admin sees %s", got)
	}
	// A personal view is nobody else's business: not listed, not changeable, not deletable.
	if got := fmt.Sprint(viewNames(savedViewList(t, api, teammate, "/api/v1/saved-views"))); got != "[shared:Billing queue]" {
		t.Fatalf("teammate sees %s", got)
	}
	for _, call := range []struct{ method, path string }{
		{http.MethodPatch, "/api/v1/saved-views/" + mine.ID}, {http.MethodDelete, "/api/v1/saved-views/" + mine.ID},
		{http.MethodPost, "/api/v1/saved-views/" + mine.ID + "/restore"},
	} {
		if response := request(t, api, call.method, call.path, map[string]any{"name": "Taken over", "updated_at": stale}, teammate, nil); response.Code != http.StatusNotFound {
			t.Fatalf("teammate %s %s = %d, want 404", call.method, call.path, response.Code)
		}
	}
	// The teammate may use the same name for their own view, and change the shared one.
	create(t, api, teammate, "/api/v1/saved-views", map[string]any{"name": "my URGENT", "scope": "personal", "filters": map[string]any{}})
	// A change sends the version it was made on: without it, or with an older one, nothing changes.
	if response := request(t, api, http.MethodPatch, "/api/v1/saved-views/"+shared, map[string]any{"name": "Billing"}, teammate, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("change without updated_at = %d, want 400", response.Code)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/saved-views/"+shared, map[string]any{"name": "Billing", "updated_at": stale}, teammate, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), "changed_elsewhere") {
		t.Fatalf("change on an older version = %d %s, want 409 changed_elsewhere", response.Code, response.Body.String())
	}
	renamed := request(t, api, http.MethodPatch, "/api/v1/saved-views/"+shared, map[string]any{"name": "Billing", "filters": map[string]any{"tag_match": "all"}, "updated_at": viewSeen(t, api, teammate, shared)}, teammate, nil)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename shared = %d %s", renamed.Code, renamed.Body.String())
	}
	if conflict := request(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "billing", "scope": "shared", "filters": map[string]any{}}, admin, nil); conflict.Code != http.StatusConflict {
		t.Fatalf("duplicate shared name = %d, want 409", conflict.Code)
	}

	// Delete is a trip to the trash, with a way back.
	deleted := request(t, api, http.MethodDelete, "/api/v1/saved-views/"+shared, nil, teammate, nil)
	var trashed savedView
	decode(t, deleted, &trashed)
	if deleted.Code != http.StatusOK || trashed.DeletedAt == nil || trashed.PurgesAt == nil {
		t.Fatalf("delete = %d %+v", deleted.Code, trashed)
	}
	if got := fmt.Sprint(viewNames(savedViewList(t, api, admin, "/api/v1/saved-views"))); got != "[personal:My urgent]" {
		t.Fatalf("after delete admin sees %s", got)
	}
	if got := fmt.Sprint(viewNames(savedViewList(t, api, admin, "/api/v1/saved-views/trash"))); got != "[shared:Billing]" {
		t.Fatalf("trash = %s", got)
	}
	// A new view may take the name while the old one is in the trash; restoring then asks to rename first.
	taker := create(t, api, admin, "/api/v1/saved-views", map[string]any{"name": "Billing", "scope": "shared", "filters": map[string]any{}})
	if response := request(t, api, http.MethodPost, "/api/v1/saved-views/"+shared+"/restore", nil, admin, nil); response.Code != http.StatusConflict {
		t.Fatalf("restore onto a taken name = %d, want 409", response.Code)
	}
	request(t, api, http.MethodDelete, "/api/v1/saved-views/"+taker, nil, admin, nil)
	if response := request(t, api, http.MethodPost, "/api/v1/saved-views/"+shared+"/restore", nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", response.Code, response.Body.String())
	}
	if got := fmt.Sprint(viewNames(savedViewList(t, api, teammate, "/api/v1/saved-views"))); got != "[shared:Billing personal:my URGENT]" {
		t.Fatalf("after restore teammate sees %s", got)
	}

	// Sharing a personal view is the owner's call; making a shared view personal hands it to the caller.
	if response := request(t, api, http.MethodPatch, "/api/v1/saved-views/"+mine.ID, map[string]any{"scope": "shared", "updated_at": viewSeen(t, api, admin, mine.ID)}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("share = %d %s", response.Code, response.Body.String())
	}
	// The teammate already has a personal view of that name.
	if response := request(t, api, http.MethodPatch, "/api/v1/saved-views/"+mine.ID, map[string]any{"scope": "personal", "updated_at": viewSeen(t, api, teammate, mine.ID)}, teammate, nil); response.Code != http.StatusConflict {
		t.Fatalf("take a shared view onto a personal name = %d, want 409", response.Code)
	}
	var taken savedView
	decode(t, request(t, api, http.MethodPatch, "/api/v1/saved-views/"+mine.ID, map[string]any{"scope": "personal", "name": "Urgent, mine now", "updated_at": viewSeen(t, api, teammate, mine.ID)}, teammate, nil), &taken)
	if taken.Scope != "personal" || taken.OwnerAgentID == nil || *taken.OwnerAgentID == *mine.OwnerAgentID {
		t.Fatalf("made personal by the teammate = %+v", taken)
	}

	// The trash keeps a view 30 days, then it is gone.
	if _, err := pool.Exec(context.Background(), `UPDATE saved_views SET deleted_at=now()-interval '31 days' WHERE id=$1`, taker); err != nil {
		t.Fatal(err)
	}
	if got := savedViewList(t, api, admin, "/api/v1/saved-views/trash"); len(got) != 0 {
		t.Fatalf("trash after 30 days = %v", viewNames(got))
	}
	if response := request(t, api, http.MethodPost, "/api/v1/saved-views/"+taker+"/restore", nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("restore after 30 days = %d, want 404", response.Code)
	}
}

func TestSavedViewsRefuseFiltersTheInboxWouldRefuse(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Strict Views", "admin@strict-views.example.com")
	for _, body := range []map[string]any{
		{"name": "", "scope": "shared", "filters": map[string]any{}},
		{"name": "   ", "scope": "shared", "filters": map[string]any{}},
		{"name": string(make([]byte, 81)), "scope": "shared", "filters": map[string]any{}},
		{"name": "x", "scope": "team", "filters": map[string]any{}},
		{"name": "x", "scope": "shared"},
		{"name": "x", "scope": "shared", "filters": map[string]any{"status": []string{"opened"}}},
		{"name": "x", "scope": "shared", "filters": map[string]any{"status": "open"}},
		{"name": "x", "scope": "shared", "filters": map[string]any{"page": 2}},
		{"name": "x", "scope": "shared", "filters": map[string]any{"tag": []string{"not-a-uuid"}}},
		{"name": "x", "scope": "shared", "filters": map[string]any{"created_after": "yesterday"}},
		{"name": "x", "scope": "shared", "filters": map[string]any{"has_pending_draft": "true"}},
	} {
		if response := request(t, api, http.MethodPost, "/api/v1/saved-views", body, token, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("POST %v = %d, want 400: %s", body, response.Code, response.Body.String())
		}
	}
	for index := range 50 {
		create(t, api, token, "/api/v1/saved-views", map[string]any{"name": fmt.Sprint("View ", index), "scope": "personal", "filters": map[string]any{}})
	}
	if response := request(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "One too many", "scope": "personal", "filters": map[string]any{}}, token, nil); response.Code != http.StatusConflict {
		t.Fatalf("51st personal view = %d, want 409", response.Code)
	}
}

func TestSavedViewsNeverCrossWorkspaces(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Views A", "admin@views-a.example.com")
	adminB := createSession(t, api, service, "Views B", "admin@views-b.example.com")
	sharedB := create(t, api, adminB, "/api/v1/saved-views", map[string]any{"name": "B shared", "scope": "shared", "filters": map[string]any{}})
	personalB := create(t, api, adminB, "/api/v1/saved-views", map[string]any{"name": "B personal", "scope": "personal", "filters": map[string]any{}})
	trashedB := create(t, api, adminB, "/api/v1/saved-views", map[string]any{"name": "B trashed", "scope": "shared", "filters": map[string]any{}})
	request(t, api, http.MethodDelete, "/api/v1/saved-views/"+trashedB, nil, adminB, nil)
	if got := savedViewList(t, api, adminA, "/api/v1/saved-views"); len(got) != 0 {
		t.Fatalf("workspace A lists %v", viewNames(got))
	}
	if got := savedViewList(t, api, adminA, "/api/v1/saved-views/trash"); len(got) != 0 {
		t.Fatalf("workspace A trash lists %v", viewNames(got))
	}
	for _, id := range []string{sharedB, personalB, trashedB} {
		for _, call := range []struct{ method, path string }{
			{http.MethodPatch, "/api/v1/saved-views/" + id}, {http.MethodDelete, "/api/v1/saved-views/" + id}, {http.MethodPost, "/api/v1/saved-views/" + id + "/restore"},
		} {
			if response := request(t, api, call.method, call.path, map[string]any{"name": "Stolen", "updated_at": stale}, adminA, nil); response.Code != http.StatusNotFound {
				t.Fatalf("workspace A %s %s = %d, want 404", call.method, call.path, response.Code)
			}
		}
	}
	if got := fmt.Sprint(viewNames(savedViewList(t, api, adminB, "/api/v1/saved-views"))); got != "[shared:B shared personal:B personal]" {
		t.Fatalf("workspace B views changed: %s", got)
	}
}
