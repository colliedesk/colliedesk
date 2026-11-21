package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

type kbEarlierVersionView struct {
	ReplacementID string `json:"replacement_id"`
	MediaID       string `json:"media_id"`
	URL           string `json:"url"`
	ReplacedAt    string `json:"replaced_at"`
}

type kbMediaWithEarlierView struct {
	ID              string                 `json:"id"`
	EarlierVersions []kbEarlierVersionView `json:"earlier_versions"`
}

// An edited copy of an image goes in one article only: the others that show the image keep it, the new
// item's details list the image as its earlier version while the replacement stands, and undo puts the
// image back in that article.
func TestKBMediaReplaceInOneArticleLeavesTheOthersAndUndoes(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Replace One", "admin@kb-replace-one.example.com")
	ctx := context.Background()
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	original := uploadKBMedia(t, api, admin, pngImage(70), nil)
	annotated := uploadKBMedia(t, api, admin, pngImage(71), nil)
	edited := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Edited", "body_md": "See ![a](" + original.URL + ")"})
	other := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Other", "status": "published", "body_md": "Also ![b](" + original.URL + ")"})
	versions := func(id string) (n int) {
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_article_versions WHERE article_id=$1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	otherVersions := versions(other.ID)

	// A member who cannot publish may change a draft, even while a published article shows the image.
	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update", "kb_media:read", "kb_media:update")
	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": edited.ID}, editor, http.StatusOK, &done)
	if len(done.ArticleIDs) != 1 || done.ArticleIDs[0] != edited.ID || done.To != annotated.ID {
		t.Fatalf("replacement = %+v, want only %s", done, edited.ID)
	}
	var got kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+edited.ID, nil, admin, http.StatusOK, &got)
	if got.BodyMD != "See ![a]("+annotated.URL+")" {
		t.Fatalf("edited body = %q", got.BodyMD)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+other.ID, nil, admin, http.StatusOK, &got)
	if got.BodyMD != "Also ![b]("+original.URL+")" || versions(other.ID) != otherVersions {
		t.Fatalf("other article changed: %q, %d versions (was %d)", got.BodyMD, versions(other.ID), otherVersions)
	}

	var details kbMediaWithEarlierView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+annotated.ID+"/details", nil, admin, http.StatusOK, &details)
	if len(details.EarlierVersions) != 1 || details.EarlierVersions[0].ReplacementID != done.ID || details.EarlierVersions[0].MediaID != original.ID ||
		details.EarlierVersions[0].URL != original.URL || details.EarlierVersions[0].ReplacedAt == "" {
		t.Fatalf("earlier versions = %+v", details.EarlierVersions)
	}

	var undone kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, editor, http.StatusOK, &undone)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+edited.ID, nil, admin, http.StatusOK, &got)
	if got.BodyMD != "See ![a]("+original.URL+")" || undone.UndoneAt == nil {
		t.Fatalf("after undo body = %q, undone = %v", got.BodyMD, undone.UndoneAt)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+annotated.ID+"/details", nil, admin, http.StatusOK, &details)
	if len(details.EarlierVersions) != 0 {
		t.Fatalf("earlier versions after undo = %+v", details.EarlierVersions)
	}

	// The published article needs kb_articles:publish.
	refused(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": other.ID}, editor, "kb_articles:publish")
	// An article that does not show the item is refused, and nothing is recorded.
	plain := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Plain", "body_md": "No image"})
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": plain.ID}, admin, http.StatusBadRequest, nil)
	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_media_replacements WHERE from_media_id=$1`, original.ID).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("replacements recorded = %d (%v), want 1", recorded, err)
	}
	// A file is replaced in place for every article, never for one.
	file := uploadKBFile(t, api, admin, "Form.pdf", pdfsample.PDF("One", 2048))
	next := uploadKBFile(t, api, admin, "Form v2.pdf", pdfsample.PDF("Two", 3000))
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+file.ID+"/replace", map[string]any{"with_media_id": next.ID, "article_id": edited.ID}, admin, http.StatusBadRequest, nil)
}

// Another workspace's article answers as if it did not exist.
func TestKBMediaReplaceInOneArticleNeverCrossesWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Replace One Alpha", "admin@kb-replace-one-alpha.example.com")
	beta := createSession(t, api, service, "KB Replace One Beta", "admin@kb-replace-one-beta.example.com")
	betaOld := uploadKBMedia(t, api, beta, pngImage(72), nil)
	betaNew := uploadKBMedia(t, api, beta, pngImage(73), nil)
	alphaImage := uploadKBMedia(t, api, alpha, pngImage(74), nil)
	collection := kbCollection(t, api, alpha, map[string]any{"name": "Docs"})
	alphaArticle := kbArticle(t, api, alpha, map[string]any{"collection_id": collection.ID, "title": "Alpha", "body_md": "![a](" + alphaImage.URL + ")"})
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+betaOld.ID+"/replace", map[string]any{"with_media_id": betaNew.ID, "article_id": alphaArticle.ID}, beta, http.StatusNotFound, nil)
	var details kbMediaWithEarlierView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+betaNew.ID+"/details", nil, beta, http.StatusOK, &details)
	if details.EarlierVersions == nil || len(details.EarlierVersions) != 0 {
		t.Fatalf("earlier versions = %#v, want an empty list", details.EarlierVersions)
	}
}
