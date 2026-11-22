package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbLadder makes a collection under top for each level below it, down to the deepest level allowed,
// and returns the deepest one.
func kbLadder(t *testing.T, api http.Handler, admin, top, name string) string {
	t.Helper()
	parent := top
	for level := 2; level <= domain.MaxKBDepth; level++ {
		parent = kbCollection(t, api, admin, map[string]any{"name": fmt.Sprintf("%s level %d", name, level), "parent_id": parent}).ID
	}
	return parent
}

// A top collection kept for the team or for help chat keeps an article at the deepest level as
// restricted as itself: the API says so and names the top collection, the help center never shows it,
// and help chat reads only the one kept for help chat.
func TestARestrictedTopCollectionRestrictsArticlesAtEveryLevel(t *testing.T) {
	d := newChatDesk(t, "Deep Kept", nil)
	team := kbCollection(t, d.api, d.admin, map[string]any{"name": "Team ladder", "visibility": "agents"})
	chat := kbCollection(t, d.api, d.admin, map[string]any{"name": "Chat ladder", "visibility": "help_chat"})
	teamArticle := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": kbLadder(t, d.api, d.admin, team.ID, "Team"),
		"title": "Override steps", "status": "published", "body_md": "Refund quokkaword overrides need the lead."})
	chatArticle := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": kbLadder(t, d.api, d.admin, chat.ID, "Chat"),
		"title": "Wrapping prices", "status": "published", "body_md": "Gift wrapping narwhalword costs 2 dollars."})

	for _, want := range []struct {
		article          kbArticleView
		effective, topID string
	}{{teamArticle, "agents", team.ID}, {chatArticle, "help_chat", chat.ID}} {
		got := kbVisibleArticleGet(t, d.api, d.admin, want.article.ID)
		if got.Visibility != "everyone" || got.EffectiveVisibility != want.effective || got.VisibilityFrom == nil || got.VisibilityFrom.ID != want.topID {
			t.Errorf("article %s = %+v, want everyone, effectively %s, from %s", want.article.Title, got, want.effective, want.topID)
		}
	}

	base := "/help/" + d.slug
	secrets := []string{"quokkaword", "narwhalword", "Override steps", "Wrapping prices", teamArticle.Slug, chatArticle.Slug}
	for _, path := range []string{base, base + "/sitemap.xml", base + "/llms.txt", base + "/search?q=override", base + "/search?q=wrapping"} {
		wantPage(t, visit(d.site, path, "192.0.2.160"), http.StatusOK, nil, secrets)
	}
	missing := visit(d.site, fmt.Sprintf("%s/a/%d-gone", base, chatArticle.Number+100), "192.0.2.160")
	for _, article := range []kbArticleView{teamArticle, chatArticle} {
		page := fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug)
		if response := visit(d.site, page, "192.0.2.160"); response.Code != http.StatusNotFound || response.Body.String() != missing.Body.String() {
			t.Errorf("GET %s = %d, want the 404 of a missing article; body=%s", page, response.Code, response.Body.String())
		}
	}

	if _, err := store.New(d.pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.fake.Script(chatReply("Wrapping costs 2 dollars."))
	d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "How much do gift wrapping and refund overrides cost?"})
	requests := d.fake.Requests()
	if system := requestSystem(requests[len(requests)-1]); !strings.Contains(system, "narwhalword") || strings.Contains(system, "quokkaword") {
		t.Fatalf("help chat prompt = %.2000q, want the help chat article and not the team one", system)
	}
}
