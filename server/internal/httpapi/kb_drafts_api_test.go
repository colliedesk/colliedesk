package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbDraftView struct {
	Title          string   `json:"title"`
	Slug           string   `json:"slug"`
	BodyMD         string   `json:"body_md"`
	SEOTitle       string   `json:"seo_title"`
	SEODescription string   `json:"seo_description"`
	CollectionID   string   `json:"collection_id"`
	CollectionIDs  []string `json:"collection_ids"`
	UpdatedAt      string   `json:"updated_at"`
	UpdatedBy      *struct {
		Name string `json:"name"`
	} `json:"updated_by"`
}

// kbDraftedArticle is an article as GET answers it, with its unpublished changes.
type kbDraftedArticle struct {
	kbArticleView
	HasDraft bool         `json:"has_draft"`
	Draft    *kbDraftView `json:"draft"`
}

// kbSeen is the updated_at a change to the article sends back, its draft or not.
func kbSeen(article kbDraftedArticle) string {
	return article.UpdatedAt
}

// kbPublishChange saves change to a published article and publishes it, as an editor does with Save
// and then Publish changes; into gets the published article.
func kbPublishChange(t *testing.T, api http.Handler, token, id string, change map[string]any, into any) {
	t.Helper()
	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+id, change, token, http.StatusOK, &saved)
	if saved.Draft == nil {
		t.Fatalf("PATCH %s saved no draft", id)
	}
	kbPublishDraft(t, api, token, id, saved.Draft.UpdatedAt, into)
}

// kbPublishDraft publishes the unpublished changes of an article whose draft has updated_at seen.
func kbPublishDraft(t *testing.T, api http.Handler, token, id, seen string, into any) {
	t.Helper()
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+id+"/draft/publish", map[string]any{"updated_at": seen}, token, http.StatusOK, into)
}

func kbGetArticle(t *testing.T, api http.Handler, token, id string) kbDraftedArticle {
	t.Helper()
	var article kbDraftedArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+id, nil, token, http.StatusOK, &article)
	return article
}

func kbVersionKinds(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var versions struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+id+"/versions", nil, token, http.StatusOK, &versions)
	kinds := []string{}
	for _, version := range versions.Items {
		kinds = append(kinds, version.Kind)
	}
	return strings.Join(kinds, ",")
}

