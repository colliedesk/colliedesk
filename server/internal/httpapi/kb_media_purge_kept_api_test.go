package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbMediaKeptView struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		KeptBy  []struct {
			MediaID       string `json:"media_id"`
			ReplacementID string `json:"replacement_id"`
			ToMediaID     string `json:"to_media_id"`
			Articles      []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"articles"`
		} `json:"kept_by"`
	} `json:"error"`
}

// The image an edited copy replaced in an article stays as its earlier version: it may go to the trash,
// but a purge refuses it (and purges nothing else in that call), naming the article and the replacement,
// the trash shows no date for it and the daily purge skips it, until the replacement is undone.
func TestKBMediaPurgeKeepsTheEarlierVersionOfAReplacementUntilUndone(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Purge Kept", "admin@kb-purge-kept.example.com")
	ctx := context.Background()
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	original := uploadKBMedia(t, api, admin, pngImage(80), nil)
	annotated := uploadKBMedia(t, api, admin, pngImage(81), nil)
	spare := uploadKBMedia(t, api, admin, pngImage(82), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Setup guide", "body_md": "See ![a](" + original.URL + ")"})
	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": article.ID}, admin, http.StatusOK, &done)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+original.ID, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+spare.ID, nil, admin, http.StatusOK, nil)

	var kept kbMediaKeptView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{spare.ID, original.ID}}, admin, http.StatusConflict, &kept)
	if kept.Error.Code != "media_kept" || !strings.Contains(kept.Error.Message, `"Setup guide"`) || !strings.Contains(kept.Error.Message, done.ID) {
		t.Fatalf("refusal = %+v", kept.Error)
	}
	if len(kept.Error.KeptBy) != 1 || kept.Error.KeptBy[0].MediaID != original.ID || kept.Error.KeptBy[0].ReplacementID != done.ID ||
		kept.Error.KeptBy[0].ToMediaID != annotated.ID || len(kept.Error.KeptBy[0].Articles) != 1 ||
		kept.Error.KeptBy[0].Articles[0].ID != article.ID || kept.Error.KeptBy[0].Articles[0].Title != "Setup guide" {
		t.Fatalf("kept_by = %+v", kept.Error.KeptBy)
	}
	var trash struct {
		Items []struct {
			ID      string  `json:"id"`
			PurgeAt *string `json:"purge_at"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/trash", nil, admin, http.StatusOK, &trash)
	dates := map[string]bool{}
	for _, item := range trash.Items {
		dates[item.ID] = item.PurgeAt != nil
	}
	if len(dates) != 2 || dates[original.ID] || !dates[spare.ID] {
		t.Fatalf("trash = %+v, want both, a date for the spare only", trash.Items)
	}

	if _, err := pool.Exec(ctx, `UPDATE kb_media SET deleted_at=now()-interval '31 days' WHERE deleted_at IS NOT NULL`); err != nil {
		t.Fatal(err)
	}
	purged, err := store.New(pool).WithObjects(storage).PurgeKBTrash(ctx)
	if err != nil || purged.Images != 1 {
		t.Fatalf("daily purge = %+v (%v), want the spare only", purged, err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_media WHERE id=$1`, original.ID).Scan(&left); err != nil || left != 1 {
		t.Fatalf("original rows after the daily purge = %d (%v)", left, err)
	}

	// Undo puts the image back in the article; once that article is gone too, the purge deletes it.
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, nil)
	var current kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &current)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(current.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+original.ID, nil, admin, http.StatusOK, nil)
	var result kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{original.ID}}, admin, http.StatusOK, &result)
	if result.status(original.ID) != "purged" {
		t.Fatalf("purge after undo = %+v", result)
	}
}
