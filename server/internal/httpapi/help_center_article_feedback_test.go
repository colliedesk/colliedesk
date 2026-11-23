package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

type helpCenterFeedbackSetting struct {
	FeedbackConversations bool `json:"feedback_conversations"`
}

// feedbackConversation is a conversation article feedback opened: its subject, contact address,
// first message's body and envelope, tags, and whether the article's author was asked on it.
type feedbackConversation struct {
	ID, Subject, Contact, Body, Via, Authentication, Tags string
	AskedAuthor                                           bool
}

// With article feedback turned on, a "No" that leaves a line and an email address opens one conversation
// from that address, with the article and the masked line, and asks the article's author on it. Off (the
// default) the page asks for no address and nothing but the vote is stored. Turning it on lets anyone on
// the internet open a conversation, so it needs channels:update as well as help_center:update.
func TestANotHelpfulVoteWithAnAddressOpensAConversationTheAuthorIsAskedOn(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	ctx := context.Background()
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Feedback Alpha", "admin@feedback-alpha.example.com")
	slug, workspace := workspaceSlug(t, api, admin), workspaceID(t, api, admin)
	enableHelpCenter(t, api, admin, "Feedback help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Reset a password", "status": "published", "body_md": "Open Settings."})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	author := queryString(t, pool, `SELECT author_agent_id::text FROM kb_articles WHERE id=$1`, article.ID)
	opened := func() []feedbackConversation {
		t.Helper()
		rows, err := pool.Query(ctx, `SELECT c.id::text, c.subject, ct.email, m.body, m.email_meta->>'via', m.email_meta->>'authentication',
				(SELECT string_agg(g.name, ',' ORDER BY g.name) FROM conversation_tags x JOIN tags g ON g.id=x.tag_id WHERE x.conversation_id=c.id),
				EXISTS (SELECT 1 FROM note_mentions n JOIN messages note ON note.id=n.message_id WHERE note.conversation_id=c.id AND note.kind='note' AND n.agent_id=$2::uuid)
			FROM conversations c JOIN contacts ct ON ct.id=c.contact_id
			JOIN LATERAL (SELECT body, email_meta FROM messages WHERE conversation_id=c.id AND kind='incoming' ORDER BY created_at LIMIT 1) m ON true
			WHERE c.workspace_id=$1 AND c.channel='article_feedback' ORDER BY c.created_at`, workspace, author)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []feedbackConversation
		for rows.Next() {
			var row feedbackConversation
			if err := rows.Scan(&row.ID, &row.Subject, &row.Contact, &row.Body, &row.Via, &row.Authentication, &row.Tags, &row.AskedAuthor); err != nil {
				t.Fatal(err)
			}
			out = append(out, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	contacts := func(address string) string {
		return queryString(t, pool, `SELECT count(*)::text FROM contacts WHERE workspace_id=$1 AND lower(email)=$2`, workspace, address)
	}
	feedback := func(line, email string) url.Values {
		return url.Values{"helpful": {"no"}, "missing": {line}, "email": {email}}
	}

	var setting helpCenterFeedbackSetting
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, admin, http.StatusOK, &setting)
	if setting.FeedbackConversations {
		t.Fatal("a new help center turns article feedback into conversations")
	}
	off := visit(site, page+"?voted=no", "192.0.2.71")
	wantPage(t, off, http.StatusOK, []string{"What was missing?"}, []string{`name="email"`})
	policy := off.Header().Get("Content-Security-Policy")
	wantSeeOther(t, postVote(site, page+"/vote", feedback("Step 2 is wrong", "early@reader.example"), testOrigin, "192.0.2.71"), page+"?voted=sent#helpful")
	// Off, the address is not read at all: a wrong one is no error.
	wantSeeOther(t, postVote(site, page+"/vote", feedback("Step 4 too", "not an address"), testOrigin, "192.0.2.76"), page+"?voted=sent#helpful")
	if got := opened(); len(got) != 0 || contacts("early@reader.example") != "0" {
		t.Fatalf("with the setting off: conversations %+v, contacts %s; want none", got, contacts("early@reader.example"))
	}

	settings := issueCustomToken(t, api, admin, "Help center only", auth.PermHelpCenterRead, auth.PermHelpCenterUpdate).Token
	turnOn := map[string]any{"enabled": true, "title": "Feedback help", "locale": "en", "feedback_conversations": true}
	refused(t, api, http.MethodPut, "/api/v1/help-center", turnOn, settings, auth.PermChannelsUpdate)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", turnOn, admin, http.StatusOK, &setting)
	if !setting.FeedbackConversations {
		t.Fatal("after turning article feedback on, the help center says it is off")
	}
	// A save that leaves it out keeps it.
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Feedback help", "locale": "en"}, settings, http.StatusOK, &setting)
	if !setting.FeedbackConversations {
		t.Fatal("saving the title alone turned article feedback off")
	}

	on := visit(site, page+"?voted=no", "192.0.2.72")
	wantPage(t, on, http.StatusOK, []string{`<input type="email" id="helpful-email" name="email"`, `autocomplete="email"`}, []string{"<script src", "<script>"})
	if got := on.Header().Get("Content-Security-Policy"); got != policy {
		t.Fatalf("the CSP with the address field = %q, want the page's %q", got, policy)
	}

	if response := postVote(site, page+"/vote", feedback("Step 3 is missing", "not an address"), testOrigin, "192.0.2.72"); response.Code != http.StatusBadRequest ||
		!strings.Contains(response.Body.String(), "valid email address") {
		t.Fatalf("a wrong address: %d %s, want 400 saying so", response.Code, response.Body.String())
	}
	// An address without a line says nothing to act on: the vote counts, no conversation opens.
	wantSeeOther(t, postVote(site, page+"/vote", feedback(" ", "quiet@reader.example"), testOrigin, "192.0.2.73"), page+"?voted=no#helpful")

	line := feedback("Step 3 is missing, call me on 0912 345 678", "Reader@Example.com")
	sent := postVote(site, page+"/vote", line, testOrigin, "192.0.2.72")
	wantSeeOther(t, sent, page+"?voted=sent#helpful")
	// The same form sent again (a reload, a second press) opens nothing more.
	wantSeeOther(t, postVote(site, page+"/vote", line, testOrigin, "192.0.2.72", chatCookie(t, sent)), page+"?voted=sent#helpful")

	got := opened()
	if len(got) != 1 {
		t.Fatalf("conversations opened = %+v, want one", got)
	}
	conversation := got[0]
	if conversation.Subject != "Article feedback: Reset a password" || conversation.Contact != "reader@example.com" ||
		conversation.Via != "help_center" || conversation.Authentication != "none" || conversation.Tags != "article-feedback" || !conversation.AskedAuthor {
		t.Fatalf("conversation = %+v", conversation)
	}
	for _, want := range []string{"Article: Reset a password " + testOrigin + page, "Step 3 is missing, call me on [number removed]"} {
		if !strings.Contains(conversation.Body, want) {
			t.Fatalf("message body %q lacks %q", conversation.Body, want)
		}
	}
	if strings.Contains(conversation.Body, "0912") {
		t.Fatalf("message body keeps the number: %q", conversation.Body)
	}
	if contacts("quiet@reader.example") != "0" {
		t.Fatal("an address without a line was stored")
	}
	linked := queryString(t, pool, `SELECT count(*)::text FROM kb_article_votes WHERE article_id=$1 AND conversation_id=$2`, article.ID, conversation.ID)
	if linked != "1" {
		t.Fatalf("votes linked to the conversation = %s, want 1", linked)
	}
	// The export carries the setting and which vote opened which conversation.
	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", exported.Code, exported.Body.String())
	}
	entries := unzip(t, exported.Body.Bytes())
	if centers := ndjsonLines(t, entries["help-center.ndjson"]); len(centers) != 1 || centers[0]["feedback_conversations"] != true {
		t.Fatalf("exported help center = %+v, want feedback_conversations true", centers)
	}
	exportedLinks := 0
	for _, vote := range ndjsonLines(t, entries["kb-article-votes.ndjson"]) {
		if _, ok := vote["conversation_id"]; !ok {
			t.Fatalf("exported vote %+v has no conversation_id", vote)
		}
		if vote["conversation_id"] == conversation.ID {
			exportedLinks++
		}
	}
	if exportedLinks != 1 {
		t.Fatalf("exported votes naming the conversation = %d, want 1", exportedLinks)
	}
	// The team answers by email, like any other conversation.
	connectEmail(t, api, admin, "support@feedback-alpha.example.com")
	reply := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation.ID+"/messages", map[string]any{"kind": "outgoing", "body": "Step 3 is added now."}, admin, nil)
	if reply.Code != http.StatusCreated {
		t.Fatalf("reply: %d %s, want 201", reply.Code, reply.Body.String())
	}
	if to := queryString(t, pool, `SELECT k.email FROM channel_outbox o JOIN messages m ON m.id=o.message_id
		JOIN conversations c ON c.id=m.conversation_id JOIN contacts k ON k.id=c.contact_id WHERE o.workspace_id=$1 AND c.id=$2`, workspace, conversation.ID); to != "reader@example.com" {
		t.Fatalf("the reply is addressed to %q, want the reader", to)
	}
}