// Saving a published article keeps the change as a draft: the help center, its search, sitemap.xml,
// llms.txt, the Markdown copy and AI answers keep the published text until someone publishes it.
func TestSavingAPublishedArticleKeepsTheHelpCenterOnThePublishedText(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB Drafts", "admin@kb-drafts.example.com")
	workspace := workspaceID(t, api, admin)
	enableHelpCenter(t, api, admin, "Draft Answers", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Accounts"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password",
		"body_md": "Press the pinecone button.", "status": "published"})

	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt,
		"title": "Change your password", "slug": "change-your-password", "body_md": "Press the marigold button."}, admin, http.StatusOK, &saved)
	if saved.Title != "Reset your password" || saved.BodyMD != "Press the pinecone button." || saved.UpdatedAt == live.UpdatedAt {
		t.Fatalf("the live article changed, or the updated_at to send back did not move: %+v", saved.kbArticleView)
	}
	if !saved.HasDraft || saved.Draft == nil || saved.Draft.Title != "Change your password" || saved.Draft.Slug != "change-your-password" ||
		saved.Draft.BodyMD != "Press the marigold button." || saved.Draft.CollectionID != collection.ID || saved.Draft.UpdatedBy == nil {
		t.Fatalf("draft = %+v", saved.Draft)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "draft,create" {
		t.Errorf("versions = %s, want draft, create", got)
	}
	if got := kbGetArticle(t, api, admin, live.ID); !got.HasDraft || got.Draft == nil || got.Draft.BodyMD != "Press the marigold button." {
		t.Fatalf("GET draft = %+v", got.Draft)
	}

	page := fmt.Sprintf("%s/a/%d-%s", base, live.Number, live.Slug)
	unchanged := func(step string) {
		t.Helper()
		for _, path := range []string{page, page + ".md", base + "/search?q=" + url.QueryEscape("pinecone"), base + "/llms.txt", base + "/sitemap.xml"} {
			response := visit(site, path, "198.51.100.40")
			if response.Code != http.StatusOK {
				t.Fatalf("%s: %s status=%d", step, path, response.Code)
			}
			body := response.Body.String()
			if strings.Contains(body, "marigold") || strings.Contains(body, "Change your password") || strings.Contains(body, "change-your-password") {
				t.Errorf("%s: %s shows the draft:\n%s", step, path, body)
			}
		}
		if body := visit(site, base+"/search?q=marigold", "198.51.100.40").Body.String(); strings.Contains(body, "Reset your password") {
			t.Errorf("%s: a search for the draft's words found the article", step)
		}
		buildAIRetrieval(t, pool)
		for _, doc := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "marigold button", KBLimit: 5}) {
			if strings.Contains(doc.Text, "marigold") || strings.Contains(doc.Title, "Change your") {
				t.Errorf("%s: AI reads the draft: %+v", step, doc)
			}
		}
	}
	unchanged("after a draft save")

	// A second save changes the same draft and needs the draft's updated_at.
	stale := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "body_md": "Lost"}, admin, nil)
	if stale.Code != http.StatusConflict || errorCode(t, stale) != "changed_elsewhere" {
		t.Fatalf("a save on the live updated_at status=%d body=%s, want 409 changed_elsewhere", stale.Code, stale.Body.String())
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.Draft.UpdatedAt,
		"body_md": "Press the marigold button twice."}, admin, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.Title != "Change your password" || saved.Draft.BodyMD != "Press the marigold button twice." {
		t.Fatalf("second draft save = %+v", saved.Draft)
	}
	unchanged("after a second draft save")

	var published kbDraftedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/draft/publish", map[string]any{"updated_at": saved.Draft.UpdatedAt}, admin, http.StatusOK, &published)
	if published.HasDraft || published.Draft != nil || published.Title != "Change your password" || published.Slug != "change-your-password" ||
		published.BodyMD != "Press the marigold button twice." || published.Status != "published" || published.LastChange == nil || published.LastChange.Kind != "edit" {
		t.Fatalf("published = %+v draft=%+v", published.kbArticleView, published.Draft)
	}
	if got := kbVersionKinds(t, api, admin, live.ID); got != "edit,draft,draft,create" {
		t.Errorf("versions = %s, want edit, draft, draft, create", got)
	}
	wantPage(t, visit(site, fmt.Sprintf("%s/a/%d-%s", base, live.Number, "change-your-password"), "198.51.100.40"), http.StatusOK,
		[]string{"Change your password", "marigold button twice"}, []string{"pinecone"})
}

// A token that may edit articles but not publish them saves drafts of a published article; publishing
// the draft, and unpublishing the article, are refused and name kb_articles:publish.
func TestATokenWithoutPublishSavesADraftButCannotPublishIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Tokens", "admin@kb-draft-tokens.example.com")
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})

	var saved kbDraftedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt, "body_md": "Within 30 days."}, editor, http.StatusOK, &saved)
	if saved.Draft == nil || saved.Draft.BodyMD != "Within 30 days." || saved.BodyMD != "Within 14 days." {
		t.Fatalf("editor's save = %+v draft=%+v", saved.kbArticleView, saved.Draft)
	}
	refused(t, api, http.MethodPost, "/api/v1/kb/articles/"+live.ID+"/draft/publish", map[string]any{"updated_at": saved.Draft.UpdatedAt}, editor, "kb_articles:publish")
	refused(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": saved.Draft.UpdatedAt, "status": "draft"}, editor, "kb_articles:publish")
	if got := kbGetArticle(t, api, admin, live.ID); got.BodyMD != "Within 14 days." || got.Status != "published" || got.Draft == nil {
		t.Fatalf("after refused calls = %+v draft=%+v", got.kbArticleView, got.Draft)
	}
}
