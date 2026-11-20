package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// An image, a video and its poster that only the unpublished changes of a published article name are
// not public: the help center answers 404 for them until the changes are published.
func TestMediaOnlyUnpublishedChangesNameStaysOffTheHelpCenter(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Draft Media Public", "admin@kb-draft-media-public.example.com")
	enableHelpCenter(t, api, admin, "Draft Media", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	image := uploadKBImage(t, api, admin, pngImage(180), http.StatusCreated)
	data := mediaVideo(t, "h264.mp4")
	video := uploadKBVideo(t, api, admin, data, len(data), nil)
	if response := serve(api, kbPosterRequest(video.ID, admin, pngImage(181))); response.Code != http.StatusOK {
		t.Fatalf("poster status=%d body=%s", response.Code, response.Body.String())
	}
	var item kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+video.ID+"/details", nil, admin, http.StatusOK, &item)
	poster := base + "/media/" + video.ID + "/poster/" + (*item.PosterURL)[len(*item.PosterURL)-8:]

	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Tour", "status": "published", "body_md": "Plain text."})
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt,
		"body_md": "![Form](/api/v1/kb/images/" + image.ID + ")\n\n<video src=\"" + video.URL + "\"></video>"}, admin, http.StatusOK, &saved)

	paths := []string{base + "/images/" + image.ID, base + "/media/" + video.ID, poster}
	for _, path := range paths {
		if response := visit(site, path, "192.0.2.30"); response.Code != http.StatusNotFound {
			t.Errorf("%s while only the unpublished changes name it: status=%d, want 404", path, response.Code)
		}
	}
	kbPublishDraft(t, api, admin, article.ID, saved.UpdatedAt, nil)
	for _, path := range paths {
		if response := visit(site, path, "192.0.2.30"); response.Code != http.StatusOK {
			t.Errorf("%s once published: status=%d, want 200", path, response.Code)
		}
	}
}

// Restoring an old version of a published article changes only its unpublished changes, so a token
// with kb_articles:update and no kb_articles:publish may do it; the live page stays as it was.
func TestATokenWithoutPublishRestoresAVersionOfAPublishedArticleIntoItsDraft(t *testing.T) {
	api, service := testAPI(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Draft Restore Token", "admin@kb-draft-restore-token.example.com")
	enableHelpCenter(t, api, admin, "Restore Token", true)
	live, saved := kbDraftedFixture(t, api, admin)
	var published kbDraftedArticle
	kbPublishDraft(t, api, admin, live.ID, saved.UpdatedAt, &published)
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update")

	var restored kbDraftedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/versions/1/restore", map[string]any{"updated_at": published.UpdatedAt}, editor, http.StatusOK, &restored)
	if restored.BodyMD != "Within 30 days." || restored.Draft == nil || restored.Draft.BodyMD != "Within 14 days." {
		t.Fatalf("restored = %+v draft=%+v; want the live text kept and version 1 in the draft", restored.kbArticleView, restored.Draft)
	}
	page := fmt.Sprintf("/help/%s/a/%d-%s", workspaceSlug(t, api, admin), published.Number, published.Slug)
	wantPage(t, visit(site, page, "192.0.2.31"), http.StatusOK, []string{"Within 30 days."}, []string{"Within 14 days."})
}

// Unpublished changes that move an article to a collection put in the trash afterwards cannot be
// published: the article would go live in a collection the help center does not show.
func TestPublishingChangesWhoseCollectionIsInTheTrashIsRefused(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Trashed Home", "admin@kb-draft-trashed-home.example.com")
	home := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	moved := kbCollection(t, api, admin, map[string]any{"name": "Archive"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": home.ID, "title": "Refunds", "status": "published", "body_md": "Within 14 days."})
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "collection_id": moved.ID}, admin, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.CollectionID != moved.ID || saved.CollectionID != home.ID {
		t.Fatalf("saved = %+v draft=%+v; want the move in the draft only", saved.kbArticleView, saved.Draft)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+moved.ID+"?updated_at="+url.QueryEscape(moved.UpdatedAt), nil, admin, http.StatusNoContent, nil)

	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/draft/publish", map[string]any{"updated_at": kbGetArticle(t, api, admin, live.ID).UpdatedAt}, admin, http.StatusConflict, nil)
	if !strings.Contains(response.Body.String(), "collection_in_trash") {
		t.Fatalf("publish body=%s, want collection_in_trash", response.Body.String())
	}
	if got := kbGetArticle(t, api, admin, live.ID); got.CollectionID != home.ID || got.Draft == nil {
		t.Fatalf("after the refusal: %+v draft=%+v; want the article in its collection with its draft", got.kbArticleView, got.Draft)
	}
}
