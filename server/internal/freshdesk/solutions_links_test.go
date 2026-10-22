package freshdesk_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// helpCenterAddress is the help center of the test workspace on a server without a help center domain.
const helpCenterAddress = testOrigin + "/help/acme-support"

// withLinks makes the fixture's articles and one canned response link old Freshdesk help center
// addresses: on the account's API host, on its account_domain, relative ones, one to an article
// nobody can import (999), one to a draft (102), and one on a host that is not Freshdesk's.
func (e *importEnv) withLinks() *importEnv {
	api := e.fake.api.URL
	e.withSolutions().withCanned()
	e.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 101)["description"] = `<p>Read <a href="` + api + `/support/solutions/articles/104-use-the-api">the API guide</a>,
<a href="/support/solutions/folders/21">invoices</a>, <a href="https://acme.freshdesk.com/en/support/solutions/1">the basics</a>,
<a href="` + api + `/support/solutions/articles/999-gone">an old page</a>, <a href="` + api + `/support/solutions/articles/102">soon</a>
and <a href="https://other.example/support/solutions/articles/104">elsewhere</a>.</p>`
		fixtureArticle(fixture, "21", 106)["description"] = `<p>Start at <a href="` + api + `/support/solutions/articles/101-reset-your-password">reset</a>.</p>`
	})
	e.fake.cannedExtra = []map[string]any{{"id": 9170, "title": "Docs", "folder_id": 801, "attachments": []any{},
		"content": "See " + api + "/support/solutions/articles/104-use-the-api. Thanks", "content_html": ""}}
	return e
}

func (e *importEnv) linkOptions(options *freshdesk.Options) {
	options.Origin = testOrigin
}

