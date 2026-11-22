package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type kbListColumnsView struct {
	Columns []string `json:"columns"`
	Default bool     `json:"default"`
}

func TestKnowledgeBaseListColumnsAreEachMembersChoice(t *testing.T) {
	f := newTeamFixture(t)
	columns := func(token string) kbListColumnsView {
		t.Helper()
		var me struct {
			Columns kbListColumnsView `json:"kb_list_columns"`
		}
		req := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
		response := serve(f.api, req)
		validateContractResponse(t, req, response)
		decode(t, response, &me)
		return me.Columns
	}
	put := func(token string, value any) kbListColumnsView {
		t.Helper()
		req := newRequest(t, http.MethodPut, "/api/v1/agents/me/kb-list-columns", map[string]any{"columns": value}, token, nil)
		response := serve(f.api, req)
		if response.Code != http.StatusOK {
			t.Fatalf("PUT kb-list-columns %v status=%d body=%s", value, response.Code, response.Body.String())
		}
		validateContractResponse(t, req, response)
		var out kbListColumnsView
		decode(t, response, &out)
		return out
	}
	const defaults = "[status author updated] true"
	for _, token := range []string{f.admin, f.ann, f.otherAdmin} {
		if got := fmt.Sprint(columns(token).Columns, " ", columns(token).Default); got != defaults {
			t.Fatalf("a member who never chose sees %s, want %s", got, defaults)
		}
	}

	chosen := put(f.ann, []string{"votes", "status", "created", "languages"})
	if got := fmt.Sprint(chosen.Columns, " ", chosen.Default); got != "[votes status created languages] false" {
		t.Fatalf("PUT answered %s", got)
	}
	if got := fmt.Sprint(columns(f.ann).Columns); got != "[votes status created languages]" {
		t.Fatalf("ann reads back %s, want her order kept", got)
	}
	if got := fmt.Sprint(columns(f.admin).Columns, " ", columns(f.admin).Default); got != defaults {
		t.Fatalf("the admin sees %s after ann chose, want the default", got)
	}
	if got := fmt.Sprint(columns(f.otherAdmin).Columns, " ", columns(f.otherAdmin).Default); got != defaults {
		t.Fatalf("the other workspace sees %s, want the default", got)
	}
	// None is a choice too: only the Article column is left.
	if got := put(f.ann, []string{}); len(got.Columns) != 0 || got.Default {
		t.Fatalf("an empty choice answered %+v", got)
	}
	if got := put(f.ann, nil); fmt.Sprint(got.Columns, " ", got.Default) != defaults {
		t.Fatalf("null answered %+v, want the default back", got)
	}

	for _, body := range []any{map[string]any{}, map[string]any{"columns": "status"}, map[string]any{"columns": []any{"title"}},
		map[string]any{"columns": []any{"status", "status"}}, map[string]any{"columns": []any{"article"}}, map[string]any{"columns": []any{1}}} {
		response := request(t, f.api, http.MethodPut, "/api/v1/agents/me/kb-list-columns", body, f.ann, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("PUT %v status=%d, want 400", body, response.Code)
		}
	}
	if response := request(t, f.api, http.MethodPut, "/api/v1/agents/me/kb-list-columns", map[string]any{"columns": []any{"title"}}, f.ann, nil); !strings.Contains(response.Body.String(), "status, collection, author, tags, languages, created, updated, votes") {
		t.Fatalf("an unknown column must name the ones there are: %s", response.Body.String())
	}
}

func TestTheArticleListCountsEachArticlesVotes(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "List Votes", "admin@list-votes.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "List help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	voted := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Voted on", "status": "published", "body_md": "Open Billing."})
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Not voted on", "status": "published", "body_md": "Open Plans."})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, voted.Number, voted.Slug)
	for index, answer := range []string{"yes", "yes", "no"} {
		postVote(site, page+"/vote", url.Values{"helpful": {answer}}, testOrigin, fmt.Sprintf("192.0.2.%d", 120+index))
	}

	req := newRequest(t, http.MethodGet, "/api/v1/kb/articles", nil, admin, nil)
	response := serve(api, req)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, req, response)
	var list struct {
		Items []struct {
			Title string `json:"title"`
			Votes struct {
				Helpful    int `json:"helpful"`
				NotHelpful int `json:"not_helpful"`
			} `json:"votes"`
		} `json:"items"`
	}
	decode(t, response, &list)
	got := map[string]string{}
	for _, item := range list.Items {
		got[item.Title] = fmt.Sprintf("%d/%d", item.Votes.Helpful, item.Votes.NotHelpful)
	}
	if fmt.Sprint(got) != "map[Not voted on:0/0 Voted on:2/1]" {
		t.Fatalf("votes per article = %v, want 2 helpful and 1 not on the voted one, none on the other", got)
	}
}
