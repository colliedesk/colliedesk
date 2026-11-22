package httpapi_test

import (
	"net/http"
	"slices"
	"testing"
)

// The export keeps an article's published tags, the tags waiting in its draft and every author change
// with the author before and after, and whether the help center shows tags, so a restored workspace
// loses none of them.
func TestTheExportKeepsArticleTagsAndAuthorChanges(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Tag Export", "admin@tag-export.example.com")
	adminID := agentID(t, api, admin)
	writerID := agentID(t, api, agentSession(t, api, admin, "writer@tag-export.example.com"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	created := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published", "tags": []string{"refund", "billing"}})

	var article kbAuthoredArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": created.UpdatedAt, "tags": []string{"money"}}, admin, http.StatusOK, &article)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": article.UpdatedAt, "author_agent_id": writerID}, admin, http.StatusOK, &article)
	putHelpCenterShowTags(t, api, admin, true)

	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	articles := ndjsonLines(t, entries["kb-articles.ndjson"])
	if len(articles) != 1 || articles[0]["author_agent_id"] != writerID || !sameStrings(articles[0]["tags"], []string{"billing", "refund"}) {
		t.Fatalf("articles = %+v, want the writer as author and the published tags billing, refund", articles)
	}
	drafts := ndjsonLines(t, entries["kb-article-drafts.ndjson"])
	if len(drafts) != 1 || !sameStrings(drafts[0]["tags"], []string{"money"}) {
		t.Fatalf("drafts = %+v, want the draft tags money", drafts)
	}
	versions := ndjsonLines(t, entries["kb-article-versions.ndjson"])
	last := versions[len(versions)-1]
	if last["kind"] != "author_change" || last["author_from_agent_id"] != adminID || last["author_to_agent_id"] != writerID {
		t.Fatalf("last version = %+v, want author_change from the admin to the writer", last)
	}
	if center := ndjsonLines(t, entries["help-center.ndjson"]); len(center) != 1 || center[0]["show_tags"] != true {
		t.Fatalf("help center = %+v, want show_tags true", center)
	}
	if first := versions[0]; first["author_from_agent_id"] != nil || first["author_to_agent_id"] != nil {
		t.Fatalf("first version = %+v, want no author change on the create", first)
	}
}

func sameStrings(value any, want []string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		got = append(got, text)
	}
	return slices.Equal(got, want)
}
