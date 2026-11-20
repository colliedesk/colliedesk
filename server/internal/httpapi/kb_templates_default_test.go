package httpapi_test

import (
	"net/http"
	"testing"
)

// A collection's default template starts an article only when the request names no text at all:
// an empty body_md is the Blank choice of the New article menu, and stays empty.
func TestACollectionDefaultStartsAnArticleOnlyWhenNoBodyIsSent(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Default Blank", "admin@kb-default-blank.example.com")
	template := kbTemplate(t, api, admin, map[string]any{"name": "Release note", "body": "## What changed\n\n## Who it affects\n"})
	var collection kbCollectionWithTemplate
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Releases", "default_template_id": template.ID}, admin, http.StatusCreated, &collection)

	started := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Version 2"})
	if started.BodyMD != "## What changed\n\n## Who it affects\n" {
		t.Fatalf("an article sent without a body = %q, want the default template's text", started.BodyMD)
	}
	blank := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Version 3", "body_md": ""})
	if blank.BodyMD != "" {
		t.Fatalf("an article sent with an empty body = %q, want it blank", blank.BodyMD)
	}
}
