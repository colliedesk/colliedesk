package mcpserver_test

import (
	"encoding/base64"
	"testing"
)

// An agent puts an edited copy of an image in one article: the other article keeps the image, the
// copy's details list the image as its earlier version, and undo takes that away again.
func TestSendTokenReplacesAnImageInOneArticle(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Replace One Tools Desk", "admin@kb-replace-one-tools.example.com")
	session := e.connect(w.send, "")
	image := e.ok(session, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(kbPNG), "name": "Settings"})
	imageID, imageURL := at(t, image, "image", "id").(string), at(t, image, "image", "url").(string)
	copied := e.ok(session, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(otherPNG()), "name": "Settings (annotated)"})
	copyID, copyURL := at(t, copied, "image", "id").(string), at(t, copied, "image", "url").(string)
	collection := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Setup"}), "collection", "id").(string)
	edited := at(t, e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Edited", "body": "![a](" + imageURL + ")"}), "article", "id").(string)
	other := at(t, e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Other", "body": "![b](" + imageURL + ")"}), "article", "id").(string)

	replaced := e.ok(session, "colliedesk_kb_media_replace", map[string]any{"media_id": imageID, "with_media_id": copyID, "article_id": edited})
	if ids := at(t, replaced, "article_ids").([]any); len(ids) != 1 || ids[0] != edited {
		t.Fatalf("replacement = %v, want only %s", replaced, edited)
	}
	if body := at(t, e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": edited}), "article", "team_text", "body"); body != "![a]("+copyURL+")" {
		t.Fatalf("edited body = %v", body)
	}
	if body := at(t, e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": other}), "article", "team_text", "body"); body != "![b]("+imageURL+")" {
		t.Fatalf("other body = %v", body)
	}
	details := e.ok(session, "colliedesk_kb_media_get", map[string]any{"media_id": copyID, "thumbnail": false})
	if at(t, details, "earlier_versions", 0, "media_id") != imageID || at(t, details, "earlier_versions", 0, "replacement_id") != at(t, replaced, "replacement_id") ||
		at(t, details, "earlier_versions", 0, "url") != imageURL || at(t, details, "earlier_versions", 0, "team_text", "name") != "Settings" {
		t.Fatalf("earlier versions = %v", details)
	}
	e.ok(session, "colliedesk_kb_media_replace_undo", map[string]any{"replacement_id": at(t, replaced, "replacement_id")})
	if versions := at(t, e.ok(session, "colliedesk_kb_media_get", map[string]any{"media_id": copyID, "thumbnail": false}), "earlier_versions").([]any); len(versions) != 0 {
		t.Fatalf("earlier versions after undo = %v", versions)
	}
}
