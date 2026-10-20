package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbCollectionView struct {
	ID           string  `json:"id"`
	ParentID     *string `json:"parent_id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	ArticleCount int     `json:"article_count"`
	UpdatedAt    string  `json:"updated_at"`
}

type kbArticleView struct {
	ID           string           `json:"id"`
	Number       int64            `json:"number"`
	CollectionID string           `json:"collection_id"`
	Title        string           `json:"title"`
	Slug         string           `json:"slug"`
	Status       string           `json:"status"`
	PublishedAt  *string          `json:"published_at"`
	BodyMD       string           `json:"body_md"`
	UpdatedAt    string           `json:"updated_at"`
	DeletedAt    *string          `json:"deleted_at"`
	LastChange   *replyChangeView `json:"last_change"`
}

// kbCall sends a JSON call, checks the status and the contract, and decodes the answer into into.
func kbCall(t *testing.T, api http.Handler, method, path string, body any, token string, want int, into any) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, path, body, token, nil)
	validateContractRequest(t, req)
	response := serve(api, req)
	if response.Code != want {
		t.Fatalf("%s %s status=%d body=%s, want %d", method, path, response.Code, response.Body.String(), want)
	}
	validateContractResponse(t, newRequest(t, method, path, body, token, nil), response)
	if into != nil {
		decode(t, response, into)
	}
	return response
}

func errorCode(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, response, &body)
	return body.Error.Code
}

func kbCollection(t *testing.T, api http.Handler, token string, body map[string]any) kbCollectionView {
	t.Helper()
	var collection kbCollectionView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", body, token, http.StatusCreated, &collection)
	return collection
}

func kbArticle(t *testing.T, api http.Handler, token string, body map[string]any) kbArticleView {
	t.Helper()
	var article kbArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", body, token, http.StatusCreated, &article)
	return article
}

func kbSearch(t *testing.T, api http.Handler, token, query string) []string {
	t.Helper()
	var page struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?"+query, nil, token, http.StatusOK, &page)
	var titles []string
	for _, item := range page.Items {
		titles = append(titles, item.Title)
	}
	return titles
}

func TestKnowledgeBaseArticlesHaveStableNumbersVersionsAndATrash(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Desk", "admin@kb-desk.example.com")
	agent := agentSession(t, api, admin, "agent@kb-desk.example.com")

	top := kbCollection(t, api, agent, map[string]any{"name": "Getting started"})
	if top.Slug != "getting-started" || top.ParentID != nil {
		t.Fatalf("top collection = %+v", top)
	}
	middle := kbCollection(t, api, agent, map[string]any{"name": "Accounts", "parent_id": top.ID})
	bottom := kbCollection(t, api, agent, map[string]any{"name": "Passwords", "parent_id": middle.ID})
	tooDeep := request(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Fourth", "parent_id": bottom.ID}, agent, nil)
	if tooDeep.Code != http.StatusBadRequest {
		t.Fatalf("fourth level status=%d body=%s, want 400", tooDeep.Code, tooDeep.Body.String())
	}
	clash := request(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Accounts!", "parent_id": top.ID}, agent, nil)
	if clash.Code != http.StatusConflict || errorCode(t, clash) != "duplicate_slug" {
		t.Fatalf("same slug under one parent status=%d body=%s", clash.Code, clash.Body.String())
	}
	kbCollection(t, api, agent, map[string]any{"name": "Accounts", "slug": "accounts"}) // same slug elsewhere is fine
	moveUnder := request(t, api, http.MethodPatch, "/api/v1/kb/collections/"+top.ID, map[string]any{"updated_at": top.UpdatedAt, "parent_id": bottom.ID}, agent, nil)
	if moveUnder.Code != http.StatusBadRequest {
		t.Fatalf("move under its own child status=%d body=%s, want 400", moveUnder.Code, moveUnder.Body.String())
	}

	first := kbArticle(t, api, agent, map[string]any{"collection_id": bottom.ID, "title": "Reset your password", "body_md": "Open **Settings**."})
	second := kbArticle(t, api, agent, map[string]any{"collection_id": bottom.ID, "title": "Đổi mật khẩu", "body_md": "Mở Cài đặt.", "status": "published"})
	if first.Number != 1 || second.Number != 2 || first.Slug != "reset-your-password" || second.Slug != "đổi-mật-khẩu" {
		t.Fatalf("numbers and slugs = %d %q, %d %q", first.Number, first.Slug, second.Number, second.Slug)
	}
	if first.Status != "draft" || first.PublishedAt != nil || second.Status != "published" || second.PublishedAt == nil {
		t.Fatalf("statuses = %+v / %+v", first, second)
	}
	if first.LastChange == nil || first.LastChange.Kind != "create" || first.LastChange.ChangedBy == nil {
		t.Fatalf("first version = %+v", first.LastChange)
	}

	var edited kbArticleView
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+first.ID,
		map[string]any{"updated_at": first.UpdatedAt, "body_md": "Open **Settings**, then *Password*.", "status": "published"}, agent, http.StatusOK, &edited)
	if edited.Status != "published" || edited.PublishedAt == nil || edited.LastChange.Version != 2 || edited.Slug != first.Slug {
		t.Fatalf("edited = %+v", edited)
	}
	stale := request(t, api, http.MethodPatch, "/api/v1/kb/articles/"+first.ID, map[string]any{"updated_at": first.UpdatedAt, "title": "Lost update"}, admin, nil)
	if stale.Code != http.StatusConflict || errorCode(t, stale) != "changed_elsewhere" {
		t.Fatalf("stale edit status=%d body=%s", stale.Code, stale.Body.String())
	}
	var restored kbArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+first.ID+"/versions/1/restore", map[string]any{"updated_at": edited.UpdatedAt}, admin, http.StatusOK, &restored)
	if restored.BodyMD != "Open **Settings**." || restored.Status != "published" || restored.LastChange.Kind != "restore" || *restored.LastChange.RestoredFromVersion != 1 {
		t.Fatalf("restored version = %+v", restored)
	}
	var versions struct {
		Items []struct {
			Version int    `json:"version"`
			Kind    string `json:"kind"`
			Status  string `json:"status"`
		} `json:"items"`
		NextBeforeVersion *int `json:"next_before_version"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+first.ID+"/versions?limit=2", nil, agent, http.StatusOK, &versions)
	if len(versions.Items) != 2 || versions.Items[0].Kind != "restore" || versions.Items[1].Status != "published" || versions.NextBeforeVersion == nil || *versions.NextBeforeVersion != 2 {
		t.Fatalf("versions = %+v", versions)
	}

	// A collection that holds an article stays; once the article is in the trash it can go too.
	full := request(t, api, http.MethodDelete, "/api/v1/kb/collections/"+bottom.ID+"?updated_at="+url.QueryEscape(bottom.UpdatedAt), nil, agent, nil)
	if full.Code != http.StatusConflict || errorCode(t, full) != "collection_not_empty" {
		t.Fatalf("delete full collection status=%d body=%s", full.Code, full.Body.String())
	}
	for _, article := range []kbArticleView{restored, second} {
		kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(article.UpdatedAt), nil, agent, http.StatusNoContent, nil)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+bottom.ID+"?updated_at="+url.QueryEscape(bottom.UpdatedAt), nil, agent, http.StatusNoContent, nil)
	var trash struct {
		Items []kbTrashItem `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/trash", nil, agent, http.StatusOK, &trash)
	if len(trash.Items) != 2 || trash.Items[0].PurgesAt.Sub(trash.Items[0].DeletedAt) != 30*24*time.Hour {
		t.Fatalf("article trash = %+v", trash.Items)
	}
	inTrash := request(t, api, http.MethodPost, "/api/v1/kb/articles/"+first.ID+"/restore", map[string]any{"updated_at": trashUpdatedAt(trash.Items, first.ID)}, agent, nil)
	if inTrash.Code != http.StatusConflict || errorCode(t, inTrash) != "collection_in_trash" {
		t.Fatalf("restore article of a trashed collection status=%d body=%s", inTrash.Code, inTrash.Body.String())
	}
	var collectionTrash struct {
		Items []struct {
			ID        string `json:"id"`
			UpdatedAt string `json:"updated_at"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections/trash", nil, agent, http.StatusOK, &collectionTrash)
	if len(collectionTrash.Items) != 1 || collectionTrash.Items[0].ID != bottom.ID {
		t.Fatalf("collection trash = %+v", collectionTrash.Items)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections/"+bottom.ID+"/restore", map[string]any{"updated_at": collectionTrash.Items[0].UpdatedAt}, agent, http.StatusOK, nil)
	var back kbArticleView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+first.ID+"/restore", map[string]any{"updated_at": trashUpdatedAt(trash.Items, first.ID)}, agent, http.StatusOK, &back)
	if back.DeletedAt != nil || back.Status != "published" || back.Number != 1 || back.LastChange.Kind != "undelete" {
		t.Fatalf("article back from the trash = %+v", back)
	}
	// The number stays taken by the article in the trash: the next article gets 3.
	if third := kbArticle(t, api, agent, map[string]any{"collection_id": top.ID, "title": "Third", "body_md": ""}); third.Number != 3 {
		t.Fatalf("third article number = %d, want 3", third.Number)
	}
}

