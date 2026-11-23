package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

// kbMailDesk is a hosted-cloud API whose attachment storage sits in a folder the test can count.
type kbMailDesk struct {
	api     http.Handler
	service *auth.Service
	pool    *pgxpool.Pool
	files   string
}

func newKBMailDesk(t *testing.T) kbMailDesk {
	t.Helper()
	root := t.TempDir()
	storage, err := attachments.NewLocalDisk(root)
	if err != nil {
		t.Fatal(err)
	}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		hostedConfig(config)
		config.Attachments = storage
	})
	return kbMailDesk{api: api, service: service, pool: pool, files: root}
}

// storedFiles counts the files written to the desk's attachment storage.
func (d kbMailDesk) storedFiles(t *testing.T) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(d.files, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			count++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// alignedStamp is the receiving server's verdict that mail From domain passed DKIM and DMARC.
func alignedStamp(domain string) string {
	return "mx.example.net; spf=pass smtp.mailfrom=" + domain + "; dkim=pass header.d=" + domain + " header.s=s1; dmarc=pass (p=reject) header.from=" + domain
}

var kbPartsCSV = []byte("sku,name\nA-1,Kettle\n")

// kbForward is a member's forward of a mail to the workspace's knowledge base address: an HTML body
// with a style sheet and an inline image, and a CSV attachment.
func kbForward(slug, from, messageID string, stamps ...string) string {
	headers := []string{"X-Original-To: kb@" + slug + "." + hostedDomain}
	for _, stamp := range stamps {
		headers = append(headers, "Authentication-Results: "+stamp)
	}
	headers = append(headers, "From: Ann Member <"+from+">", "To: kb@"+slug+"."+hostedDomain, "Subject: Fwd: How to reset the kettle",
		"Message-ID: <"+messageID+">", "MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="outer"`)
	return mail(headers, "--outer\nContent-Type: text/html; charset=utf-8\n\n"+
		"<html><head><style>p { color: red }</style></head><body><p>Open <b>Settings</b> and press <a href=\"https://example.com/reset\">Reset</a>.</p>"+
		"<p><img src=\"cid:logo@customer.example\"></p></body></html>\n"+
		"--outer\nContent-Type: text/csv; name=\"parts.csv\"\nContent-Disposition: attachment; filename=\"parts.csv\"\nContent-Transfer-Encoding: base64\n\n"+
		base64.StdEncoding.EncodeToString(kbPartsCSV)+"\n--outer--\n")
}

type kbInboundResult struct {
	Status    string  `json:"status"`
	Reason    *string `json:"reason"`
	ArticleID *string `json:"article_id"`
}

func kbInbound(t *testing.T, api http.Handler, raw string) kbInboundResult {
	t.Helper()
	response := inbound(t, api, platformSecret, raw)
	if response.Code != http.StatusOK {
		t.Fatalf("inbound status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, inboundRequest(t, raw, platformSecret), response)
	var result kbInboundResult
	decode(t, response, &result)
	return result
}

// A member's authenticated forward to kb@ becomes a draft article: the subject without Fwd: is its
// title, the HTML body its Markdown, the attachment a file of the article. No conversation is opened,
// and the same mail delivered again adds nothing.
func TestAMembersForwardToTheKBAddressBecomesADraftArticle(t *testing.T) {
	desk := newKBMailDesk(t)
	admin := createSession(t, desk.api, desk.service, "KB Mail", "admin@kb-mail.example.com")
	collection := kbCollection(t, desk.api, admin, map[string]any{"name": "Kettles"})
	ann := agentSession(t, desk.api, admin, "ann@kb-mail.example.com")
	raw := kbForward("kb-mail", "ann@kb-mail.example.com", "forward-1@kb-mail.example.com", alignedStamp("kb-mail.example.com"))

	result := kbInbound(t, desk.api, raw)
	if result.Status != "created" || result.ArticleID == nil {
		t.Fatalf("result = %+v, want created with an article", result)
	}
	var article kbArticleView
	kbCall(t, desk.api, http.MethodGet, "/api/v1/kb/articles/"+*result.ArticleID, nil, ann, http.StatusOK, &article)
	if article.Title != "How to reset the kettle" || article.Status != "draft" || article.CollectionID != collection.ID {
		t.Fatalf("article = %+v, want draft %q in %s", article, "How to reset the kettle", collection.ID)
	}
	for _, want := range []string{"Open **Settings** and press [Reset](https://example.com/reset)."} {
		if !strings.Contains(article.BodyMD, want) {
			t.Errorf("body lacks %q:\n%s", want, article.BodyMD)
		}
	}
	for _, unwanted := range []string{"color", "cid:", "<"} {
		if strings.Contains(article.BodyMD, unwanted) {
			t.Errorf("body keeps %q:\n%s", unwanted, article.BodyMD)
		}
	}
	var files kbArticleWithFiles
	kbCall(t, desk.api, http.MethodGet, "/api/v1/kb/articles/"+*result.ArticleID, nil, ann, http.StatusOK, &files)
	if len(files.Attachments) != 1 || files.Attachments[0].Name != "parts.csv" || files.Attachments[0].ContentType != "text/csv" || files.Attachments[0].Size != 20 {
		t.Fatalf("attachments = %+v, want parts.csv text/csv of 20 bytes", files.Attachments)
	}
	author := queryString(t, desk.pool, `SELECT a.email FROM kb_articles k JOIN agents a ON a.workspace_id=k.workspace_id AND a.id=k.author_agent_id WHERE k.id=$1`, *result.ArticleID)
	if author != "ann@kb-mail.example.com" {
		t.Errorf("author = %s, want the member who forwarded", author)
	}
	if conversations := conversationList(t, desk.api, admin); len(conversations) != 0 {
		t.Fatalf("kb@ opened %d conversations, want none", len(conversations))
	}

	again := kbInbound(t, desk.api, raw)
	if again.Status != "duplicate" || again.ArticleID == nil || *again.ArticleID != *result.ArticleID {
		t.Fatalf("the same mail again = %+v, want duplicate of %s", again, *result.ArticleID)
	}
	ws := workspaceID(t, desk.api, admin)
	if count := queryString(t, desk.pool, `SELECT count(*)::text FROM kb_articles WHERE workspace_id=$1`, ws); count != "1" {
		t.Fatalf("articles after the same mail twice = %s, want 1", count)
	}
	if count := queryString(t, desk.pool, `SELECT count(*)::text FROM kb_media WHERE workspace_id=$1`, ws); count != "1" {
		t.Fatalf("library items after the same mail twice = %s, want 1", count)
	}
	if files := desk.storedFiles(t); files != 1 {
		t.Fatalf("stored files after the same mail twice = %d, want 1", files)
	}
}

// Mail to kb@ that does not prove it comes from a member who may write articles is dropped: nothing is
// stored, no conversation or contact is made, and a warning names the reason without any address.
// From alone proves nothing, nor does an Authentication-Results header the sender wrote.
func TestMailToTheKBAddressFromAnUnprovenSenderIsDroppedAndLogged(t *testing.T) {
	desk := newKBMailDesk(t)
	admin := createSession(t, desk.api, desk.service, "KB Drop", "admin@kb-drop.example.com")
	kbCollection(t, desk.api, admin, map[string]any{"name": "Guides"})
	agentSession(t, desk.api, admin, "reader@kb-drop.example.com")
	kbCall(t, desk.api, http.MethodPost, "/api/v1/roles", map[string]any{"name": "Reader", "permissions": []string{"kb_articles:read"}}, admin, http.StatusCreated, nil)
	ws := workspaceID(t, desk.api, admin)
	reader := queryString(t, desk.pool, `SELECT id::text FROM agents WHERE workspace_id=$1 AND email='reader@kb-drop.example.com'`, ws)
	if got := setRoles(t, desk.api, admin, reader, "Reader"); got.code != http.StatusOK {
		t.Fatalf("give Reader: %d %s", got.code, got.body)
	}
	other := createSession(t, desk.api, desk.service, "KB Other", "admin@kb-other.example.com")

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	cases := []struct {
		name, from, reason string
		stamps             []string
	}{
		{"someone who is not a member", "stranger@kb-drop.example.com", "sender_not_member", []string{alignedStamp("kb-drop.example.com")}},
		{"a member of another workspace", "admin@kb-other.example.com", "sender_not_member", []string{alignedStamp("kb-other.example.com")}},
		{"a member whose roles cannot write articles", "reader@kb-drop.example.com", "sender_not_permitted", []string{alignedStamp("kb-drop.example.com")}},
		{"a member's address with no authentication", "admin@kb-drop.example.com", "sender_not_verified", nil},
		{"a member's address with the sender's own dmarc pass", "admin@kb-drop.example.com", "sender_not_verified",
			[]string{"mx.sender.example; dkim=pass header.d=kb-drop.example.com; dmarc=pass header.from=kb-drop.example.com"}},
		{"a sender's dmarc pass below the trusted fail", "admin@kb-drop.example.com", "sender_not_verified",
			[]string{"mx.example.net; dkim=fail header.d=kb-drop.example.com; dmarc=fail header.from=kb-drop.example.com",
				"mx.example.net; dkim=pass header.d=kb-drop.example.com; dmarc=pass header.from=kb-drop.example.com"}},
		{"a member's address signed by another domain", "admin@kb-drop.example.com", "sender_not_verified",
			[]string{"mx.example.net; spf=pass smtp.mailfrom=mailer.example; dkim=pass header.d=mailer.example; dmarc=none header.from=kb-drop.example.com"}},
	}
	for index, tc := range cases {
		messageID := "drop-" + string(rune('a'+index)) + "@kb-drop.example.com"
		result := kbInbound(t, desk.api, kbForward("kb-drop", tc.from, messageID, tc.stamps...))
		if result.Status != "dropped" || result.Reason == nil || *result.Reason != tc.reason || result.ArticleID != nil {
			t.Errorf("%s: result = %+v, want dropped %s", tc.name, result, tc.reason)
		}
	}
	for _, table := range []string{"kb_articles", "kb_media", "kb_email_articles", "conversations", "contacts"} {
		if count := queryString(t, desk.pool, `SELECT count(*)::text FROM `+table+` WHERE workspace_id=$1`, ws); count != "0" {
			t.Errorf("%s rows after the dropped mail = %s, want 0", table, count)
		}
	}
	if conversations := conversationList(t, desk.api, other); len(conversations) != 0 {
		t.Errorf("the other workspace got %d conversations", len(conversations))
	}
	if files := desk.storedFiles(t); files != 0 {
		t.Errorf("stored files after the dropped mail = %d, want 0", files)
	}
	var dropped []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `msg="knowledge base email dropped"`) {
			dropped = append(dropped, line)
		}
	}
	if len(dropped) != len(cases) {
		t.Fatalf("%d drop warnings, want %d:\n%s", len(dropped), len(cases), logs.String())
	}
	for index, line := range dropped {
		if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "workspace="+ws) || !strings.Contains(line, "reason="+cases[index].reason) {
			t.Errorf("warning %d = %s, want WARN for workspace %s with reason %s", index, line, ws, cases[index].reason)
		}
		if strings.Contains(line, "@") {
			t.Errorf("warning carries an address: %s", line)
		}
	}
}

// A workspace without a collection has nowhere to put the draft: the mail is dropped and logged.
func TestMailToTheKBAddressWithoutACollectionIsDropped(t *testing.T) {
	desk := newKBMailDesk(t)
	admin := createSession(t, desk.api, desk.service, "KB Empty", "admin@kb-empty.example.com")
	result := kbInbound(t, desk.api, kbForward("kb-empty", "admin@kb-empty.example.com", "empty-1@kb-empty.example.com", alignedStamp("kb-empty.example.com")))
	if result.Status != "dropped" || result.Reason == nil || *result.Reason != "no_collection" {
		t.Fatalf("result = %+v, want dropped no_collection", result)
	}
	ws := workspaceID(t, desk.api, admin)
	for _, table := range []string{"kb_articles", "kb_media", "conversations"} {
		if count := queryString(t, desk.pool, `SELECT count(*)::text FROM `+table+` WHERE workspace_id=$1`, ws); count != "0" {
			t.Errorf("%s rows = %s, want 0", table, count)
		}
	}
	if files := desk.storedFiles(t); files != 0 {
		t.Errorf("stored files = %d, want 0", files)
	}
}

// The MTA's recipient check accepts kb@ of every workspace on the hosted cloud, also one without an
// email channel, and refuses it for a slug no workspace has.
func TestRecipientCheckAcceptsTheKBAddress(t *testing.T) {
	desk := newKBMailDesk(t)
	createSession(t, desk.api, desk.service, "KB Check", "admin@kb-check.example.com")
	if code := recipientCheck(t, desk.api, platformSecret, "kb@kb-check."+hostedDomain); code != http.StatusNoContent {
		t.Fatalf("kb@ of a workspace without email: %d, want 204", code)
	}
	if code := recipientCheck(t, desk.api, platformSecret, "kb@kb-nobody."+hostedDomain); code != http.StatusNotFound {
		t.Fatalf("kb@ of no workspace: %d, want 404", code)
	}
}

type kbAddressView struct {
	KBAddress *string `json:"kb_address"`
}

func kbAddressOf(t *testing.T, api http.Handler, token string) *string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET email channel status=%d body=%s", response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, "/api/v1/channels/email", nil, token, nil), response)
	var view kbAddressView
	decode(t, response, &view)
	return view.KBAddress
}

