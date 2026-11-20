package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

// Delete forever keeps an image that a live template still names: it waits in the trash, as the help
// center's logo does, so starting an article from the template never shows a missing image.
func TestDeleteForeverKeepsAnImageALiveTemplateNames(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Template Media", "admin@kb-template-media.example.com")
	image := uploadKBMedia(t, api, admin, pngImage(61), nil)
	kbTemplate(t, api, admin, map[string]any{"name": "Screenshot steps", "body": "1. Open settings ![s](" + image.URL + ")"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+image.ID, nil, admin, http.StatusOK, nil)
	var key string
	if err := pool.QueryRow(context.Background(), `SELECT storage_key FROM kb_media WHERE id=$1`, image.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}

	var result kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{image.ID}}, admin, http.StatusOK, &result)
	if result.status(image.ID) != "in_use" {
		t.Fatalf("purge of an image a template names = %+v, want in_use", result)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+image.ID+"/details", nil, admin, http.StatusOK, nil)
	if body, err := storage.Open(context.Background(), key); err != nil {
		t.Fatalf("the file of an image a template names is gone: %v", err)
	} else {
		body.Close()
	}
}
