package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// TestTheHelpChatCitesThePublishedVersionNotTheDraft: the help chat cites an article by its number and
// version and caches its answers on them, so a draft save must leave both on the published text.
func TestTheHelpChatCitesThePublishedVersionNotTheDraft(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "KB Draft Chat", "admin@kb-draft-chat.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Accounts"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset your password",
		"body_md": "Press the pinecone button.", "status": "published"})
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+live.ID, map[string]any{"updated_at": live.UpdatedAt,
		"body_md": "Press the marigold button."}, admin, http.StatusOK, nil)

	var articles []store.ChatKBArticle
	err := database.New(pool).InWorkspace(context.Background(), workspace, func(tx pgx.Tx) error {
		var err error
		articles, err = store.ChatKBArticles(context.Background(), tx, workspace, true)
		return err
	})
	if err != nil {
		t.Fatalf("chat articles: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("chat articles = %+v, want one", articles)
	}
	if got := articles[0]; got.Version != 1 || got.Body != "Press the pinecone button." {
		t.Fatalf("chat reads version %d %q, want version 1 with the published text", got.Version, got.Body)
	}
}