// Each workspace reads its own kb@ address, on the hosted cloud only; a forward reaches only the
// workspace the address names.
func TestEachWorkspaceReadsItsOwnKBAddress(t *testing.T) {
	desk := newKBMailDesk(t)
	alpha := createSession(t, desk.api, desk.service, "KB Alpha", "admin@kb-alpha.example.com")
	beta := createSession(t, desk.api, desk.service, "KB Beta", "admin@kb-beta.example.com")
	if got := kbAddressOf(t, desk.api, alpha); got == nil || *got != "kb@kb-alpha."+hostedDomain {
		t.Fatalf("alpha kb address = %v", got)
	}
	if got := kbAddressOf(t, desk.api, beta); got == nil || *got != "kb@kb-beta."+hostedDomain {
		t.Fatalf("beta kb address = %v", got)
	}
	kbCollection(t, desk.api, alpha, map[string]any{"name": "Alpha guides"})
	kbCollection(t, desk.api, beta, map[string]any{"name": "Beta guides"})
	if result := kbInbound(t, desk.api, kbForward("kb-alpha", "admin@kb-alpha.example.com", "cross-1@kb-alpha.example.com", alignedStamp("kb-alpha.example.com"))); result.Status != "created" {
		t.Fatalf("alpha forward = %+v", result)
	}
	if titles := kbSearch(t, desk.api, beta, "status=draft"); len(titles) != 0 {
		t.Fatalf("beta sees %v after a forward to alpha", titles)
	}
	if titles := kbSearch(t, desk.api, alpha, "status=draft"); len(titles) != 1 {
		t.Fatalf("alpha sees %v, want its one draft", titles)
	}

	off, service, _, _ := testAPIWithConfig(t, nil)
	plain := createSession(t, off, service, "KB Plain", "admin@kb-plain.example.com")
	if got := kbAddressOf(t, off, plain); got != nil {
		t.Fatalf("kb address without the hosted cloud = %q, want none", *got)
	}
}

