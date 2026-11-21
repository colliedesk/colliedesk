package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// kbVisibleArticle is an article answer with who can see it.
type kbVisibleArticle struct {
	ID                  string `json:"id"`
	Status              string `json:"status"`
	Visibility          string `json:"visibility"`
	EffectiveVisibility string `json:"effective_visibility"`
	UpdatedAt           string `json:"updated_at"`
	VisibilityFrom      *struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Visibility string `json:"visibility"`
	} `json:"visibility_from"`
}

// kbVisibleCollection is a collection answer with who can see it.
type kbVisibleCollection struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Visibility          string `json:"visibility"`
	EffectiveVisibility string `json:"effective_visibility"`
	UpdatedAt           string `json:"updated_at"`
}

func kbVisibleArticleGet(t *testing.T, api http.Handler, token, id string) kbVisibleArticle {
	t.Helper()
	var article kbVisibleArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+id, nil, token, http.StatusOK, &article)
	return article
}

func kbListByVisibility(t *testing.T, api http.Handler, token, visibility string) []string {
	t.Helper()
	var page struct {
		Items []kbVisibleArticle `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?visibility="+visibility, nil, token, http.StatusOK, &page)
	var ids []string
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	slices.Sort(ids)
	return ids
}

// An article and a collection each say who can see them; an article in a restricted collection says
// which collection keeps it restricted, and the list filters on the effective visibility.
func TestArticlesAndCollectionsSayWhoCanSeeThem(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Visibility", "admin@kb-visibility.example.com")

	var open, chat kbVisibleCollection
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Guides"}, admin, http.StatusCreated, &open)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Chat notes", "visibility": "help_chat"}, admin, http.StatusCreated, &chat)
	if open.Visibility != "everyone" || open.EffectiveVisibility != "everyone" || chat.Visibility != "help_chat" || chat.EffectiveVisibility != "help_chat" {
		t.Fatalf("collections: open=%+v chat=%+v", open, chat)
	}

	var team, inChat, plain kbVisibleArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": open.ID, "title": "Refund overrides", "body_md": "Team only.",
		"status": "published", "visibility": "agents"}, admin, http.StatusCreated, &team)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": chat.ID, "title": "Delivery windows", "body_md": "For chat.",
		"status": "published"}, admin, http.StatusCreated, &inChat)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": open.ID, "title": "Change your password", "body_md": "Anyone.",
		"status": "published"}, admin, http.StatusCreated, &plain)
	if team.Visibility != "agents" || team.EffectiveVisibility != "agents" || team.VisibilityFrom != nil {
		t.Fatalf("agents-only article = %+v", team)
	}
	got := kbVisibleArticleGet(t, api, admin, inChat.ID)
	if got.Visibility != "everyone" || got.EffectiveVisibility != "help_chat" || got.VisibilityFrom == nil ||
		got.VisibilityFrom.ID != chat.ID || got.VisibilityFrom.Name != "Chat notes" || got.VisibilityFrom.Visibility != "help_chat" {
		t.Fatalf("article in a help chat collection = %+v (from %+v)", got, got.VisibilityFrom)
	}
	if got := kbVisibleArticleGet(t, api, admin, plain.ID); got.EffectiveVisibility != "everyone" || got.VisibilityFrom != nil {
		t.Fatalf("plain article = %+v", got)
	}

	if ids, want := kbListByVisibility(t, api, admin, "agents"), []string{team.ID}; !slices.Equal(ids, want) {
		t.Fatalf("visibility=agents lists %v, want %v", ids, want)
	}
	if ids, want := kbListByVisibility(t, api, admin, "help_chat"), []string{inChat.ID}; !slices.Equal(ids, want) {
		t.Fatalf("visibility=help_chat lists %v, want %v", ids, want)
	}
	// The contract refuses these values too, so they go straight to the server.
	if response := serve(api, newRequest(t, http.MethodGet, "/api/v1/kb/articles?visibility=secret", nil, admin, nil)); response.Code != http.StatusBadRequest ||
		errorCode(t, response) != "invalid_request" {
		t.Fatalf("an unknown visibility filter: %d %s", response.Code, response.Body.String())
	}
	odd := map[string]any{"collection_id": open.ID, "title": "Odd", "visibility": "partners"}
	if response := serve(api, newRequest(t, http.MethodPost, "/api/v1/kb/articles", odd, admin, nil)); response.Code != http.StatusBadRequest ||
		errorCode(t, response) != "invalid_request" {
		t.Fatalf("an unknown visibility on create: %d %s", response.Code, response.Body.String())
	}

	// Lowering the collection takes its article with it; raising the article itself back is its own field.
	var lowered kbVisibleCollection
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+open.ID, map[string]any{"updated_at": open.UpdatedAt, "visibility": "agents"}, admin, http.StatusOK, &lowered)
	if lowered.Visibility != "agents" {
		t.Fatalf("lowered collection = %+v", lowered)
	}
	if got := kbVisibleArticleGet(t, api, admin, plain.ID); got.EffectiveVisibility != "agents" || got.VisibilityFrom == nil || got.VisibilityFrom.Name != "Guides" {
		t.Fatalf("article in a lowered collection = %+v", got)
	}
	var raised kbVisibleArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+team.ID, map[string]any{"updated_at": team.UpdatedAt, "visibility": "everyone"}, admin, http.StatusOK, &raised)
	if raised.Visibility != "everyone" || raised.EffectiveVisibility != "agents" {
		t.Fatalf("article set to everyone in an agents-only collection = %+v", raised)
	}
}
