package httpapi_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// kbRaiseRefused checks that a change is refused with 409 approval_required and the message that names
// the articles it would have made public.
func kbRaiseRefused(t *testing.T, api http.Handler, method, path string, body any, token, wantMessage string) {
	t.Helper()
	response := serve(api, newRequest(t, method, path, body, token, nil))
	if response.Code != http.StatusConflict || errorCode(t, response) != "approval_required" {
		t.Fatalf("%s %s status=%d body=%s, want 409 approval_required", method, path, response.Code, response.Body.String())
	}
	if got := errorMessage(t, response); got != "approval_required: "+wantMessage {
		t.Fatalf("%s %s message = %q, want %q", method, path, got, wantMessage)
	}
	validateContractResponse(t, newRequest(t, method, path, body, token, nil), response)
}

func kbRequireApproval(t *testing.T, api http.Handler, admin string, collection *kbCollectionView) {
	t.Helper()
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "require_approval": "on"},
		admin, http.StatusOK, collection)
}

// Showing published articles to more people publishes them: an article's own visibility, a
// collection's visibility, moving an article and moving a collection each need kb_articles:publish
// when they raise it, while keeping articles from people is an edit.
func TestRaisingVisibilityNeedsPublish(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Raise Tokens", "admin@kb-raise-tokens.example.com")
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update")

	team := kbCollection(t, api, admin, map[string]any{"name": "Team notes", "visibility": "agents"})
	open := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	child := kbCollection(t, api, admin, map[string]any{"name": "Refunds", "parent_id": team.ID})
	kept := kbArticle(t, api, admin, map[string]any{"collection_id": open.ID, "title": "Override codes", "body_md": "Team only.",
		"status": "published", "visibility": "agents"})
	inTeam := kbArticle(t, api, admin, map[string]any{"collection_id": team.ID, "title": "Escalations", "body_md": "Call the lead.", "status": "published"})
	kbArticle(t, api, admin, map[string]any{"collection_id": child.ID, "title": "Refund window", "body_md": "14 days.", "status": "published"})

	refused(t, api, http.MethodPatch, "/api/v1/kb/articles/"+kept.ID, map[string]any{"updated_at": kept.UpdatedAt, "visibility": "everyone"}, editor, "kb_articles:publish")
	refused(t, api, http.MethodPatch, "/api/v1/kb/collections/"+team.ID, map[string]any{"updated_at": team.UpdatedAt, "visibility": "everyone"}, editor, "kb_articles:publish")
	var moved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+inTeam.ID, map[string]any{"updated_at": inTeam.UpdatedAt, "collection_id": open.ID}, editor, http.StatusOK, &moved)
	if moved.Draft == nil || moved.CollectionID != team.ID {
		t.Fatalf("an editor's move of a published article = %+v draft=%+v, want a draft that keeps it in Team notes", moved.kbArticleView, moved.Draft)
	}
	refused(t, api, http.MethodPost, "/api/v1/kb/articles/"+inTeam.ID+"/draft/publish", map[string]any{"updated_at": moved.Draft.UpdatedAt}, editor, "kb_articles:publish")
	refused(t, api, http.MethodPatch, "/api/v1/kb/collections/"+child.ID, map[string]any{"updated_at": child.UpdatedAt, "parent_id": nil}, editor, "kb_articles:publish")
	if got := kbVisibleArticleGet(t, api, admin, kept.ID); got.EffectiveVisibility != "agents" {
		t.Fatalf("after refused raises the article is %+v", got)
	}
	if got := kbVisibleArticleGet(t, api, admin, inTeam.ID); got.EffectiveVisibility != "agents" {
		t.Fatalf("after refused raises the moved article is %+v", got)
	}

	// Lowering applies at once for the same token.
	plain := kbArticle(t, api, admin, map[string]any{"collection_id": open.ID, "title": "Change your password", "body_md": "Anyone.", "status": "published"})
	var lowered kbVisibleArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+plain.ID, map[string]any{"updated_at": plain.UpdatedAt, "visibility": "help_chat"}, editor, http.StatusOK, &lowered)
	if lowered.Visibility != "help_chat" || lowered.EffectiveVisibility != "help_chat" {
		t.Fatalf("an editor lowering an article = %+v", lowered)
	}
	var closed kbVisibleCollection
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/collections/"+open.ID, map[string]any{"updated_at": open.UpdatedAt, "visibility": "agents"}, editor, http.StatusOK, &closed)
	if closed.Visibility != "agents" {
		t.Fatalf("an editor lowering a collection = %+v", closed)
	}
	if got := kbVisibleArticleGet(t, api, admin, plain.ID); got.EffectiveVisibility != "agents" {
		t.Fatalf("an article in a lowered collection is %+v", got)
	}
}

