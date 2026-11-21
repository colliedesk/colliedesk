package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// Results reach members without a knowledge base permission, so its article rows name only articles
// the help center shows: an article kept for the team or for help chat
// that an AI draft quoted is not listed, nor counted in the total.
func TestResultsListOnlyArticlesTheHelpCenterShows(t *testing.T) {
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.UTC)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = func() time.Time { return now } })
	admin := createSession(t, api, service, "Shown Desk", "admin@shown.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	public := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "Use the link."})
	team := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refund overrides", "status": "published", "visibility": "agents", "body_md": "Call the duty lead."})
	chat := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Escalation notes", "status": "published", "visibility": "help_chat", "body_md": "Ask in the channel."})

	contact := queryString(t, pool, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'Casey','casey@customer.example') RETURNING id::text`, workspace)
	conversation := openConversationAt(t, pool, workspace, contact, "2026-11-18T10:00:00Z", "open", false)
	reply := queryString(t, pool, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'outgoing','Here you go','2026-11-18T12:00:00Z') RETURNING id::text`, workspace, conversation)
	for _, article := range []string{public.ID, team.ID, chat.ID} {
		seedSQL(t, pool, `INSERT INTO kb_article_uses(workspace_id,kb_article_id,conversation_id,message_id,kind,created_at) VALUES($1,$2,$3,$4,'ai_source','2026-11-18T12:00:00Z')`, workspace, article, conversation, reply)
	}

	var page helpCenterBlock
	decode(t, request(t, api, http.MethodGet, "/api/v1/results", nil, admin, nil), &page)
	month := page.HelpCenter.Windows["30d"]
	if month.ArticlesTotal != 1 || len(month.Articles) != 1 || month.Articles[0].Title != "Reset a password" || month.Articles[0].Uses != 1 {
		t.Fatalf("articles = %+v (total %d), want only Reset a password with 1 use", month.Articles, month.ArticlesTotal)
	}
}
