package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// An autosave changes only the title and the text: it never changes who can see an article, so a
// published article kept for agents stays off the help center, and its typed text stays a draft.
func TestAutosaveNeverChangesWhoCanSeeAnArticle(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Autosave Visibility", "admin@kb-autosave-visibility.example.com")
	enableHelpCenter(t, api, admin, "Autosave Visibility", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Internal"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Escalation steps",
		"body_md": "Call the duty lead.", "status": "published", "visibility": "agents"})
	page := fmt.Sprintf("%s/a/%d-%s", base, live.Number, live.Slug)

	var refused struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "autosave": true,
		"body_md": "Call the duty lead first.", "visibility": "everyone"}, admin, http.StatusBadRequest, &refused)
	if refused.Error.Message != "An autosave changes only title and body_md; save the other fields without autosave." {
		t.Fatalf("autosave with visibility refused with %q", refused.Error.Message)
	}
	if got := kbVisibleArticleGet(t, api, admin, live.ID); got.Visibility != "agents" || got.EffectiveVisibility != "agents" {
		t.Fatalf("after a refused autosave the article is visible to %s (effective %s), want agents", got.Visibility, got.EffectiveVisibility)
	}

	// A plain autosave keeps the article for agents and the help center still answers 404.
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "autosave": true,
		"body_md": "Call the duty lead first."}, admin, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.BodyMD != "Call the duty lead first." {
		t.Fatalf("draft after autosave = %+v", saved.Draft)
	}
	if got := kbVisibleArticleGet(t, api, admin, live.ID); got.EffectiveVisibility != "agents" {
		t.Fatalf("after an autosave the article is visible to %s, want agents", got.EffectiveVisibility)
	}
	wantPage(t, visit(site, page, "198.51.100.42"), http.StatusNotFound, nil, []string{"duty lead"})
}
