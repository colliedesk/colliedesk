package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// An autosave of a published article keeps the text in its unpublished changes and adds no version:
// the help center keeps the published text, and the next Save adds the version.
func TestAutosaveOfAPublishedArticleChangesOnlyItsDraft(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Autosave", "admin@kb-autosave.example.com")
	enableHelpCenter(t, api, admin, "Autosave Answers", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Accounts"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password",
		"body_md": "Press the pinecone button.", "status": "published"})

	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "autosave": true,
		"title": "Change your password", "body_md": "Press the marigold button."}, admin, http.StatusOK, &saved)
	if saved.Title != "Reset your password" || saved.BodyMD != "Press the pinecone button." || saved.UpdatedAt == live.UpdatedAt {
		t.Fatalf("the live article changed, or updated_at did not move: %+v", saved.kbArticleView)
	}
	if saved.Draft == nil || saved.Draft.Title != "Change your password" || saved.Draft.BodyMD != "Press the marigold button." {
		t.Fatalf("draft = %+v", saved.Draft)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "create" {
		t.Errorf("versions after an autosave = %s, want create", got)
	}
	page := fmt.Sprintf("%s/a/%d-%s", base, live.Number, live.Slug)
	wantPage(t, visit(site, page, "198.51.100.41"), http.StatusOK, []string{"pinecone"}, []string{"marigold", "Change your password"})

	// A second autosave changes the same draft; the next Save records one version for both.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.UpdatedAt, "autosave": true,
		"body_md": "Press the marigold button twice."}, admin, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.BodyMD != "Press the marigold button twice." || saved.Draft.Title != "Change your password" {
		t.Fatalf("second autosave draft = %+v", saved.Draft)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.UpdatedAt,
		"body_md": "Press the marigold button twice."}, admin, http.StatusOK, &saved)
	if got := kbVersionKinds(t, api, admin, live.ID); got != "draft,create" {
		t.Errorf("versions after Save = %s, want draft, create", got)
	}
	wantPage(t, visit(site, page, "198.51.100.41"), http.StatusOK, []string{"pinecone"}, []string{"marigold"})
}

// An autosave of an article that was never published changes it in place, with no version.
func TestAutosaveOfADraftArticleAddsNoVersion(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Draft", "admin@kb-autosave-draft.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})

	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "autosave": true,
		"body_md": "Within 30 days."}, admin, http.StatusOK, &saved)
	if saved.BodyMD != "Within 30 days." || saved.Status != "draft" || saved.HasDraft || saved.UpdatedAt == article.UpdatedAt {
		t.Fatalf("autosaved = %+v has_draft=%v", saved.kbArticleView, saved.HasDraft)
	}
	if got := kbVersionKinds(t, api, admin, article.ID); got != "create" {
		t.Errorf("versions after an autosave = %s, want create", got)
	}
	if got := kbGetArticle(t, api, admin, article.ID); got.BodyMD != "Within 30 days." {
		t.Fatalf("GET body = %q", got.BodyMD)
	}
}

// An autosave carries only the words: a move, a new slug or a status change needs a real Save, and a
// save that started from an older updated_at is refused like any other.
func TestAutosaveRefusesOtherFieldsAndAStaleUpdatedAt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Autosave Rules", "admin@kb-autosave-rules.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})

	for _, field := range []map[string]any{{"status": "published"}, {"slug": "refund-policy"}, {"collection_id": collection.ID},
		{"collection_ids": []string{collection.ID}}, {"seo_title": "Refunds"}, {"seo_description": "How"}, {"show_contents": false}} {
		change := map[string]any{"updated_at": article.UpdatedAt, "autosave": true, "body_md": "Within 30 days."}
		for key, value := range field {
			change[key] = value
		}
		response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, change, admin, nil)
		if response.Code != http.StatusBadRequest || errorCode(t, response) != "invalid_request" {
			t.Errorf("autosave with %v status=%d body=%s, want 400 invalid_request", field, response.Code, response.Body.String())
		}
	}

	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "autosave": true,
		"body_md": "Within 30 days."}, admin, http.StatusOK, &saved)
	stale := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "autosave": true,
		"body_md": "Lost"}, admin, nil)
	if stale.Code != http.StatusConflict || errorCode(t, stale) != "changed_elsewhere" {
		t.Fatalf("stale autosave status=%d body=%s, want 409 changed_elsewhere", stale.Code, stale.Body.String())
	}
	if got := kbGetArticle(t, api, admin, article.ID); got.BodyMD != "Within 30 days." {
		t.Fatalf("a refused autosave changed the text: %q", got.BodyMD)
	}
}

// Another workspace cannot autosave an article it does not own.
func TestAutosaveCannotReachAnotherWorkspacesArticle(t *testing.T) {
	api, service := testAPI(t)
	owner := createSession(t, api, service, "KB Autosave Owner", "admin@kb-autosave-owner.example.com")
	stranger := createSession(t, api, service, "KB Autosave Stranger", "admin@kb-autosave-stranger.example.com")
	collection := kbCollection(t, api, owner, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, owner, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})

	response := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "autosave": true,
		"body_md": "Stolen"}, stranger, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("another workspace's autosave status=%d body=%s, want 404", response.Code, response.Body.String())
	}
	if got := kbGetArticle(t, api, owner, article.ID); got.BodyMD != "Within 14 days." {
		t.Fatalf("another workspace changed the text: %q", got.BodyMD)
	}
}