// A mailbox whose support address is kb@ gets another forwarding address, as kb@ is the knowledge
// base's.
func TestAMailboxNamedKBForwardsElsewhere(t *testing.T) {
	desk := newKBMailDesk(t)
	admin := createSession(t, desk.api, desk.service, "KB Boxes", "admin@kb-boxes.example.com")
	connectEmail(t, desk.api, admin, "help@kb-boxes.example.com")
	kb := addEmailChannel(t, desk.api, admin, "kb@kb-boxes.example.com")
	if got := emailChannelOf(t, desk.api, admin, kb).HostedAddress; got == nil || *got != "kb-2@kb-boxes."+hostedDomain {
		t.Fatalf("the kb@ mailbox forwards to %v, want kb-2@kb-boxes.%s", got, hostedDomain)
	}
	if got := kbAddressOf(t, desk.api, admin); got == nil || *got != "kb@kb-boxes."+hostedDomain {
		t.Fatalf("kb address = %v", got)
	}
}

// A mailbox that forwards to kb@ from before the knowledge base address keeps its mail: forwards to kb@
// open conversations there, and the workspace has no knowledge base address.
func TestAnOlderMailboxAtKBKeepsItsMail(t *testing.T) {
	desk := newKBMailDesk(t)
	admin := createSession(t, desk.api, desk.service, "KB Legacy", "admin@kb-legacy.example.com")
	connectEmail(t, desk.api, admin, "help@kb-legacy.example.com")
	legacy := addEmailChannel(t, desk.api, admin, "docs@kb-legacy.example.com")
	ws := workspaceID(t, desk.api, admin)
	if _, err := desk.pool.Exec(t.Context(), `UPDATE email_channels SET hosted_local='kb' WHERE workspace_id=$1 AND channel_id=$2`, ws, legacy); err != nil {
		t.Fatal(err)
	}
	kbCollection(t, desk.api, admin, map[string]any{"name": "Guides"})
	if got := kbAddressOf(t, desk.api, admin); got != nil {
		t.Fatalf("kb address beside a mailbox at kb@ = %q, want none", *got)
	}
	response := inbound(t, desk.api, platformSecret, kbForward("kb-legacy", "admin@kb-legacy.example.com", "legacy-1@kb-legacy.example.com", alignedStamp("kb-legacy.example.com")))
	var result inboundResult
	decode(t, response, &result)
	if response.Code != http.StatusOK || result.ConversationID == nil {
		t.Fatalf("mail to the older kb@ mailbox: %d %s, want a conversation", response.Code, response.Body.String())
	}
	if count := queryString(t, desk.pool, `SELECT count(*)::text FROM kb_articles WHERE workspace_id=$1`, ws); count != "0" {
		t.Fatalf("articles = %s, want 0", count)
	}
}
