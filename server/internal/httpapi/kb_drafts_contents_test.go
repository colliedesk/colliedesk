package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// kbContentsDraft is an article and its draft as far as the on-this-page switch goes.
type kbContentsDraft struct {
	contentsArticleView
	Draft *struct {
		ShowContents *bool `json:"show_contents"`
	} `json:"draft"`
}

// Turning the contents list off on a published article is a change like any other: it waits in the
// draft, the help center keeps the list until the draft is published, and the draft version records it.
func TestTurningContentsOffOnAPublishedArticleWaitsForPublish(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Contents Draft", "admin@contents-draft.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Contents draft help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	var live contentsArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Long guide", "status": "published", "body_md": fourSections},
		admin, http.StatusCreated, &live)
	path := fmt.Sprintf("/help/%s/a/%d-%s", slug, live.Number, live.Slug)

	var saved kbContentsDraft
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "show_contents": false}, admin, http.StatusOK, &saved)
	if !saved.ShowContents || saved.Draft == nil || saved.Draft.ShowContents == nil || *saved.Draft.ShowContents {
		t.Fatalf("after the save show_contents = %v, draft = %+v; want the live article on and the draft off", saved.ShowContents, saved.Draft)
	}
	wantPage(t, visit(site, path, "192.0.2.150"), http.StatusOK, []string{`class="contents`}, nil)
	var drafted contentsArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"?version=draft", nil, admin, http.StatusOK, &drafted)
	if drafted.ShowContents {
		t.Fatal("?version=draft shows the contents list on, want off")
	}

	// A later save that leaves the switch out keeps the draft's value.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.UpdatedAt, "title": "Long guide, revised"}, admin, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.ShowContents == nil || *saved.Draft.ShowContents {
		t.Fatalf("a save without show_contents reset the draft to %+v", saved.Draft)
	}

	var versions struct {
		Items []struct {
			Kind         string `json:"kind"`
			ShowContents bool   `json:"show_contents"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID+"/versions", nil, admin, http.StatusOK, &versions)
	if len(versions.Items) == 0 || versions.Items[0].Kind != "draft" || versions.Items[0].ShowContents {
		t.Fatalf("versions = %+v, want the newest a draft with the list off", versions.Items)
	}

	var published contentsArticleView
	kbPublishDraft(t, api, admin, live.ID, saved.UpdatedAt, &published)
	if published.ShowContents {
		t.Fatal("publishing the draft left the contents list on")
	}
	wantPage(t, visit(site, path, "192.0.2.151"), http.StatusOK, nil, []string{`class="contents`})
}