// One network opens at most 5 conversations from article feedback in 10 minutes: the help center is open
// to anyone, and each one adds a contact. Past that the vote still counts and the reader sees the same page.
func TestArticleFeedbackFromOneNetworkIsCapped(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Feedback Echo", "admin@feedback-echo.example.com")
	slug, workspace := workspaceSlug(t, api, admin), workspaceID(t, api, admin)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Echo help", "locale": "en", "feedback_conversations": true}, admin, http.StatusOK, nil)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Billing", "status": "published", "body_md": "a"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	for i := 1; i <= 6; i++ {
		form := url.Values{"helpful": {"no"}, "missing": {"Nothing about refunds"}, "email": {fmt.Sprintf("reader%d@echo.example", i)}}
		wantSeeOther(t, postVote(site, page+"/vote", form, testOrigin, "192.0.2.77"), page+"?voted=sent#helpful")
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1 AND channel='article_feedback'`, workspace); got != "5" {
		t.Fatalf("conversations from one network = %s, want 5", got)
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM contacts WHERE workspace_id=$1 AND email='reader6@echo.example'`, workspace); got != "0" {
		t.Fatalf("the sixth address was stored as a contact (%s)", got)
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM kb_article_votes WHERE article_id=$1 AND NOT helpful`, article.ID); got != "6" {
		t.Fatalf("not-helpful votes = %s, want all 6", got)
	}
}

// Article feedback of one workspace never opens a conversation in another, and the setting is the
// workspace's own: B's help center with it off stores no address while A's is on.
func TestArticleFeedbackStaysInItsWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	adminA := createSession(t, api, service, "Feedback Bravo", "admin@feedback-bravo.example.com")
	adminB := createSession(t, api, service, "Feedback Charlie", "admin@feedback-charlie.example.com")
	slugB, workspaceA, workspaceB := workspaceSlug(t, api, adminB), workspaceID(t, api, adminA), workspaceID(t, api, adminB)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "A help", "locale": "en", "feedback_conversations": true}, adminA, http.StatusOK, nil)
	enableHelpCenter(t, api, adminB, "B help", true)
	collection := kbCollection(t, api, adminB, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, adminB, map[string]any{"collection_id": collection.ID, "title": "Export data", "status": "published", "body_md": "a"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slugB, article.Number, article.Slug)

	wantPage(t, visit(site, page+"?voted=no", "192.0.2.74"), http.StatusOK, nil, []string{`name="email"`})
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"no"}, "missing": {"No CSV"}, "email": {"cross@reader.example"}}, testOrigin, "192.0.2.74"), page+"?voted=sent#helpful")
	for _, workspace := range []string{workspaceA, workspaceB} {
		if got := queryString(t, pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1 AND channel='article_feedback'`, workspace); got != "0" {
			t.Fatalf("workspace %s has %s feedback conversations, want 0", workspace, got)
		}
	}
	if got := queryString(t, pool, `SELECT count(*)::text FROM contacts WHERE lower(email)='cross@reader.example'`); got != "0" {
		t.Fatalf("contacts with the address = %s, want 0", got)
	}
}

