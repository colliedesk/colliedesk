package mcpserver_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

func base64Of(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

// An AI agent puts a file under an article in two calls: upload it, then set the article's list.
// A published article lists it in its draft, a translation at once; a read token can do neither.
func TestSendTokenUploadsAFileAndListsItUnderAnArticle(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB File Tools", "admin@kb-file-tools.example.com")
	session := e.connect(w.adminTier, "")

	form := pdfsample.PDF("Fill in the warranty form", 2048)
	uploaded := e.ok(session, "colliedesk_kb_file_upload", map[string]any{"name": "Warranty form.pdf", "data_base64": base64Of(form)})
	fileID := at(t, uploaded, "media", "id").(string)
	if at(t, uploaded, "media", "kind") != "file" || at(t, uploaded, "media", "content_type") != "application/pdf" || at(t, uploaded, "media", "team_text", "name") != "Warranty form.pdf" {
		t.Fatalf("upload = %v", uploaded)
	}
	refusedType := e.fails(session, "colliedesk_kb_file_upload", map[string]any{"name": "page.html", "data_base64": base64Of([]byte("<html><body>hi</body></html>"))})
	if !strings.Contains(refusedType, "PDF") {
		t.Fatalf("an HTML file = %q, want the refusal to list the allowed types", refusedType)
	}
	sheet := e.ok(session, "colliedesk_kb_file_upload", map[string]any{"name": "parts.csv", "data_base64": base64Of([]byte("sku,name\nA-1,Kettle\n"))})
	sheetID := at(t, sheet, "media", "id").(string)
	if files := at(t, e.ok(session, "colliedesk_kb_media_list", map[string]any{"kind": "file"}), "media").([]any); len(files) != 2 {
		t.Fatalf("files in the library = %v, want 2", files)
	}

	collection := at(t, e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Returns"}), "collection", "id").(string)
	article := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Returns policy", "body": "Send it back.", "status": "published"})
	articleID := at(t, article, "article", "id").(string)
	set := e.ok(session, "colliedesk_kb_article_attachments_set", map[string]any{"article_id": articleID, "media_ids": []string{sheetID, fileID}})
	if got := at(t, set, "article", "attachments").([]any); len(got) != 0 {
		t.Fatalf("live files of a published article = %v, want none until the draft is published", got)
	}
	if at(t, set, "article", "draft", "attachments", 0, "team_text", "name") != "parts.csv" || at(t, set, "article", "draft", "attachments", 1, "team_text", "name") != "Warranty form.pdf" ||
		at(t, set, "article", "draft", "attachments", 1, "size") != float64(len(form)) {
		t.Fatalf("draft files = %v", set)
	}
	published := e.ok(session, "colliedesk_kb_article_draft_publish", map[string]any{"article_id": articleID, "updated_at": at(t, set, "article", "updated_at")})
	read := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID})
	if at(t, read, "article", "attachments", 0, "id") != sheetID || at(t, read, "article", "attachments", 1, "content_type") != "application/pdf" || at(t, read, "article", "draft") != nil {
		t.Fatalf("after publish (%v) the article = %v", published, read)
	}

	e.ok(session, "colliedesk_help_center_languages_set", map[string]any{"languages": []any{map[string]any{"locale": "fr", "visible": false}}})
	e.ok(session, "colliedesk_kb_translation_create", map[string]any{"article_id": articleID, "locale": "fr", "title": "Retours", "body": "Renvoyez-le."})
	translated := e.ok(session, "colliedesk_kb_article_attachments_set", map[string]any{"article_id": articleID, "locale": "fr", "media_ids": []string{fileID}})
	if at(t, translated, "translation", "attachments", 0, "team_text", "name") != "Warranty form.pdf" {
		t.Fatalf("translation files = %v", translated)
	}

	image := e.ok(session, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64Of(kbPNG), "name": "Logo"})
	if message := e.fails(session, "colliedesk_kb_article_attachments_set", map[string]any{"article_id": articleID, "media_ids": []string{at(t, image, "image", "id").(string)}}); !strings.Contains(message, "kind=file") {
		t.Fatalf("an image as a file = %q, want how to find the files", message)
	}

	reader := e.connect(w.read, "")
	e.refused(reader, "colliedesk_kb_article_attachments_set", map[string]any{"article_id": articleID, "media_ids": []string{}})
	e.refused(reader, "colliedesk_kb_file_upload", map[string]any{"name": "a.txt", "data_base64": base64Of([]byte("hello there"))})
}
