package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// The export carries every file of the library with its bytes, the lists articles keep of them in
// order, and the list a published article's draft waits to publish, so a move to another server
// loses no attachment.
func TestExportCarriesFilesAndTheListsOfArticles(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB File Export", "admin@kb-file-export.example.com")
	manual := pdfsample.PDF("Installation manual", 3000)
	first := uploadKBFile(t, api, admin, "Manual.pdf", manual)
	second := uploadKBFile(t, api, admin, "Parts.csv", []byte("part,count\nscrew,4\n"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Setup"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Install it", "body_md": "Read the manual."})
	listed := putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+article.ID, []string{second.ID, first.ID}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"status": "published", "updated_at": listed.UpdatedAt}, admin, http.StatusOK, nil)
	putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+article.ID, []string{first.ID}, http.StatusOK)

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	entries := unzip(t, response.Body.Bytes())
	files := ndjsonLines(t, entries["kb-files.ndjson"])
	if len(files) != 2 {
		t.Fatalf("kb-files.ndjson = %v, want the two files", files)
	}
	byID := map[string]map[string]any{}
	for _, file := range files {
		byID[file["id"].(string)] = file
	}
	if file := byID[first.ID]; file == nil || file["name"] != "Manual.pdf" || file["content_type"] != "application/pdf" || file["path"] != "kb-files/"+first.ID+".pdf" ||
		file["size"] != float64(len(manual)) || file["sha256"] == nil || file["created_at"] == nil || file["deleted_at"] != nil {
		t.Fatalf("the manual's record = %v", file)
	}
	if !bytes.Equal(entries["kb-files/"+first.ID+".pdf"], manual) || string(entries["kb-files/"+second.ID+".csv"]) != "part,count\nscrew,4\n" {
		t.Fatal("the files' bytes are not in the export")
	}
	if images := ndjsonLines(t, entries["kb-images.ndjson"]); len(images) != 0 {
		t.Fatalf("a file is in kb-images.ndjson: %v", images)
	}

	lists := ndjsonLines(t, entries["kb-article-attachments.ndjson"])
	if len(lists) != 2 || lists[0]["article_id"] != article.ID || lists[0]["locale"] != nil || lists[0]["media_id"] != second.ID || lists[0]["position"] != float64(0) ||
		lists[1]["media_id"] != first.ID || lists[1]["position"] != float64(1) || lists[0]["created_at"] == nil {
		t.Fatalf("kb-article-attachments.ndjson = %v, want Parts.csv then Manual.pdf", lists)
	}
	drafts := ndjsonLines(t, entries["kb-article-drafts.ndjson"])
	if len(drafts) != 1 || len(drafts[0]["attachment_ids"].([]any)) != 1 || drafts[0]["attachment_ids"].([]any)[0] != first.ID {
		t.Fatalf("kb-article-drafts.ndjson = %v, want the draft listing only the manual", drafts)
	}

	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	if manifest.Counts["kb_files"] != 2 || manifest.Counts["kb_article_attachments"] != 2 {
		t.Fatalf("counts = %v, want kb_files 2 and kb_article_attachments 2", manifest.Counts)
	}
}
