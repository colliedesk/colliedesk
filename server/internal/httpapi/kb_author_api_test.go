package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

type kbAuthorView struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	Left    bool   `json:"left"`
}

type kbAuthoredArticle struct {
	kbArticleView
	AuthorAgentID *string       `json:"author_agent_id"`
	Author        *kbAuthorView `json:"author"`
	HasDraft      bool          `json:"has_draft"`
}

type kbAuthorVersions struct {
	Items []struct {
		Kind         string `json:"kind"`
		AuthorChange *struct {
			From *kbAuthorView `json:"from"`
			To   *kbAuthorView `json:"to"`
		} `json:"author_change"`
	} `json:"items"`
}

// Crediting a published article to someone else applies at once, never waits in the draft, and the
// history says from whom to whom. An author who left the team still shows, marked as having left.
func TestChangingAnArticleAuthorIsRecordedInItsHistory(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Authors", "admin@kb-authors.example.com")
	adminID := agentID(t, api, admin)
	writerID := agentID(t, api, agentSession(t, api, admin, "writer@kb-authors.example.com"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	created := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})

	var article kbAuthoredArticle
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+created.ID, nil, admin, http.StatusOK, &article)
	if article.Author == nil || article.Author.AgentID != adminID || article.Author.Name == "" || article.Author.Left {
		t.Fatalf("new article author = %+v, want the admin who wrote it", article.Author)
	}

	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": article.UpdatedAt, "author_agent_id": writerID}, admin, http.StatusOK, &article)
	if article.AuthorAgentID == nil || *article.AuthorAgentID != writerID || article.Author == nil || article.Author.Name == "" || article.HasDraft {
		t.Fatalf("after the change author=%v %+v has_draft=%v, want the writer at once and no draft", article.AuthorAgentID, article.Author, article.HasDraft)
	}
	if article.LastChange == nil || article.LastChange.Kind != "author_change" {
		t.Fatalf("last change = %+v, want author_change", article.LastChange)
	}

	var versions kbAuthorVersions
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+created.ID+"/versions", nil, admin, http.StatusOK, &versions)
	newest := versions.Items[0]
	if newest.Kind != "author_change" || newest.AuthorChange == nil || newest.AuthorChange.From == nil || newest.AuthorChange.To == nil ||
		newest.AuthorChange.From.AgentID != adminID || newest.AuthorChange.To.AgentID != writerID {
		t.Fatalf("newest version = %+v, want author_change from the admin to the writer", newest)
	}
	if len(versions.Items) != 2 || versions.Items[1].AuthorChange != nil {
		t.Fatalf("versions = %+v, want the create without an author change, then the author change", versions.Items)
	}

	// The same author again adds nothing.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": article.UpdatedAt, "author_agent_id": writerID}, admin, http.StatusOK, &article)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+created.ID+"/versions", nil, admin, http.StatusOK, &versions)
	if len(versions.Items) != 2 {
		t.Fatalf("setting the same author again added a version: %d versions", len(versions.Items))
	}

	// The writer leaves the team: the article still names them, marked as having left.
	request(t, api, http.MethodPatch, "/api/v1/agents/"+writerID, map[string]any{"active": false}, admin, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+created.ID, nil, admin, http.StatusOK, &article)
	if article.Author == nil || article.Author.AgentID != writerID || !article.Author.Left {
		t.Fatalf("author after leaving = %+v, want the writer with left=true", article.Author)
	}
}

// Only an active member of this workspace's team can be credited. Someone of another workspace, a
// member who left and an id nobody has get the same answer, so it never tells that an id exists.
func TestAnArticleAuthorMustBeAnActiveMemberOfTheTeam(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Author Checks", "admin@kb-author-checks.example.com")
	leftID := agentID(t, api, agentSession(t, api, admin, "left@kb-author-checks.example.com"))
	request(t, api, http.MethodPatch, "/api/v1/agents/"+leftID, map[string]any{"active": false}, admin, nil)
	foreignID := agentID(t, api, createSession(t, api, service, "KB Author Elsewhere", "admin@kb-author-elsewhere.example.com"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	created := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})

	var messages []string
	for name, id := range map[string]string{"another workspace": foreignID, "left the team": leftID, "unknown": "00000000-0000-4000-8000-000000000000"} {
		response := kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+created.ID, map[string]any{"updated_at": created.UpdatedAt, "author_agent_id": id}, admin, http.StatusBadRequest, nil)
		if code := errorCode(t, response); code != "invalid_request" {
			t.Fatalf("%s: code=%s, want invalid_request", name, code)
		}
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, body.Error.Message)
	}
	if messages[0] != messages[1] || messages[1] != messages[2] {
		t.Fatalf("refusals differ, so they tell which ids exist: %q", messages)
	}
	if article := kbGetArticle(t, api, admin, created.ID); article.LastChange == nil || article.LastChange.Kind != "create" {
		t.Fatalf("a refused author change was recorded: %+v", article.LastChange)
	}
}
