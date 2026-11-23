package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// shelvedChat is a chat desk with a second help center, Hardware, that has a collection of its own
// with one public article and one kept for help chat, and a help chat channel of its own.
type shelvedChat struct {
	chatDesk
	hardware                string
	hardwareChannel         string
	swap, kept              kbArticleView
	hardwareBase, swapTitle string
}

func newShelvedChat(t *testing.T, name string) shelvedChat {
	t.Helper()
	d := newChatDesk(t, name, nil)
	hardware := createHelpCenter(t, d.api, d.admin, "Hardware", "hardware")
	var cables placedCollectionView
	kbCall(t, d.api, http.MethodPost, "/api/v1/kb/collections", map[string]any{"name": "Cables", "help_center_ids": []string{hardware.ID}},
		d.admin, http.StatusCreated, &cables)
	swap := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": cables.ID, "title": "Swap a cable", "status": "published",
		"body_md": "Swap the cable copperword in five minutes."})
	kept := kbArticle(t, d.api, d.admin, map[string]any{"collection_id": cables.ID, "title": "Warranty swaps", "status": "published",
		"visibility": "help_chat", "body_md": "Warranty swaps pangolinword need the receipt."})
	var channel struct {
		ID           string  `json:"id"`
		HelpCenterID *string `json:"help_center_id"`
	}
	kbCall(t, d.api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "help_chat", "config": map[string]any{"greeting": "Hi!"},
		"help_center_id": hardware.ID}, d.admin, http.StatusCreated, &channel)
	if _, err := store.New(d.pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatal(err)
	}
	return shelvedChat{chatDesk: d, hardware: hardware.ID, hardwareChannel: channel.ID, swap: swap, kept: kept,
		hardwareBase: d.base + "/hardware", swapTitle: "Swap a cable"}
}

// onHardware is the chat desk as the Hardware help center's own pages call it.
func (s shelvedChat) onHardware() chatDesk {
	d := s.chatDesk
	d.base = s.hardwareBase
	return d
}

// The help chat of a second help center answers from that help center's articles, its kept ones
// included, and never from the primary's; the primary's chat never reads the second one's articles.
// The article kept for help chat is not a page of either help center, nor found by their search.
func TestHelpChatOfAHelpCenterReadsOnlyThatHelpCentersArticles(t *testing.T) {
	s := newShelvedChat(t, "Chat Shelves")
	hardware := s.onHardware()

	s.fake.Script(chatReply("Swap it [#"+num(s.swap.Number)+"]. ", "Bring the receipt [#"+num(s.kept.Number)+"]. ",
		"Orders ship in 2 days [#"+num(s.article.Number)+"]."))
	response := hardware.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "How do I swap a cable under warranty?"})
	chatEvents(t, response)
	requests := s.fake.Requests()
	system := requestSystem(requests[len(requests)-1])
	if !strings.Contains(system, "copperword") || !strings.Contains(system, "pangolinword") || strings.Contains(system, "Orders ship in 2 days") {
		t.Fatalf("hardware prompt = %.3000q", system)
	}
	sources := chatSourcesOf(t, response.Body.String())
	want := []chatSourceOut{{Number: s.swap.Number, Title: s.swapTitle, Folder: "Cables", URL: testOrigin + s.hardwareBase + "/a/" + num(s.swap.Number)},
		{Number: s.kept.Number}}
	if len(sources) != len(want) || sources[0] != want[0] || sources[1] != want[1] {
		t.Fatalf("hardware sources = %+v, want %+v", sources, want)
	}

	s.fake.Script(chatReply("Orders ship in 2 days [#" + num(s.article.Number) + "]."))
	chatEvents(t, s.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my cable ship?"}))
	requests = s.fake.Requests()
	if system := requestSystem(requests[len(requests)-1]); !strings.Contains(system, "Orders ship in 2 days") ||
		strings.Contains(system, "copperword") || strings.Contains(system, "pangolinword") {
		t.Fatalf("primary prompt = %.3000q", system)
	}

	for _, page := range []string{s.hardwareBase + "/a/" + num(s.kept.Number), s.base + "/a/" + num(s.kept.Number)} {
		wantPage(t, visit(s.site, page, "198.51.100.70"), http.StatusNotFound, nil, []string{"pangolinword", "Warranty swaps"})
	}
	wantPage(t, visit(s.site, s.hardwareBase+"/search?q=pangolinword", "198.51.100.71"), http.StatusOK, nil, []string{"Warranty swaps"})
	wantPage(t, visit(s.site, s.hardwareBase+"/search?q=copperword", "198.51.100.72"), http.StatusOK, []string{s.swapTitle}, nil)
}

// A chat names its channel with ?channel=: a channel that serves another help center is not there.
func TestHelpChatRefusesAChannelOfAnotherHelpCenter(t *testing.T) {
	s := newShelvedChat(t, "Chat Crossed")
	hardware := s.onHardware()
	if response := hardware.call(t, http.MethodGet, "/chat/session?channel="+s.channel, nil); response.Code != http.StatusNotFound {
		t.Fatalf("primary channel on hardware: status=%d %s", response.Code, response.Body.String())
	}
	if response := s.call(t, http.MethodGet, "/chat/session?channel="+s.hardwareChannel, nil); response.Code != http.StatusNotFound {
		t.Fatalf("hardware channel on the primary: status=%d %s", response.Code, response.Body.String())
	}
	if response := hardware.call(t, http.MethodGet, "/chat/session?channel="+s.hardwareChannel, nil); response.Code != http.StatusNoContent {
		t.Fatalf("hardware channel on hardware: status=%d %s", response.Code, response.Body.String())
	}
}

// An article whose collection is on no help center reaches no help chat.
func TestAnArticleOnNoHelpCenterReachesNoHelpChat(t *testing.T) {
	d := newChatDesk(t, "Chat Unshelved", nil)
	loose := kbCollection(t, d.api, d.admin, map[string]any{"name": "Loose"})
	kbArticle(t, d.api, d.admin, map[string]any{"collection_id": loose.ID, "title": "Loose ends", "status": "published",
		"visibility": "help_chat", "body_md": "Loose ends axolotlword are tied on Fridays."})
	if _, err := d.pool.Exec(context.Background(), `DELETE FROM kb_collection_help_centers WHERE workspace_id=$1 AND collection_id=$2`, d.ws, loose.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(d.pool).BuildAIRetrieval(context.Background()); err != nil {
		t.Fatal(err)
	}
	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	chatEvents(t, d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When are loose ends tied?"}))
	requests := d.fake.Requests()
	if system := requestSystem(requests[len(requests)-1]); strings.Contains(system, "axolotlword") || !strings.Contains(system, "Orders ship in 2 days") {
		t.Fatalf("prompt = %.3000q", system)
	}
}
