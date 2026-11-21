package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

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