type kbTrashItem struct {
	ID        string    `json:"id"`
	UpdatedAt string    `json:"updated_at"`
	DeletedAt time.Time `json:"deleted_at"`
	PurgesAt  time.Time `json:"purges_at"`
}

func trashUpdatedAt(items []kbTrashItem, id string) string {
	for _, item := range items {
		if item.ID == id {
			return item.UpdatedAt
		}
	}
	return ""
}

func TestKnowledgeBaseSearchSplitsWordsLikeTheText(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Search", "admin@kb-search.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"})
	for title, body := range map[string]string{
		"Contact us":         "Write to help@acme.example for anything.",
		"Sign-in problems":   "Use single sign-on or e-mail links.",
		"Links":              "See https://status.acme.example/incidents for outages.",
		"100% refund policy": "Refunds within 30 days.",
		"Passwords":          "Reset it from the settings page.",
	} {
		kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": title, "body_md": body, "status": "published"})
	}
	for query, want := range map[string]string{
		"q=help%40acme.example":       "Contact us",
		"q=sign-on":                   "Sign-in problems",
		"q=status.acme":               "Links",
		"q=pass":                      "Passwords",
		"q=reset+settings":            "Passwords",
		"q=100%25":                    "100% refund policy",
		"q=settings&status=published": "Passwords",
		"q=settings&collection_id=" + collection.ID: "Passwords",
	} {
		if got := kbSearch(t, api, admin, query); len(got) == 0 || got[0] != want {
			t.Errorf("search %s = %v, want %q first", query, got, want)
		}
	}
	if got := kbSearch(t, api, admin, "q=%25"); len(got) != 1 {
		t.Errorf("a percent sign matches only titles holding one, got %v", got)
	}
	for _, odd := range []string{"q=it%27s+o%27reilly", "q=a%5Cb", "q=%27%3A*+%26+%21", "q=%E5%AF%86%E7%A0%81"} {
		kbSearch(t, api, admin, odd) // quotes, backslashes and tsquery syntax are words, never a query error
	}
	if got := kbSearch(t, api, admin, "q=nothing+here"); len(got) != 0 {
		t.Errorf("unmatched search = %v", got)
	}
	if got := kbSearch(t, api, admin, "status=draft"); len(got) != 0 {
		t.Errorf("drafts = %v, want none", got)
	}
	var page struct {
		Items      []kbArticleView `json:"items"`
		NextCursor *string         `json:"next_cursor"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?limit=3", nil, admin, http.StatusOK, &page)
	if len(page.Items) != 3 || page.NextCursor == nil || page.Items[0].BodyMD != "" {
		t.Fatalf("first page = %d items, cursor %v", len(page.Items), page.NextCursor)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?limit=3&cursor="+*page.NextCursor, nil, admin, http.StatusOK, &page)
	if len(page.Items) != 2 || page.NextCursor != nil {
		t.Fatalf("second page = %d items, cursor %v", len(page.Items), page.NextCursor)
	}
}

func TestKnowledgeBaseNeverCrossesWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Alpha", "admin@kb-alpha.example.com")
	beta := createSession(t, api, service, "KB Beta", "admin@kb-beta.example.com")
	collection := kbCollection(t, api, alpha, map[string]any{"name": "Alpha docs"})
	image := uploadKBImage(t, api, alpha, kbPNG, http.StatusCreated)
	article := kbArticle(t, api, alpha, map[string]any{"collection_id": collection.ID, "title": "Alpha secret", "body_md": "![x](/api/v1/kb/images/" + image.ID + ")"})
	betaCollection := kbCollection(t, api, beta, map[string]any{"name": "Beta docs"})

	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/kb/articles/" + article.ID},
		{http.MethodGet, "/api/v1/kb/articles/" + article.ID + "/versions"},
		{http.MethodGet, "/api/v1/kb/images/" + image.ID},
	} {
		if response := request(t, api, call.method, call.path, nil, beta, nil); response.Code != http.StatusNotFound {
			t.Errorf("beta %s %s status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	writes := []struct {
		method, path string
		body         any
	}{
		{http.MethodPatch, "/api/v1/kb/articles/" + article.ID, map[string]any{"updated_at": article.UpdatedAt, "title": "Taken"}},
		{http.MethodDelete, "/api/v1/kb/articles/" + article.ID + "?updated_at=" + url.QueryEscape(article.UpdatedAt), nil},
		{http.MethodPost, "/api/v1/kb/articles/" + article.ID + "/restore", map[string]any{"updated_at": article.UpdatedAt}},
		{http.MethodPost, "/api/v1/kb/articles/" + article.ID + "/versions/1/restore", map[string]any{"updated_at": article.UpdatedAt}},
		{http.MethodPatch, "/api/v1/kb/collections/" + collection.ID, map[string]any{"updated_at": collection.UpdatedAt, "name": "Taken"}},
		{http.MethodDelete, "/api/v1/kb/collections/" + collection.ID + "?updated_at=" + url.QueryEscape(collection.UpdatedAt), nil},
		{http.MethodPost, "/api/v1/kb/collections/" + collection.ID + "/restore", map[string]any{"updated_at": collection.UpdatedAt}},
	}
	for _, write := range writes {
		if response := request(t, api, write.method, write.path, write.body, beta, nil); response.Code != http.StatusNotFound {
			t.Errorf("beta %s %s status=%d body=%s, want 404", write.method, write.path, response.Code, response.Body.String())
		}
	}
	// Another workspace's collection is not a place for an article, nor a parent, nor a move target.
	for _, refused := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Smuggled", "body_md": "x"}},
		{http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Smuggled", "parent_id": collection.ID}},
		{http.MethodPatch, "/api/v1/kb/collections/" + betaCollection.ID, map[string]any{"updated_at": betaCollection.UpdatedAt, "parent_id": collection.ID}},
	} {
		if response := request(t, api, refused.method, refused.path, refused.body, beta, nil); response.Code != http.StatusBadRequest {
			t.Errorf("beta %s %s status=%d body=%s, want 400", refused.method, refused.path, response.Code, response.Body.String())
		}
	}
	// Beta linking alpha's image id links nothing, and listing or searching shows only its own.
	kbArticle(t, api, beta, map[string]any{"collection_id": betaCollection.ID, "title": "Beta page", "body_md": "![x](/api/v1/kb/images/" + image.ID + ")"})
	if got := kbSearch(t, api, beta, "q=secret"); len(got) != 0 {
		t.Errorf("beta search found %v", got)
	}
	if got := kbSearch(t, api, beta, ""); len(got) != 1 || got[0] != "Beta page" {
		t.Errorf("beta list = %v", got)
	}
	var collections struct {
		Items []kbCollectionView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, beta, http.StatusOK, &collections)
	if len(collections.Items) != 1 || collections.Items[0].ID != betaCollection.ID {
		t.Errorf("beta collections = %+v", collections.Items)
	}
	// The same bytes in beta are beta's own image, not alpha's.
	if betaImage := uploadKBImage(t, api, beta, kbPNG, http.StatusCreated); betaImage.ID == image.ID {
		t.Fatal("beta got alpha's image id for the same bytes")
	}
}

type kbImageView struct {
	ID           string  `json:"id"`
	ContentType  string  `json:"content_type"`
	Size         int64   `json:"size"`
	URL          string  `json:"url"`
	UpdatedAt    string  `json:"updated_at"`
	ThumbnailURL *string `json:"thumbnail_url"`
}

func uploadKBImage(t *testing.T, api http.Handler, token string, data []byte, want int) kbImageView {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kb/images", bytes.NewReader(data))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", "Bearer "+token)
	response := serve(api, req)
	if response.Code != want {
		t.Fatalf("upload status=%d body=%s, want %d", response.Code, response.Body.String(), want)
	}
	var image kbImageView
	if want == http.StatusCreated {
		decode(t, response, &image)
	}
	return image
}

func TestKnowledgeBaseImagesAreCheckedStoredOnceAndServedInert(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Images", "admin@kb-images.example.com")
	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	if image.ContentType != "image/png" || image.Size != int64(len(kbPNG)) || image.URL != "/api/v1/kb/images/"+image.ID {
		t.Fatalf("image = %+v", image)
	}
	if again := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated); again.ID != image.ID {
		t.Fatalf("the same bytes gave a second image %s", again.ID)
	}
	uploadKBImage(t, api, admin, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), http.StatusBadRequest)
	uploadKBImage(t, api, admin, []byte("<html><script>alert(1)</script></html>"), http.StatusBadRequest)
	uploadKBImage(t, api, admin, append(append([]byte{}, kbPNG...), bytes.Repeat([]byte{0}, 5<<20)...), http.StatusRequestEntityTooLarge)

	served := request(t, api, http.MethodGet, image.URL, nil, admin, nil)
	if served.Code != http.StatusOK || !bytes.Equal(served.Body.Bytes(), kbPNG) {
		t.Fatalf("served status=%d len=%d", served.Code, served.Body.Len())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, image.URL, nil, admin, nil), served)
	for header, want := range map[string]string{
		"Content-Type": "image/png", "X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'; sandbox",
		"Cache-Control": "private, max-age=31536000, immutable",
	} {
		if got := served.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestKnowledgeBaseTokenTiers(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Tokens", "admin@kb-tokens.example.com")
	read := issueToken(t, api, admin, "Reader", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	collection := kbCollection(t, api, send, map[string]any{"name": "Agent docs"})

	proposal := request(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Proposed", "body_md": "x"}, read, nil)
	if proposal.Code != http.StatusAccepted {
		t.Fatalf("read token create status=%d body=%s, want 202", proposal.Code, proposal.Body.String())
	}
	if got := kbSearch(t, api, read, ""); len(got) != 0 {
		t.Fatalf("a proposal wrote %v before approval", got)
	}
	article := kbArticle(t, api, send, map[string]any{"collection_id": collection.ID, "title": "Written by an agent", "body_md": "x"})
	if article.LastChange == nil || article.LastChange.Token == nil || article.LastChange.Token.Label != "Writer" {
		t.Fatalf("agent version = %+v", article.LastChange)
	}
	// An image never waits for approval: the bytes are refused, not filed.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/kb/images", bytes.NewReader(kbPNG))
	req.Header.Set("Content-Type", "image/png")
	req.Header.Set("Authorization", "Bearer "+read)
	if response := serve(api, req); response.Code != http.StatusForbidden || errorCode(t, response) != "files_need_approval" {
		t.Fatalf("read token image status=%d body=%s", response.Code, response.Body.String())
	}
	uploadKBImage(t, api, send, kbPNG, http.StatusCreated)
	// Turning the help center on is an admin's: a send token files a request.
	put := map[string]any{"enabled": true, "title": "Help", "locale": "en"}
	if response := request(t, api, http.MethodPut, "/api/v1/help-center", put, send, nil); response.Code != http.StatusAccepted {
		t.Fatalf("send token help center status=%d body=%s, want 202", response.Code, response.Body.String())
	}
}

func TestHelpCenterSettingsAndContentVersion(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Center", "admin@kb-center.example.com")
	agent := agentSession(t, api, admin, "agent@kb-center.example.com")
	type center struct {
		Enabled        bool    `json:"enabled"`
		Title          string  `json:"title"`
		Locale         string  `json:"locale"`
		ContentVersion int64   `json:"content_version"`
		UpdatedAt      *string `json:"updated_at"`
	}
	var before center
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, agent, http.StatusOK, &before)
	if before.Enabled || before.ContentVersion != 0 || before.UpdatedAt != nil {
		t.Fatalf("new workspace help center = %+v", before)
	}
	if response := request(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "", "locale": "en"}, agent, nil); response.Code != http.StatusForbidden {
		t.Fatalf("agent turned the help center on: status=%d", response.Code)
	}
	if response := request(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "", "locale": "not a tag"}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("bad locale status=%d", response.Code)
	}
	var on center
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Acme Help", "locale": "pt-BR"}, admin, http.StatusOK, &on)
	if !on.Enabled || on.Title != "Acme Help" || on.Locale != "pt-BR" || on.ContentVersion != 1 {
		t.Fatalf("after put = %+v", on)
	}
	collection := kbCollection(t, api, agent, map[string]any{"name": "Docs"})
	kbArticle(t, api, agent, map[string]any{"collection_id": collection.ID, "title": "One", "body_md": "x"})
	var after center
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, agent, http.StatusOK, &after)
	if after.ContentVersion != 3 || !after.Enabled {
		t.Fatalf("content version after a collection and an article = %d, want 3", after.ContentVersion)
	}
}

func TestKnowledgeBaseTrashIsPurgedAfterThirtyDaysWithUnusedImages(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Purge", "admin@kb-purge.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Old"})
	kept := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	orphan := uploadKBImage(t, api, admin, pngImage(1), http.StatusCreated)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Old", "body_md": "![a](/api/v1/kb/images/" + kept.ID + ")"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Live", "body_md": "![a](/api/v1/kb/images/" + kept.ID + ")"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(article.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_articles SET deleted_at=now()-interval '31 days' WHERE id=$1`, article.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET created_at=now()-interval '31 days', last_uploaded_at=now()-interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	var orphanKey string
	if err := pool.QueryRow(ctx, `SELECT storage_key FROM kb_media WHERE id=$1`, orphan.ID).Scan(&orphanKey); err != nil {
		t.Fatal(err)
	}
	purged, err := store.New(pool).WithObjects(storage).PurgeKBTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if purged.Articles != 1 || purged.Images != 1 {
		t.Fatalf("purged = %+v, want one article and one image", purged)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("purged article status=%d", response.Code)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/kb/images/"+kept.ID, nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("image of a live article status=%d", response.Code)
	}
	if body, err := storage.Open(ctx, orphanKey); err == nil {
		body.Close()
		t.Fatal("the unused image's file is still stored")
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+live.ID, nil, admin, http.StatusOK, nil)
}

func TestExportCarriesTheWholeKnowledgeBaseAndNoOtherWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Export", "admin@kb-export.example.com")
	rival := createSession(t, api, service, "KB Export Rival", "admin@kb-export-rival.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Exported guide", "body_md": "![s](" + image.URL + ")", "status": "published"})
	trashed := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Trashed guide", "body_md": "gone"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+trashed.ID+"?updated_at="+url.QueryEscape(trashed.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_articles SET source_html='<p>original</p>', source_meta='{"freshdesk_id": 7}' WHERE id=$1`, article.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md) SELECT workspace_id,id,'de','Exportierte Anleitung','Text' FROM kb_articles WHERE id=$1`, article.ID); err != nil {
		t.Fatal(err)
	}
	rivalCollection := kbCollection(t, api, rival, map[string]any{"name": "Rival guides"})
	kbArticle(t, api, rival, map[string]any{"collection_id": rivalCollection.ID, "title": "rival-only article", "body_md": "rival-only text"})

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	entries := unzip(t, response.Body.Bytes())
	articles := ndjsonLines(t, entries["kb-articles.ndjson"])
	if len(articles) != 2 || articles[0]["source_html"] != "<p>original</p>" || articles[1]["deleted_at"] == nil {
		t.Fatalf("kb-articles.ndjson = %v", articles)
	}
	if ids, _ := articles[0]["image_ids"].([]any); len(ids) != 1 || ids[0] != image.ID {
		t.Fatalf("image_ids = %v", articles[0]["image_ids"])
	}
	for file, want := range map[string]int{"kb-collections.ndjson": 1, "kb-article-versions.ndjson": 3, "kb-article-translations.ndjson": 1, "kb-images.ndjson": 1, "help-center.ndjson": 1} {
		if got := len(ndjsonLines(t, entries[file])); got != want {
			t.Errorf("%s has %d lines, want %d", file, got, want)
		}
	}
	images := ndjsonLines(t, entries["kb-images.ndjson"])
	path, _ := images[0]["path"].(string)
	if path != "kb-images/"+image.ID+".png" || !bytes.Equal(entries[path], kbPNG) || images[0]["storage_key"] != nil {
		t.Fatalf("image record %v, file %d bytes", images[0], len(entries[path]))
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
		Files  []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Counts["kb_articles"] != 2 || manifest.Counts["kb_images"] != 1 || len(manifest.Files) != 1 || manifest.Files[0].Path != path {
		t.Fatalf("manifest counts %v files %v", manifest.Counts, manifest.Files)
	}
	var all bytes.Buffer
	for name, content := range entries {
		all.WriteString(name + "\n")
		all.Write(content)
	}
	for _, foreign := range []string{"rival-only article", "rival-only text", "Rival guides", rivalCollection.ID} {
		if strings.Contains(all.String(), foreign) {
			t.Errorf("export contains %q of another workspace", foreign)
		}
	}
}

