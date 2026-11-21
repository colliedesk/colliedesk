package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
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
	finishedFreshdeskImport(t, d.pool, slug, "printers.freshdesk.com", `{}`)
	ledgerRow(t, d.pool, slug, "freshdesk:account:777", "kb_article", "7201", d.articleA, "done", "2026-10-20 10:00:00+00")
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

func TestOnlyTheWorkspacesOwnHelpCenterHostsCountAsUses(t *testing.T) {
	d := newDraftDesk(t, func(config *httpapi.Config) { config.HelpCenterDomain = "colliedesk.example" })
	slug := workspaceSlug(t, d.api, d.adminA)
	n := d.articleANumber
	const custom = "help.draft-a.example"
	if _, err := d.pool.Exec(context.Background(), `INSERT INTO help_center_domains (workspace_id, hostname, claim, token, status, verified_at)
		SELECT id, $2, left(md5('uses-claim'), 24), md5('uses-domain'), 'active', now() FROM workspaces WHERE slug=$1`, slug, custom); err != nil {
		t.Fatal(err)
	}
	finishedFreshdeskImport(t, d.pool, slug, "printers.freshdesk.com", `{"old_help_center_domain":"support.draft-a.example"}`)
	ledgerRow(t, d.pool, slug, "freshdesk:account:777", "kb_article", "7201", d.articleA, "done", "2026-10-20 10:00:00+00")
	// Hosts that only look like the help center: the slug as a first label elsewhere, the help center
	// path on another host, an old Freshdesk path on a host the workspace never imported from.
	reply(t, d.api, d.adminA, d.conversationA, fmt.Sprintf("See https://%s.evil.example/help/a/%d-x, https://elsewhere.example/help/%s/a/%d-x, "+
		"https://evil.example/support/solutions/articles/7201-x and https://%s/support/solutions/articles/7201", slug, n, slug, n, testOriginHost))
	if got := articleUses(t, d, "link"); got != "0" {
		t.Fatalf("uses after lookalike hosts = %s, want 0", got)
	}
	for i, link := range []string{
		fmt.Sprintf("https://%s/help/%s/a/%d-printer-paper-jams", testOriginHost, slug, n),
		fmt.Sprintf("https://%s.colliedesk.example/help/a/%d", slug, n),
		fmt.Sprintf("https://%s/a/%d-printer-paper-jams", custom, n),
		"https://printers.freshdesk.com/support/solutions/articles/7201-paper-jams",
		"https://support.draft-a.example/support/solutions/articles/7201",
	} {
		reply(t, d.api, d.adminA, d.conversationA, "See "+link)
		if got, want := articleUses(t, d, "link"), fmt.Sprint(i+1); got != want {
			t.Fatalf("uses after %s = %s, want %s", link, got, want)
		}
	}
}