func (e *importEnv) setHelpCenter(t *testing.T, enabled bool) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO help_centers(workspace_id,enabled,content_version,updated_at) VALUES($1,$2,1,now())
		ON CONFLICT (workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled`, e.workspaceID, enabled); err != nil {
		t.Fatal(err)
	}
}

func (e *importEnv) linkRun(t *testing.T) *freshdesk.Report {
	t.Helper()
	report, err := e.run(t, context.Background(), e.linkOptions)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return report
}

// articleNumber is the number of the article imported from a Freshdesk article.
func (e *importEnv) articleNumber(t *testing.T, freshdeskID string) int64 {
	t.Helper()
	var number int64
	if err := e.pool.QueryRow(context.Background(), `SELECT number FROM kb_articles WHERE workspace_id=$1 AND source_meta->>'freshdesk_id'=$2`,
		e.workspaceID, freshdeskID).Scan(&number); err != nil {
		t.Fatal(err)
	}
	return number
}

func (e *importEnv) versions(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("articles=%d replies=%d", e.count(t, "kb_article_versions"), e.count(t, "saved_reply_versions"))
}

func (e *importEnv) savedReplyBody(t *testing.T, title string) storedReply {
	t.Helper()
	for _, reply := range e.savedReplies(t) {
		if reply.Title == title {
			return reply
		}
	}
	t.Fatalf("no saved reply %q", title)
	return storedReply{}
}

func TestImportPointsFreshdeskLinksAtTheNewHelpCenter(t *testing.T) {
	env := newImportEnv(t).withLinks()
	env.setHelpCenter(t, true)
	report := env.linkRun(t)
	api := env.fake.api.URL
	articles := env.kbArticles(t, env.workspaceID)

	guide := fmt.Sprintf("%s/a/%d-use-the-api", helpCenterAddress, env.articleNumber(t, "104"))
	body := articles["101"].Body
	for _, want := range []string{"[the API guide](" + guide + ")", "[invoices](" + helpCenterAddress + "/c/billing/invoices)",
		"[the basics](" + helpCenterAddress + "/c/getting-started)", "[an old page](" + api + "/support/solutions/articles/999-gone)",
		"[soon](" + api + "/support/solutions/articles/102)", "[elsewhere](https://other.example/support/solutions/articles/104)"} {
		if !strings.Contains(body, want) {
			t.Errorf("article 101 lacks %q:\n%s", want, body)
		}
	}
	if want := fmt.Sprintf("[reset](%s/a/%d-reset-your-password)", helpCenterAddress, env.articleNumber(t, "101")); !strings.Contains(articles["106"].Body, want) {
		t.Errorf("article 106 lacks %q:\n%s", want, articles["106"].Body)
	}
	// A plain-text saved reply: the sentence's full stop is not part of the address.
	if reply := env.savedReplyBody(t, "General / Docs"); reply.Body != "See "+guide+". Thanks" {
		t.Errorf("saved reply body = %q", reply.Body)
	}
	// The original HTML stays as Freshdesk sent it.
	if !strings.Contains(articles["101"].SourceHTML, api+"/support/solutions/articles/104-use-the-api") {
		t.Errorf("source_html changed: %s", articles["101"].SourceHTML)
	}

	links := report.Links
	if links == nil {
		t.Fatalf("report has no links section: %+v", report)
	}
	// Old addresses, written by hand: the home and solutions pages, categories 1 and 2, the folders
	// every visitor saw (11, 111, 1111, 21; 12 is for agents) and the public articles (101, 104, 105,
	// 106): 12 paths, each also below /en/ and /es/ (primary and portal language).
	if links.OldAddresses.Found != 36 || links.OldAddresses.Redirected != 36 || len(links.OldAddresses.Unmapped) != 0 {
		t.Errorf("old addresses = %+v", links.OldAddresses)
	}
	if want := (freshdesk.LinkRewrites{Articles: 2, SavedReplies: 1, Links: 5}); links.Rewritten != want {
		t.Errorf("rewritten = %+v, want %+v", links.Rewritten, want)
	}
	left := map[string]string{}
	for _, link := range links.Left {
		left[link.Entity+" "+link.FreshdeskID+" "+link.URL] = link.Reason
	}
	wantLeft := map[string]string{
		"kb_article 101 " + api + "/support/solutions/articles/999-gone":      freshdesk.LinkNotImported,
		"kb_article 101 " + api + "/support/solutions/articles/102":           freshdesk.LinkNotPublic,
		"kb_article 101 https://other.example/support/solutions/articles/104": freshdesk.LinkUnknownHost,
	}
	if fmt.Sprint(left) != fmt.Sprint(wantLeft) {
		t.Errorf("left = %v, want %v", left, wantLeft)
	}
	if links.Status != freshdesk.StatusUnverified || links.HelpCenter != helpCenterAddress {
		t.Errorf("links status=%s help center=%s", links.Status, links.HelpCenter)
	}
	// Links are not data: the import itself stays OK.
	if report.Status != freshdesk.StatusOK {
		t.Errorf("status=%s failures=%+v", report.Status, report.Failures)
	}
	markdown := report.Markdown()
	for _, want := range []string{"## Old help center links", "36 old addresses", "999-gone"} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}

	// A rerun finds the links already pointing at the help center and writes no new version.
	before := env.versions(t)
	again := env.linkRun(t)
	if after := env.versions(t); after != before {
		t.Errorf("rerun versions %s, before %s", after, before)
	}
	if again.Links == nil || again.Links.Rewritten != (freshdesk.LinkRewrites{}) || len(again.Links.Left) != 3 {
		t.Errorf("rerun links = %+v", again.Links)
	}
	if body := env.kbArticles(t, env.workspaceID)["101"].Body; body != articles["101"].Body {
		t.Errorf("rerun changed article 101:\n%s", body)
	}
}

// Content changed in Colliedesk after the import is never rewritten: it is listed instead. A help
// center that is off gets no links at all, and the report says to turn it on.
func TestImportLeavesChangedContentAndAnOffHelpCenterAlone(t *testing.T) {
	env := newImportEnv(t).withLinks()
	first := env.linkRun(t)
	api := env.fake.api.URL
	if first.Links == nil || first.Links.Status != freshdesk.StatusUnverified || !strings.Contains(strings.Join(first.Links.Detail, " "), "turn the help center on") {
		t.Fatalf("off help center links = %+v", first.Links)
	}
	if first.Links.OldAddresses.Redirected != 0 || first.Links.Rewritten != (freshdesk.LinkRewrites{}) {
		t.Errorf("off help center links = %+v", first.Links)
	}
	if body := env.kbArticles(t, env.workspaceID)["101"].Body; !strings.Contains(body, api+"/support/solutions/articles/104-use-the-api") {
		t.Fatalf("an off help center got its links rewritten:\n%s", body)
	}

	// An agent edits article 106 and the saved reply; then the help center goes on and the import runs again.
	articles := env.kbArticles(t, env.workspaceID)
	edited := articles["106"].Body + "\n\nEdited here."
	env.editArticle(t, articles["106"].ID, edited)
	reply := env.savedReplyBody(t, "General / Docs")
	data := store.New(env.pool)
	var seen time.Time
	if err := env.pool.QueryRow(context.Background(), `SELECT updated_at FROM saved_replies WHERE id=$1`, reply.ID).Scan(&seen); err != nil {
		t.Fatal(err)
	}
	replyText := reply.Body + " Edited."
	if _, err := data.UpdateSavedReply(context.Background(), env.workspaceID, store.Actor{}, reply.ID, seen, nil, &replyText); err != nil {
		t.Fatal(err)
	}
	env.setHelpCenter(t, true)
	second := env.linkRun(t)
	after := env.kbArticles(t, env.workspaceID)
	if after["106"].Body != edited {
		t.Errorf("edited article 106 changed:\n%s", after["106"].Body)
	}
	if got := env.savedReplyBody(t, "General / Docs").Body; got != replyText {
		t.Errorf("edited saved reply changed: %q", got)
	}
	if !strings.Contains(after["101"].Body, helpCenterAddress+"/c/billing/invoices") {
		t.Errorf("article 101 not rewritten once the help center is on:\n%s", after["101"].Body)
	}
	var reasons []string
	for _, link := range second.Links.Left {
		if link.Reason == freshdesk.LinkChangedInColliedesk {
			reasons = append(reasons, link.Entity+" "+link.FreshdeskID)
		}
	}
	slices.Sort(reasons)
	if strings.Join(reasons, ",") != "kb_article 106,saved_reply 9170" {
		t.Errorf("left as changed in Colliedesk: %v (links %+v)", reasons, second.Links.Left)
	}
}

// Undo removes what the import brought, rewritten links included: nothing it wrote stays behind as
// if a person had edited it.
func TestUndoRemovesRewrittenArticlesAndSavedReplies(t *testing.T) {
	env := newImportEnv(t).withLinks()
	env.setHelpCenter(t, true)
	report := env.linkRun(t)
	if report.Links == nil || report.Links.Rewritten.SavedReplies != 1 {
		t.Fatalf("links = %+v", report.Links)
	}
	result, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.KeptSavedReplies) != 0 || len(result.KeptKBArticles) != 0 {
		t.Errorf("undo kept %v saved replies and %v articles", result.KeptSavedReplies, result.KeptKBArticles)
	}
	for _, table := range []string{"kb_articles", "saved_replies", "kb_collections"} {
		if n := env.count(t, table); n != 0 {
			t.Errorf("%s left after undo: %d", table, n)
		}
	}
}

// Without the server's address (APP_ORIGIN unset where the import runs) no link is rewritten, even
// with the help center on, and the report says why rather than blaming the help center.
func TestImportWithoutTheServerAddressRewritesNoLinkAndSaysWhy(t *testing.T) {
	env := newImportEnv(t).withLinks()
	env.setHelpCenter(t, true)
	report, err := env.run(t, context.Background(), nil)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	api := env.fake.api.URL
	if body := env.kbArticles(t, env.workspaceID)["101"].Body; !strings.Contains(body, api+"/support/solutions/articles/104-use-the-api") {
		t.Fatalf("links were rewritten without the server's address:\n%s", body)
	}
	links := report.Links
	if links == nil || links.Status != freshdesk.StatusUnverified || links.Rewritten != (freshdesk.LinkRewrites{}) {
		t.Fatalf("links = %+v", links)
	}
	if detail := strings.Join(links.Detail, " "); !strings.Contains(detail, "APP_ORIGIN") || strings.Contains(detail, "turn the help center on") {
		t.Errorf("detail = %q", detail)
	}
	// A link on a host this run does not know keeps that reason: naming the host is the fix there.
	for _, link := range links.Left {
		if link.Reason != freshdesk.LinkNoServerAddress && link.Reason != freshdesk.LinkUnknownHost {
			t.Errorf("left %s %s %s: reason %s, want %s", link.Entity, link.FreshdeskID, link.URL, link.Reason, freshdesk.LinkNoServerAddress)
		}
	}
	if len(links.Left) == 0 {
		t.Error("no link was listed as left")
	}
}

// withLinkShapes gives article 101 every shape an old link takes in a real article, and a plain-text
// saved reply that quotes old paths. help.acme.example stands for the old portal's own custom domain.
func (e *importEnv) withLinkShapes() *importEnv {
	api := e.fake.api.URL
	e.withSolutions().withCanned()
	e.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 101)["description"] = `<p><a href="` + api + `/support/solutions/articles/104-use-the-api#step-2">step two</a>,
<a href="` + api + `/support">old home</a>, <a href="` + api + `/support/search?term=reset+password">search</a>,
<a href="` + api + `/Support/Solutions/Articles/104">shouting</a>, <a href="` + api + `/support/tickets/new">new ticket</a>,
<a href="https://help.acme.example/support/solutions/articles/104">portal</a>, <a href="https://help.acme.example/support/home">portal home</a>,
<a href="` + api + `/supporters">fans</a>.</p>
<p>Old addresses looked like <code>` + api + `/support/solutions/articles/104</code>.</p>
<pre><code>curl ` + api + `/support/solutions/articles/104
</code></pre>`
	})
	e.fake.cannedExtra = []map[string]any{{"id": 9171, "title": "Quoted", "folder_id": 801, "attachments": []any{},
		"content": `Read "/support/solutions/articles/104-use-the-api" or '/support/home'.`, "content_html": ""}}
	return e
}

func TestImportRewritesEveryShapeOfOldLinkAndListsWhatItCannot(t *testing.T) {
	env := newImportEnv(t).withLinkShapes()
	env.setHelpCenter(t, true)
	report := env.linkRun(t)
	api := env.fake.api.URL
	guide := fmt.Sprintf("%s/a/%d-use-the-api", helpCenterAddress, env.articleNumber(t, "104"))
	body := env.kbArticles(t, env.workspaceID)["101"].Body
	for _, want := range []string{
		"[step two](" + guide + "#step-2)",
		"[old home](" + helpCenterAddress + ")",
		"[search](" + helpCenterAddress + "/search?q=reset+password)",
		"[shouting](" + guide + ")",
		"[new ticket](" + api + "/support/tickets/new)",
		"[portal](https://help.acme.example/support/solutions/articles/104)",
		"[portal home](https://help.acme.example/support/home)",
		"[fans](" + api + "/supporters)",
		"`" + api + "/support/solutions/articles/104`",
		"curl " + api + "/support/solutions/articles/104\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("article 101 lacks %q:\n%s", want, body)
		}
	}
	if reply := env.savedReplyBody(t, "General / Quoted").Body; reply != `Read "`+guide+`" or '`+helpCenterAddress+`'.` {
		t.Errorf("saved reply = %q", reply)
	}
	left := map[string]string{}
	for _, link := range report.Links.Left {
		left[link.Entity+" "+link.FreshdeskID+" "+link.URL] = link.Reason
	}
	wantLeft := map[string]string{
		"kb_article 101 " + api + "/support/tickets/new":                          freshdesk.LinkNoNewPage,
		"kb_article 101 https://help.acme.example/support/solutions/articles/104": freshdesk.LinkUnknownHost,
	}
	if fmt.Sprint(left) != fmt.Sprint(wantLeft) {
		t.Errorf("left = %v, want %v", left, wantLeft)
	}
	if want := (freshdesk.LinkRewrites{Articles: 1, SavedReplies: 1, Links: 6}); report.Links.Rewritten != want {
		t.Errorf("rewritten = %+v, want %+v", report.Links.Rewritten, want)
	}
	if detail := strings.Join(report.Links.Detail, " "); report.Links.Status != freshdesk.StatusUnverified || !strings.Contains(detail, "old help center address") {
		t.Errorf("status %s, detail %q", report.Links.Status, detail)
	}

	// Named as the old help center's address, that host's links are this account's.
	again, err := env.run(t, context.Background(), func(options *freshdesk.Options) {
		env.linkOptions(options)
		options.OldHelpCenterDomain = "help.acme.example"
	})
	if err != nil {
		t.Fatal(err)
	}
	if body := env.kbArticles(t, env.workspaceID)["101"].Body; !strings.Contains(body, "[portal]("+guide+")") || !strings.Contains(body, "[portal home]("+helpCenterAddress+")") {
		t.Errorf("article 101 after naming the old help center:\n%s", body)
	}
	if len(again.Links.Left) != 1 || again.Links.Left[0].Reason != freshdesk.LinkNoNewPage {
		t.Errorf("left after naming the old help center = %+v", again.Links.Left)
	}
}

func TestOldHelpCenterHostReadsWhatAPersonTypes(t *testing.T) {
	for typed, want := range map[string]string{
		"help.acme.example": "help.acme.example", " https://Support.Acme.com/ ": "support.acme.com", "http://support.acme.com": "support.acme.com",
		"support.acme.com:8443": "support.acme.com:8443",
	} {
		if got, err := freshdesk.OldHelpCenterHost(typed); err != nil || got != want {
			t.Errorf("OldHelpCenterHost(%q) = %q, %v; want %q", typed, got, err, want)
		}
	}
	for _, typed := range []string{"", "support", "https://support.acme.com/support/home", "support acme.com", "-acme.com", "acme..com", "user@acme.com", strings.Repeat("a", 250) + ".com"} {
		if got, err := freshdesk.OldHelpCenterHost(typed); !errors.Is(err, freshdesk.ErrOldHelpCenterHost) {
			t.Errorf("OldHelpCenterHost(%q) = %q, %v; want ErrOldHelpCenterHost", typed, got, err)
		}
	}
}

// withTranslationLinks makes the Spanish translation of article 101 link an imported article and one
// nobody can import, below the portal's /es/ prefix.
func (e *importEnv) withTranslationLinks() *importEnv {
	api := e.fake.api.URL
	e.withLinks()
	links := e.fake.solutionsEdit
	e.editSolutions(func(fixture map[string]any) {
		links(fixture)
		translation := fixture["translations"].(map[string]any)["es"].(map[string]any)["11"].([]any)[0].(map[string]any)
		translation["description"] = `<p>Lea <a href="` + api + `/es/support/solutions/articles/104-use-the-api">la guía</a> y <a href="` + api + `/es/support/solutions/articles/999">otra</a>.</p>`
	})
	return e
}

func (e *importEnv) translationBody(t *testing.T, freshdeskID, locale string) string {
	t.Helper()
	var body string
	if err := e.pool.QueryRow(context.Background(), `SELECT t.body_md FROM kb_article_translations t JOIN kb_articles a ON a.workspace_id=t.workspace_id AND a.id=t.article_id
		WHERE t.workspace_id=$1 AND a.source_meta->>'freshdesk_id'=$2 AND t.locale=$3`, e.workspaceID, freshdeskID, locale).Scan(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// Translations follow the article's rule: rewritten while nobody changed the article in Colliedesk,
// listed otherwise, and never skipped without a word.
func TestImportPointsLinksInTranslationsAtTheHelpCenter(t *testing.T) {
	env := newImportEnv(t).withTranslationLinks()
	env.setHelpCenter(t, true)
	report := env.linkRun(t)
	api := env.fake.api.URL
	guide := fmt.Sprintf("%s/a/%d-use-the-api", helpCenterAddress, env.articleNumber(t, "104"))
	if body := env.translationBody(t, "101", "es"); body != "Lea [la guía]("+guide+") y [otra]("+api+"/es/support/solutions/articles/999)." {
		t.Errorf("translation = %q", body)
	}
	if want := (freshdesk.LinkRewrites{Articles: 2, Translations: 1, SavedReplies: 1, Links: 6}); report.Links.Rewritten != want {
		t.Errorf("rewritten = %+v, want %+v", report.Links.Rewritten, want)
	}
	found := false
	for _, link := range report.Links.Left {
		if link.Entity == "kb_translation" && link.FreshdeskID == "101:es" && link.URL == api+"/es/support/solutions/articles/999" && link.Reason == freshdesk.LinkNotImported {
			found = true
		}
	}
	if !found {
		t.Errorf("the translation's unimported link is not listed: %+v", report.Links.Left)
	}
	before := env.versions(t)
	if again := env.linkRun(t); again.Links.Rewritten != (freshdesk.LinkRewrites{}) || env.versions(t) != before {
		t.Errorf("rerun rewrote again: %+v", again.Links.Rewritten)
	}
}

// Once a person changes the article, its translations are left and listed too.
func TestImportLeavesTheTranslationsOfAChangedArticleAndListsThem(t *testing.T) {
	changed := newImportEnv(t).withTranslationLinks()
	changed.linkRun(t)
	article := changed.kbArticles(t, changed.workspaceID)["101"]
	changed.editArticle(t, article.ID, article.Body+"\n\nEdited here.")
	changed.setHelpCenter(t, true)
	second := changed.linkRun(t)
	if body := changed.translationBody(t, "101", "es"); !strings.Contains(body, changed.fake.api.URL+"/es/support/solutions/articles/104-use-the-api") {
		t.Errorf("the translation of a changed article was rewritten: %q", body)
	}
	left := 0
	for _, link := range second.Links.Left {
		if link.Entity == "kb_translation" && link.Reason == freshdesk.LinkChangedInColliedesk {
			left++
		}
	}
	if left != 2 {
		t.Errorf("translation links left as changed = %d, want 2: %+v", left, second.Links.Left)
	}
}
