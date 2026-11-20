package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestExportKeepsUnpublishedChanges: a draft is work a person has not published yet, so the export
// carries it next to the published article, or it would be lost on a move to another server.
func TestExportKeepsUnpublishedChanges(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Draft Export", "admin@kb-draft-export.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Accounts"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password",
		"body_md": "Press the pinecone button.", "status": "published"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt,
		"title": "Change your password", "body_md": "Press the marigold button."}, admin, http.StatusOK, nil)

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	entries := unzip(t, response.Body.Bytes())
	drafts := ndjsonLines(t, entries["kb-article-drafts.ndjson"])
	if len(drafts) != 1 {
		t.Fatalf("kb-article-drafts.ndjson = %v, want one draft", drafts)
	}
	draft := drafts[0]
	if draft["article_id"] != live.ID || draft["locale"] != nil || draft["title"] != "Change your password" || draft["slug"] != "reset-your-password" ||
		draft["body_md"] != "Press the marigold button." || draft["collection_id"] != nil || draft["extra_collection_ids"] != nil ||
		draft["updated_by_agent_id"] == nil || draft["created_at"] == nil || draft["updated_at"] == nil {
		t.Fatalf("draft record = %v", draft)
	}
	if articles := ndjsonLines(t, entries["kb-articles.ndjson"]); len(articles) != 1 || articles[0]["body_md"] != "Press the pinecone button." {
		t.Fatalf("kb-articles.ndjson = %v, want the published text", articles)
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("manifest.json: %v", err)
	}
	if manifest.Counts["kb_article_drafts"] != 1 {
		t.Fatalf("counts[kb_article_drafts] = %d, want 1", manifest.Counts["kb_article_drafts"])
	}
}