// In a collection that needs approval, a raise that would show a published article to more people
// than its approved text was approved for is refused, naming each article, on each of the four ways.
func TestRaisingVisibilityInAnApprovalCollectionNeedsAnApprovedReview(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Raise Reviews", "admin@kb-raise-reviews.example.com")

	docs := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	team := kbCollection(t, api, admin, map[string]any{"name": "Team notes", "visibility": "agents"})
	child := kbCollection(t, api, admin, map[string]any{"name": "Refunds", "parent_id": team.ID})
	own := kbArticle(t, api, admin, map[string]any{"collection_id": docs.ID, "title": "Override codes", "body_md": "Team only.",
		"status": "published", "visibility": "agents"})
	first := kbArticle(t, api, admin, map[string]any{"collection_id": team.ID, "title": "Escalations", "body_md": "Call the lead.", "status": "published"})
	second := kbArticle(t, api, admin, map[string]any{"collection_id": team.ID, "title": "Holidays", "body_md": "Closed on the 1st.", "status": "published"})
	nested := kbArticle(t, api, admin, map[string]any{"collection_id": child.ID, "title": "Refund window", "body_md": "14 days.", "status": "published"})
	kbRequireApproval(t, api, admin, &docs)
	kbRequireApproval(t, api, admin, &team)
	kbRequireApproval(t, api, admin, &child)

	kbRaiseRefused(t, api, http.MethodPatch, "/api/v1/kb/articles/"+own.ID, map[string]any{"updated_at": own.UpdatedAt, "visibility": "everyone"}, admin,
		"1 article would become public without an approved review: "+own.ID+". Send each for review and approve it first.")
	three := []string{first.ID, second.ID, nested.ID}
	slices.Sort(three)
	kbRaiseRefused(t, api, http.MethodPatch, "/api/v1/kb/collections/"+team.ID, map[string]any{"updated_at": team.UpdatedAt, "visibility": "everyone"}, admin,
		"3 articles would become public without an approved review: "+strings.Join(three, ", ")+". Send each for review and approve it first.")
	kbRaiseRefused(t, api, http.MethodPatch, "/api/v1/kb/collections/"+child.ID, map[string]any{"updated_at": child.UpdatedAt, "parent_id": nil}, admin,
		"1 article would become public without an approved review: "+nested.ID+". Send each for review and approve it first.")
	var moved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+first.ID, map[string]any{"updated_at": first.UpdatedAt, "collection_id": docs.ID}, admin, http.StatusOK, &moved)
	if moved.Draft == nil {
		t.Fatalf("moving a published article = %+v, want a draft", moved.kbArticleView)
	}
	// A draft that moves the article is new text for that collection: the review every draft there
	// needs refuses it first, and says how to get one.
	kbRaiseRefused(t, api, http.MethodPost, "/api/v1/kb/articles/"+first.ID+"/draft/publish", map[string]any{"updated_at": moved.Draft.UpdatedAt}, admin,
		"This collection needs an approved version; send it for review: POST /api/v1/kb/articles/{id}/reviews with reviewer_ids; once a reviewer approves this exact text, publish it.")

	for _, id := range []string{own.ID, first.ID, second.ID, nested.ID} {
		if got := kbVisibleArticleGet(t, api, admin, id); got.EffectiveVisibility != "agents" {
			t.Fatalf("after refused raises article %s is %+v", id, got)
		}
	}
}
