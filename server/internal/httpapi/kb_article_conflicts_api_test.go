package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type kbConflictsView struct {
	ArticleID string `json:"article_id"`
	Open      int    `json:"open"`
	Conflicts []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
		Reply  struct {
			MessageID string `json:"message_id"`
			Excerpt   string `json:"excerpt"`
			Author    *struct {
				Name string `json:"name"`
			} `json:"author"`
			Conversation *struct {
				ID      string `json:"id"`
				Subject string `json:"subject"`
			} `json:"conversation"`
		} `json:"reply"`
		ClearedAt *time.Time `json:"cleared_at"`
		ClearedBy *struct {
			Name string `json:"name"`
		} `json:"cleared_by"`
		ClearedWithToken *struct {
			Label string `json:"label"`
		} `json:"cleared_with_token"`
	} `json:"conflicts"`
}

type kbConflictCount struct {
	ID            string `json:"id"`
	OpenConflicts int    `json:"open_conflicts"`
}

// flagKBArticle stores what the worker stores when the small model says the newest reply on
// conversation contradicts article.
func flagKBArticle(t *testing.T, pool *pgxpool.Pool, conversation, article, reason string) {
	t.Helper()
	message := queryString(t, pool, `SELECT id::text FROM messages WHERE conversation_id=$1 AND kind='outgoing' ORDER BY created_at DESC, id DESC LIMIT 1`, conversation)
	queryString(t, pool, `INSERT INTO kb_article_conflicts (workspace_id, article_id, message_id, reason)
		SELECT workspace_id, $2, id, $3 FROM messages WHERE id=$1 RETURNING id::text`, message, article, reason)
}

func TestAReplyThatContradictsAnArticleFlagsItUntilAnEditorClearsIt(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.November, 23, 9, 0, 0, 0, time.UTC)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = clock.Now })
	admin := createSession(t, api, service, "Conflict Desk", "admin@conflict-desk.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password", "status": "published", "body_md": "The reset link works for 24 hours."})
	other := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Change a plan", "status": "published", "body_md": "Open Billing."})
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@conflict-desk.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Reset link expired"})
	reply(t, api, admin, conversation, "The reset link works for 48 hours. Write to ada@conflict-desk.example.com if it fails.")
	flagKBArticle(t, pool, conversation, article.ID, `The reply says "48 hours", the article says "24 hours".`)
	path := "/api/v1/kb/articles/" + article.ID + "/conflicts"

	var read kbConflictCount
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &read)
	if read.OpenConflicts != 1 {
		t.Fatalf("article open_conflicts = %d, want 1", read.OpenConflicts)
	}
	var list struct {
		Items []kbConflictCount `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles", nil, admin, http.StatusOK, &list)
	counts := map[string]int{}
	for _, item := range list.Items {
		counts[item.ID] = item.OpenConflicts
	}
	if counts[article.ID] != 1 || counts[other.ID] != 0 {
		t.Fatalf("list open_conflicts = %v, want 1 for the flagged article and 0 for the other", counts)
	}

	var conflicts kbConflictsView
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &conflicts)
	if conflicts.ArticleID != article.ID || conflicts.Open != 1 || len(conflicts.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want one open conflict of the article", conflicts)
	}
	got := conflicts.Conflicts[0]
	if got.Reason != `The reply says "48 hours", the article says "24 hours".` ||
		got.Reply.Excerpt != "The reset link works for 48 hours. Write to [email removed] if it fails." ||
		got.Reply.Author == nil || got.Reply.Author.Name != "admin@conflict-desk.example.com" ||
		got.Reply.Conversation == nil || got.Reply.Conversation.ID != conversation || got.Reply.Conversation.Subject != "Reset link expired" ||
		got.ClearedAt != nil || got.ClearedBy != nil || got.ClearedWithToken != nil {
		t.Fatalf("conflict = %+v, want the masked reply, its author and conversation, not cleared", got)
	}

	// A reader of the knowledge base who may not read conversations sees the reply, not where it was sent.
	kbReader := customToken(t, api, admin, "KB reader", "kb_articles:read")
	kbCall(t, api, http.MethodGet, path, nil, kbReader, http.StatusOK, &conflicts)
	if len(conflicts.Conflicts) != 1 || conflicts.Conflicts[0].Reply.Conversation != nil || conflicts.Conflicts[0].Reply.Excerpt == "" {
		t.Fatalf("conflicts for a token without conversations:read = %+v, want the reply without its conversation", conflicts)
	}
	refused(t, api, http.MethodPost, path+"/clear", nil, kbReader, "kb_articles:update")

	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update")
	clock.set(time.Date(2026, time.November, 23, 10, 30, 0, 0, time.UTC))
	var cleared kbConflictsView
	kbCall(t, api, http.MethodPost, path+"/clear", nil, editor, http.StatusOK, &cleared)
	if cleared.Open != 0 || len(cleared.Conflicts) != 1 {
		t.Fatalf("after clear = %+v, want none open and the cleared one kept", cleared)
	}
	done := cleared.Conflicts[0]
	if done.ClearedAt == nil || !done.ClearedAt.Equal(clock.Now()) || done.ClearedBy == nil || done.ClearedBy.Name != "admin@conflict-desk.example.com" ||
		done.ClearedWithToken == nil || done.ClearedWithToken.Label != "Editor" {
		t.Fatalf("cleared = %+v, want cleared at 10:30 by the admin through the Editor token", done)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &read)
	if read.OpenConflicts != 0 {
		t.Fatalf("article open_conflicts after clear = %d, want 0", read.OpenConflicts)
	}
	// A repeated press clears nothing more and keeps who cleared it first.
	clock.set(time.Date(2026, time.November, 23, 11, 0, 0, 0, time.UTC))
	kbCall(t, api, http.MethodPost, path+"/clear", nil, admin, http.StatusOK, &cleared)
	if len(cleared.Conflicts) != 1 || cleared.Conflicts[0].ClearedWithToken == nil || !cleared.Conflicts[0].ClearedAt.Equal(*done.ClearedAt) {
		t.Fatalf("after a repeat = %+v, want the first clear kept", cleared.Conflicts)
	}
}

func TestAnArticlesConflictsStayInItsWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Conflict Own", "admin@conflict-own.example.com")
	stranger := createSession(t, api, service, "Conflict Other", "admin@conflict-other.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Own guide", "status": "published", "body_md": "a"})
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Bo", "email": "bo@conflict-own.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Guide"})
	reply(t, api, admin, conversation, "The guide is wrong about this.")
	flagKBArticle(t, pool, conversation, article.ID, "The reply says one thing, the article another.")
	path := "/api/v1/kb/articles/" + article.ID + "/conflicts"

	kbCall(t, api, http.MethodGet, path, nil, stranger, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, path+"/clear", nil, stranger, http.StatusNotFound, nil)
	var conflicts kbConflictsView
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &conflicts)
	if conflicts.Open != 1 || conflicts.Conflicts[0].ClearedAt != nil {
		t.Fatalf("another workspace's clear reached this article: %+v", conflicts)
	}
	for _, missing := range []string{"/api/v1/kb/articles/00000000-0000-4000-8000-000000000000/conflicts", "/api/v1/kb/articles/not-an-id/conflicts"} {
		kbCall(t, api, http.MethodGet, missing, nil, admin, http.StatusNotFound, nil)
		kbCall(t, api, http.MethodPost, missing+"/clear", nil, admin, http.StatusNotFound, nil)
	}
}
