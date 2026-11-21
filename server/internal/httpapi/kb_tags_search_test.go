package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

type helpCenterTagsView struct {
	ShowTags bool `json:"show_tags"`
}

func putHelpCenterShowTags(t *testing.T, api http.Handler, admin string, show bool) helpCenterTagsView {
	t.Helper()
	var center helpCenterTagsView
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Tag Answers", "locale": "en", "show_tags": show},
		admin, http.StatusOK, &center)
	return center
}

// A reader finds an article by its tag as by a word of its title, never by a tag only an unpublished
// draft or a draft article has. The page shows the tags only once the team turns show_tags on.
func TestHelpCenterFindsArticlesByTagAndShowsTagsOnlyWhenTurnedOn(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Tag Search", "admin@tag-search.example.com")
	base := "/help/" + workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Tag Answers", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	var live kbTaggedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Returns policy",
		"body_md": "Send the parcel back within 30 days.", "status": "published", "tags": []string{"zephyrine"}}, admin, http.StatusCreated, &live)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Draft notes",
		"body_md": "Unfinished.", "tags": []string{"quillwort"}}, admin, http.StatusCreated, nil)

	wantPage(t, visit(site, base+"/search?q=zephyrine", "192.0.2.80"), http.StatusOK, []string{"1 result for “zephyrine”", "Returns policy"}, nil)
	wantPage(t, visit(site, base+"/search?q=zephyrine+returns", "192.0.2.80"), http.StatusOK, []string{"1 result for “zephyrine returns”", "Returns policy"}, nil)
	wantPage(t, visit(site, base+"/search?q=quillwort", "192.0.2.80"), http.StatusOK, []string{"No results for “quillwort”"}, []string{"Draft notes"})

	var saved kbTaggedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt,
		"tags": []string{"zephyrine", "marram"}}, admin, http.StatusOK, &saved)
	wantPage(t, visit(site, base+"/search?q=marram", "192.0.2.80"), http.StatusOK, []string{"No results for “marram”"}, []string{"Returns policy"})
	page := fmt.Sprintf("%s/a/%d-%s", base, live.Number, live.Slug)
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{"Returns policy"}, []string{"zephyrine"})

	kbPublishDraft(t, api, admin, live.ID, saved.UpdatedAt, nil)
	wantPage(t, visit(site, base+"/search?q=marram", "192.0.2.80"), http.StatusOK, []string{"1 result for “marram”", "Returns policy"}, nil)
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{"Returns policy"}, []string{"zephyrine", "marram"})

	if center := putHelpCenterShowTags(t, api, admin, true); !center.ShowTags {
		t.Fatalf("PUT show_tags true answered show_tags=false")
	}
	var read helpCenterTagsView
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &read)
	if !read.ShowTags {
		t.Fatalf("GET after turning show_tags on = false")
	}
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{">Tags<", ">marram<", ">zephyrine<"}, nil)
	// A save that leaves show_tags out keeps it.
	enableHelpCenter(t, api, admin, "Tag Answers", true)
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{">zephyrine<"}, nil)

	if center := putHelpCenterShowTags(t, api, admin, false); center.ShowTags {
		t.Fatalf("PUT show_tags false answered show_tags=true")
	}
	wantPage(t, visit(site, page, "192.0.2.80"), http.StatusOK, []string{"Returns policy"}, []string{"zephyrine", "marram"})
}

// Help chat retrieval finds an article by its tag; the tag is a word to find it by, not text the
// model reads.
func TestAIRetrievalFindsAnArticleByItsTag(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Tag Retrieval", "admin@tag-retrieval.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	bigKB(t, api, admin, collection)
	var tagged kbTaggedArticle
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection, "title": "Returns policy",
		"body_md": "Send the parcel back within 30 days.", "status": "published", "tags": []string{"zephyrine"}}, admin, http.StatusCreated, &tagged)
	buildAIRetrieval(t, pool)

	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "zephyrine", KBLimit: 5})
	if len(got) == 0 || got[0].KBArticleID != tagged.ID {
		t.Fatalf("tag query = %v, want Returns policy first", retrievedTitles(got))
	}
	if strings.Contains(got[0].Text, "zephyrine") {
		t.Errorf("the model reads the tag as text: %q", got[0].Text)
	}

	// A tag-only change reaches help chat once it is published.
	var saved kbTaggedArticle
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+tagged.ID, map[string]any{"updated_at": tagged.UpdatedAt,
		"tags": []string{"zephyrine", "marram"}}, admin, http.StatusOK, &saved)
	kbPublishDraft(t, api, admin, tagged.ID, saved.UpdatedAt, nil)
	buildAIRetrieval(t, pool)
	if got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "marram", KBLimit: 5}); len(got) == 0 || got[0].KBArticleID != tagged.ID {
		t.Fatalf("published new tag query = %v, want Returns policy first", retrievedTitles(got))
	}
}

// The help center search finds the tags of a search's words through an index, not by building a
// search vector for every tag of the workspace.
func TestHelpCenterTagSearchReadsAnIndex(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Tag Index", "admin@tag-index.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Help"}).ID
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Returns", "body_md": "x", "status": "published", "tags": []string{"zephyrine"}})
	workspace := workspaceID(t, api, admin)
	// A knowledge base with a few thousand tags, of which the search matches one.
	fixtureSQL(t, pool, `INSERT INTO kb_article_tags(workspace_id, article_id, tag) SELECT $1, $2, 'topic' || n FROM generate_series(1, 5000) n`,
		workspace, article.ID)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ANALYZE kb_article_tags`); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, "EXPLAIN "+store.KBTagHitsSQL(), workspace, "zephyrine return")
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if plan := strings.Join(lines, "\n"); !strings.Contains(plan, "kb_article_tags_search_idx") {
		t.Fatalf("the tag search does not use kb_article_tags_search_idx:\n%s", plan)
	}
}
