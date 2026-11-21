package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

// articleUses counts the replies that used the article in the way kind says.
func articleUses(t *testing.T, d draftDesk, kind string) string {
	t.Helper()
	return queryString(t, d.pool, `SELECT count(*)::text FROM kb_article_uses WHERE kb_article_id=$1 AND kind=$2`, d.articleA, kind)
}

func TestASentReplyLinkingAnArticleCountsOneUseAndANoteNone(t *testing.T) {
	d := newDraftDesk(t)
	slug := workspaceSlug(t, d.api, d.adminA)
	link := fmt.Sprintf("https://%s/help/%s/a/%d-printer-paper-jams", testOriginHost, slug, d.articleANumber)
	kbCall(t, d.api, http.MethodPost, "/api/v1/conversations/"+d.conversationA+"/messages", map[string]any{"kind": "note", "body": "Send her " + link}, d.adminA, http.StatusCreated, nil)
	if got := articleUses(t, d, "link"); got != "0" {
		t.Fatalf("uses after a note = %s, want 0", got)
	}
	reply(t, d.api, d.adminA, d.conversationA, "Hi Ada, see "+link+" (the same steps: "+link+").")
	if got := articleUses(t, d, "link"); got != "1" {
		t.Fatalf("uses after a reply = %s, want 1", got)
	}
	// The old Freshdesk address of the imported article counts as the article.
	ledgerRow(t, d.pool, slug, "freshdesk:printers.freshdesk.com", "kb_article", "7201", d.articleA, "done", "2026-10-20 10:00:00+00")
	reply(t, d.api, d.adminA, d.conversationA, "Also https://printers.freshdesk.com/support/solutions/articles/7201-paper-jams")
	if got := articleUses(t, d, "link"); got != "2" {
		t.Fatalf("uses after an old address = %s, want 2", got)
	}
	// Another workspace's reply naming A's address is not a use of A's article.
	reply(t, d.api, d.adminB, d.convoB, "See "+link)
	if got := articleUses(t, d, "link"); got != "2" {
		t.Fatalf("uses after B's reply = %s, want 2", got)
	}
}

func TestADraftThatQuotedAnArticleCountsAUseOnlyWhenSent(t *testing.T) {
	d := newDraftDesk(t)
	d.fake.Script(draftAnswer("Hi Ada, open tray two and pull the paper out gently.", []string{"kb1"}, ""))
	queued := d.ask(t, d.adminA, d.conversationA, "Tell her how to clear it")
	d.run(t)
	draft := d.get(t, d.adminA, d.conversationA, queued.ID)
	if len(draft.Sources) != 1 || draft.Sources[0].ID != d.articleA {
		t.Fatalf("sources = %+v", draft.Sources)
	}
	if got := articleUses(t, d, "ai_source"); got != "0" {
		t.Fatalf("uses of a draft not sent = %s, want 0", got)
	}
	sent := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+d.conversationA+"/messages", map[string]any{"kind": "outgoing", "body": *draft.Text, "ai_draft_id": draft.ID}, d.adminA, nil)
	if sent.Code != http.StatusCreated {
		t.Fatalf("send the draft: %d %s", sent.Code, sent.Body.String())
	}
	if got := articleUses(t, d, "ai_source"); got != "1" {
		t.Fatalf("uses of the sent draft = %s, want 1", got)
	}
	var conversation string
	if err := d.pool.QueryRow(context.Background(), `SELECT conversation_id::text FROM kb_article_uses WHERE kb_article_id=$1`, d.articleA).Scan(&conversation); err != nil || conversation != d.conversationA {
		t.Fatalf("use conversation = %q (%v), want %s", conversation, err, d.conversationA)
	}
}
