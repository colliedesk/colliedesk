package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

type chatSourceOut struct {
	Number int64  `json:"number"`
	Title  string `json:"title"`
	Folder string `json:"folder"`
	URL    string `json:"url"`
}

// chatSourcesOf is the sources event of a streamed answer.
func chatSourcesOf(t *testing.T, body string) []chatSourceOut {
	t.Helper()
	lines := bufio.NewScanner(strings.NewReader(body))
	for lines.Scan() {
		var event struct {
			Type    string          `json:"type"`
			Sources []chatSourceOut `json:"sources"`
		}
		if err := json.Unmarshal(lines.Bytes(), &event); err != nil {
			t.Fatalf("line %q: %v", lines.Text(), err)
		}
		if event.Type == "sources" {
			return event.Sources
		}
	}
	t.Fatalf("no sources event in %s", body)
	return nil
}

// Help chat answers from an article kept for help chat but never gives its address or its name: the
// end user cannot open it. An article kept for the team never reaches the model.
func TestHelpChatQuotesItsOwnNotesWithoutLinkingThemAndNeverReadsTeamNotes(t *testing.T) {
	d := newChatDesk(t, "Chat Kept", nil)
	notes := kbCollection(t, d.api, d.admin, map[string]any{"name": "Notes"})
	wrap := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": notes.ID, "title": "Gift wrapping", "status": "published", "visibility": "help_chat",
		"body_md": "Gift wrapping narwhalword costs 2 dollars."})
	kbArticle(t, d.api, d.admin, map[string]any{"collection_id": notes.ID, "title": "Refund overrides", "status": "published", "visibility": "agents",
		"body_md": "Gift wrapping quokkaword refunds are approved by the lead."})
	if _, err := store.New(d.pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.fake.Script(chatReply("Orders ship in 2 days [#"+num(d.article.Number)+"]. ", "Wrapping costs 2 dollars [#"+num(wrap.Number)+"]."))
	response := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "How much is gift wrapping and when does it ship?"})
	cookie := chatCookie(t, response)
	requests := d.fake.Requests()
	if system := requestSystem(requests[len(requests)-1]); !strings.Contains(system, "narwhalword") || strings.Contains(system, "quokkaword") {
		t.Fatalf("prompt = %.2000q", system)
	}
	want := []chatSourceOut{{Number: d.article.Number, Title: "Shipping times", Folder: "Orders", URL: testOrigin + "/help/" + d.slug + "/a/" + num(d.article.Number)},
		{Number: wrap.Number}}
	wantSources := func(where string, got []chatSourceOut) {
		t.Helper()
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("%s sources = %+v, want %+v", where, got, want)
		}
	}
	wantSources("streamed", chatSourcesOf(t, response.Body.String()))

	var session struct {
		Turns []struct {
			Sources []chatSourceOut `json:"sources"`
		} `json:"turns"`
	}
	decode(t, d.call(t, http.MethodGet, "/chat/session", nil, cookie), &session)
	if len(session.Turns) != 1 {
		t.Fatalf("session = %+v", session)
	}
	wantSources("read back", session.Turns[0].Sources)
}

// Help chat links an article only when the help center shows it: not one kept for help chat, not one
// under a parent collection kept for help chat, not one also in a collection kept for help chat. It
// reads an article in a language only from a published translation outside the trash.
func TestHelpChatLinksOnlyWhatTheHelpCenterShowsAndReadsOnlyPublishedTranslations(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Chat Links", "admin@chat-links.example.com")
	workspace := workspaceID(t, api, admin)
	shown := kbCollection(t, api, admin, map[string]any{"name": "Shown"})
	chatTop := kbCollection(t, api, admin, map[string]any{"name": "Chat top", "visibility": "help_chat"})
	chatChild := kbCollection(t, api, admin, map[string]any{"name": "Chat child", "parent_id": chatTop.ID})
	chatShelf := kbCollection(t, api, admin, map[string]any{"name": "Chat shelf", "visibility": "help_chat"})
	article := func(title string, body map[string]any) kbArticleView {
		body["title"], body["status"], body["body_md"] = title, "published", title+" text."
		return kbArticle(t, api, admin, body)
	}
	plain := article("Plain", map[string]any{"collection_id": shown.ID})
	kept := article("Kept", map[string]any{"collection_id": shown.ID, "visibility": "help_chat"})
	underChat := article("Under chat", map[string]any{"collection_id": chatChild.ID})
	alsoChat := article("Also chat", map[string]any{"collection_ids": []string{shown.ID, chatShelf.ID}})

	primary := queryString(t, pool, `SELECT primary_help_center_id($1)::text`, workspace)
	ctx := context.Background()
	for _, step := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO help_center_languages(workspace_id,help_center_id,locale,visible) VALUES($1,$2,'fr',true),($1,$2,'de',true),($1,$2,'es',true)`,
			[]any{workspace, primary}},
		{`INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md,slug,status,published_at,deleted_at) VALUES
			($1,$2,'fr','Simple','Texte.','s','published',now(),NULL),
			($1,$2,'de','Einfach','Text.','e','draft',NULL,NULL),
			($1,$2,'es','Sencillo','Texto.','c','published',now(),now())`, []any{workspace, plain.ID}},
	} {
		if _, err := pool.Exec(ctx, step.sql, step.args...); err != nil {
			t.Fatalf("%s: %v", step.sql, err)
		}
	}

	read := func(locale string) map[string]store.ChatKBArticle {
		t.Helper()
		byTitle := map[string]store.ChatKBArticle{}
		err := database.New(pool).InHelpChat(ctx, workspace, primary, func(tx pgx.Tx) error {
			articles, err := store.ChatKBArticlesIn(ctx, tx, workspace, false, locale)
			for _, a := range articles {
				byTitle[a.Title] = a
			}
			return err
		})
		if err != nil {
			t.Fatalf("chat articles in %q: %v", locale, err)
		}
		return byTitle
	}
	got := read("")
	for title, public := range map[string]bool{plain.Title: true, kept.Title: false, underChat.Title: false, alsoChat.Title: false} {
		if a, ok := got[title]; !ok || a.Public != public {
			t.Errorf("help chat reads %q = %+v (found %v), want public %v", title, a, ok, public)
		}
	}
	if len(got) != 4 {
		t.Errorf("help chat reads %d articles, want 4: %+v", len(got), got)
	}
	for locale, title := range map[string]string{"fr": "Simple", "de": plain.Title, "es": plain.Title} {
		if a, ok := read(locale)[title]; !ok || a.Number != plain.Number || a.Public != true {
			t.Errorf("help chat in %s reads article %d as %q = %+v (found %v), want it public", locale, plain.Number, title, a, ok)
		}
	}
}