// A move into a collection, or an undelete, that runs while the collection is being deleted waits
// for the delete and then refuses: a live article never ends up inside a collection in the trash.
func TestKnowledgeBaseArticleNeverLandsInACollectionBeingDeleted(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Race", "admin@kb-race.example.com")
	home := kbCollection(t, api, admin, map[string]any{"name": "Home"})
	target := kbCollection(t, api, admin, map[string]any{"name": "Target"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": home.ID, "title": "Mover", "body_md": "x"})
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// The delete of the target, between its emptiness check and its commit.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM kb_collections WHERE id=$1 FOR UPDATE`, target.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serve(api, newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": article.UpdatedAt, "collection_id": target.ID}, admin, nil))
	}()
	select {
	case response := <-done:
		t.Fatalf("the move did not wait for the delete: status=%d body=%s", response.Code, response.Body.String())
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_collections SET deleted_at=now(), updated_at=now() WHERE id=$1`, target.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if response := <-done; response.Code != http.StatusBadRequest {
		t.Fatalf("move into a collection deleted meanwhile: status=%d body=%s, want 400", response.Code, response.Body.String())
	}
	var current kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &current)
	if current.CollectionID != home.ID {
		t.Fatalf("article is in %s, want %s", current.CollectionID, home.ID)
	}
}

// A lower tier's knowledge base write waits for a person; once approved it runs as that person, and
// the version names the token that asked and the request.
func TestApprovedKnowledgeBaseRequestsRunAndNameTheirToken(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Approvals", "admin@kb-approvals.example.com")
	read := issueToken(t, api, admin, "Proposer", "read").Token
	send := issueToken(t, api, admin, "Writer", "send").Token
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})

	filed := accepted(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Proposed guide", "body_md": "Steps."}, read)
	if response := approve(t, api, admin, filed.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve article status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles", nil, admin, http.StatusOK, &page)
	if len(page.Items) != 1 || page.Items[0].Title != "Proposed guide" {
		t.Fatalf("articles after approval = %+v", page.Items)
	}
	change := page.Items[0].LastChange
	if change == nil || change.AgentRequestID == nil || *change.AgentRequestID != filed.RequestID || change.Token == nil || change.Token.Label != "Proposer" || change.ChangedBy == nil {
		t.Fatalf("approved version = %+v", change)
	}

	asked := accepted(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Help", "locale": "de"}, send)
	if response := approve(t, api, admin, asked.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve help center status=%d body=%s", response.Code, response.Body.String())
	}
	var center struct {
		Enabled bool   `json:"enabled"`
		Locale  string `json:"locale"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &center)
	if !center.Enabled || center.Locale != "de" {
		t.Fatalf("help center after approval = %+v", center)
	}
}

// Uploading bytes the workspace already holds gives the same image, needs no storage room, and starts
// its 30 days again, so the purge does not delete it under an article about to be saved.
func TestUploadingAnImageAgainKeepsItAndNeedsNoRoom(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := newLimitsFixtureConfigured(t, true, nil, func(config *httpapi.Config) { config.Attachments = storage })
	api, pool, admin := f.api, f.pool, f.adminA
	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET created_at=now()-interval '40 days', last_uploaded_at=now()-interval '40 days' WHERE id=$1`, image.ID); err != nil {
		t.Fatal(err)
	}
	// The trial's storage is full.
	if _, err := pool.Exec(ctx, `UPDATE workspaces SET counted_storage_bytes=2147483648, stored_storage_bytes=2147483648 WHERE id=$1`, f.workspaceA); err != nil {
		t.Fatal(err)
	}
	if again := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated); again.ID != image.ID {
		t.Fatalf("the same bytes gave image %s, want %s", again.ID, image.ID)
	}
	uploadKBImage(t, api, admin, pngImage(2), http.StatusForbidden)
	if _, err := store.New(pool).WithObjects(storage).PurgeKBTrash(ctx); err != nil {
		t.Fatal(err)
	}
	if response := request(t, api, http.MethodGet, image.URL, nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("image uploaded again was purged: status=%d", response.Code)
	}
}