// On a Cloud trial, a reply to article feedback is refused like one to a web form: anyone can type any
// address into the help center, so it must not become a way to make the trial write to it.
func TestTrialRepliesToArticleFeedbackAreRefused(t *testing.T) {
	f := newLimitsFixture(t, true)
	site := helpCenterSite(f.api)
	admin := createSession(t, f.api, f.billing.service, "Feedback Delta", "admin@feedback-delta.example.com")
	connectEmail(t, f.api, admin, "support@feedback-delta.example.com")
	slug, workspace := workspaceSlug(t, f.api, admin), workspaceID(t, f.api, admin)
	kbCall(t, f.api, http.MethodPut, "/api/v1/help-center", map[string]any{"enabled": true, "title": "Delta help", "locale": "en", "feedback_conversations": true}, admin, http.StatusOK, nil)
	collection := kbCollection(t, f.api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, f.api, admin, map[string]any{"collection_id": collection.ID, "title": "Invoices", "status": "published", "body_md": "a"})
	page := fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug)
	wantSeeOther(t, postVote(site, page+"/vote", url.Values{"helpful": {"no"}, "missing": {"Where is the PDF?"}, "email": {"anyone@victim.example"}}, testOrigin, "192.0.2.75"), page+"?voted=sent#helpful")
	conversation := queryString(t, f.pool, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND channel='article_feedback'`, workspace)
	wantStatus(t, f.reply(t, admin, conversation), http.StatusForbidden, restrictedCode, "trial reply to article feedback")
}
