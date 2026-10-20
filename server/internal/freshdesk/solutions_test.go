package freshdesk_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (e *importEnv) withSolutions() *importEnv {
	e.fake.solutions = true
	return e
}

// editSolutions changes the knowledge base fixture for the next requests.
func (e *importEnv) editSolutions(edit func(map[string]any)) {
	e.fake.mu.Lock()
	e.fake.solutionsEdit = edit
	e.fake.mu.Unlock()
}

// fixtureArticle returns article id of folder in the fixture, to change it.
func fixtureArticle(fixture map[string]any, folder string, id float64) map[string]any {
	for _, item := range fixture["articles"].(map[string]any)[folder].([]any) {
		if article := item.(map[string]any); article["id"] == id {
			return article
		}
	}
	panic(fmt.Sprintf("no article %v in folder %s", id, folder))
}

type storedArticle struct {
	ID, Collection, Title, Slug, Body, Status, SourceHTML, Meta string
	Author                                                      *string
	CreatedAt, UpdatedAt                                        time.Time
	PublishedAt                                                 *time.Time
}

// kbArticles returns the workspace's articles by their Freshdesk id (from source_meta).
func (e *importEnv) kbArticles(t *testing.T, workspaceID string) map[string]storedArticle {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT a.id::text,c.name,a.title,a.slug,a.body_md,a.status,COALESCE(a.source_html,''),COALESCE(a.source_meta::text,''),
			COALESCE(a.source_meta->>'freshdesk_id',a.id::text),a.author_agent_id::text,a.created_at,a.updated_at,a.published_at
		FROM kb_articles a JOIN kb_collections c ON c.workspace_id=a.workspace_id AND c.id=a.collection_id WHERE a.workspace_id=$1`, workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]storedArticle{}
	for rows.Next() {
		var item storedArticle
		var key string
		if err := rows.Scan(&item.ID, &item.Collection, &item.Title, &item.Slug, &item.Body, &item.Status, &item.SourceHTML, &item.Meta, &key, &item.Author,
			&item.CreatedAt, &item.UpdatedAt, &item.PublishedAt); err != nil {
			t.Fatal(err)
		}
		out[key] = item
	}
	return out
}

// kbTree lists the collections as "parent > name" lines, sorted.
func (e *importEnv) kbTree(t *testing.T) string {
	t.Helper()
	var tree string
	if err := e.pool.QueryRow(context.Background(), `SELECT string_agg(COALESCE(p.name||' > ','')||c.name, ' | ' ORDER BY COALESCE(p.name||' > ','')||c.name)
		FROM kb_collections c LEFT JOIN kb_collections p ON p.workspace_id=c.workspace_id AND p.id=c.parent_id WHERE c.workspace_id=$1`, e.workspaceID).Scan(&tree); err != nil {
		t.Fatal(err)
	}
	return tree
}

func (e *importEnv) countIn(t *testing.T, workspaceID, table string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE workspace_id=$1`, workspaceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func warningIDs(report *freshdesk.Report, code string) string {
	for _, warning := range report.Warnings {
		if warning.Code == code {
			return strings.Join(warning.FreshdeskIDs, ",")
		}
	}
	return ""
}

func TestSolutionsBecomeTheKnowledgeBaseWithOriginalTimestampsDraftsAndTranslations(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s missing=%v failures=%+v verification=%+v", report.Status, report.Missing, report.Failures, report.Verification.KnowledgeBase)
	}
	kb := report.KnowledgeBase
	// Folder 111 is listed both in its category and as a subfolder of 11: it is one collection.
	assertCount(t, "kb collections", kb.Collections, 7, 7)
	assertCount(t, "kb articles", kb.Articles, 6, 6)
	assertCount(t, "kb translations", kb.Translations, 1, 1)
	assertCount(t, "kb images", kb.Images.EntityCount, 2, 1)
	if kb.Images.Gone != 1 || kb.Images.GoneImages[0].ArticleID != "104" {
		t.Fatalf("gone images = %+v", kb.Images)
	}
	if got := strings.Join(kb.Published, ","); got != "101,104,105,106" {
		t.Fatalf("published = %s", got)
	}
	if fmt.Sprint(kb.Drafts) != "map[draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]" {
		t.Fatalf("drafts = %v", kb.Drafts)
	}
	if strings.Join(kb.Languages, ",") != "es" || len(kb.Portals) != 0 || !report.Verification.KnowledgeBase.Verified {
		t.Fatalf("languages=%v portals=%v verification=%+v", kb.Languages, kb.Portals, report.Verification.KnowledgeBase)
	}
	if warningIDs(report, "kb_folder_flattened") != "1111" || warningIDs(report, "kb_article_markup_dropped") != "106" {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	markdown := report.Markdown()
	for _, want := range []string{"| Knowledge base articles | 6 | 6 | 0 |", "Knowledge base: 4 published, 2 imported as drafts", "- **Knowledge base: verified.**",
		"folder_not_visible_to_all_users (in a folder not visible to all users", "article 104: download image"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("report lacks %q:\n%s", want, markdown)
		}
	}
	if slices.Contains(report.NotImported, "Solution (knowledge base) articles") {
		t.Fatal("the report still says solutions are not imported")
	}

	if got := env.kbTree(t); got != "Basics > Advanced | Basics > Advanced / Deep | Billing | Billing > Invoices | Getting started | Getting started > Basics | Getting started > Internal" {
		t.Fatalf("tree = %s", got)
	}
	articles := env.kbArticles(t, env.workspaceID)
	reset := articles["101"]
	if reset.Status != "published" || reset.Collection != "Basics" || reset.Title != "Reset your password" || reset.Slug != "reset-your-password" ||
		reset.Body != "## Steps\n\nOpen **Settings** and choose [Reset](https://acme.example/reset)." {
		t.Fatalf("article 101 = %+v", reset)
	}
	if !reset.CreatedAt.Equal(time.Date(2023, 3, 1, 10, 0, 0, 0, time.UTC)) || !reset.UpdatedAt.Equal(time.Date(2023, 4, 1, 10, 0, 0, 0, time.UTC)) ||
		reset.PublishedAt == nil || !reset.PublishedAt.Equal(reset.UpdatedAt) {
		t.Fatalf("article 101 timestamps = %v %v %v", reset.CreatedAt, reset.UpdatedAt, reset.PublishedAt)
	}
	var alice string
	if err := env.pool.QueryRow(context.Background(), `SELECT id::text FROM agents WHERE workspace_id=$1 AND email='alice@acme.example'`, env.workspaceID).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	if reset.Author == nil || *reset.Author != alice || articles["106"].Author != nil {
		t.Fatalf("authors: 101=%v 106=%v", reset.Author, articles["106"].Author)
	}
	if !strings.Contains(reset.SourceHTML, "<b>Settings</b>") || !strings.Contains(reset.Meta, `"tags": ["password", "login"]`) || !strings.Contains(reset.Meta, `"hits": 42`) {
		t.Fatalf("article 101 source = %q meta = %s", reset.SourceHTML, reset.Meta)
	}
	if articles["102"].Status != "draft" || articles["103"].Status != "draft" || articles["105"].Collection != "Advanced / Deep" {
		t.Fatalf("articles = %+v", articles)
	}
	invoice := articles["106"].Body
	if strings.Contains(invoice, "javascript") || strings.Contains(invoice, "iframe") || !strings.Contains(invoice, "click") || !strings.Contains(invoice, "| Total | Total |") {
		t.Fatalf("article 106 body = %q", invoice)
	}
	api := articles["104"].Body
	var imageID, imageSource string
	var counted bool
	if err := env.pool.QueryRow(context.Background(), `SELECT i.id::text,i.counted,a.source_html FROM kb_images i
		JOIN kb_article_images l ON l.workspace_id=i.workspace_id AND l.image_id=i.id JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		WHERE i.workspace_id=$1`, env.workspaceID).Scan(&imageID, &counted, &imageSource); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(api, "![Token page](/api/v1/kb/images/"+imageID+")") || !strings.Contains(api, env.fake.files.URL+"/inline-kb-gone") || counted {
		t.Fatalf("article 104 body = %q, image %s counted=%v", api, imageID, counted)
	}
	if !strings.Contains(imageSource, "inline-kb-7001") {
		t.Fatalf("the original HTML was changed: %q", imageSource)
	}
	var versions string
	if err := env.pool.QueryRow(context.Background(), `SELECT string_agg(DISTINCT kind, ',') FROM kb_article_versions WHERE workspace_id=$1`, env.workspaceID).Scan(&versions); err != nil || versions != "import" {
		t.Fatalf("version kinds = %q, %v", versions, err)
	}
	var locale, title, body string
	if err := env.pool.QueryRow(context.Background(), `SELECT t.locale,t.title,t.body_md FROM kb_article_translations t WHERE t.workspace_id=$1 AND t.article_id=$2`,
		env.workspaceID, reset.ID).Scan(&locale, &title, &body); err != nil || locale != "es" || title != "Restablecer la contraseña" || body != "Abra **Ajustes**." {
		t.Fatalf("translation = %s %q %q, %v", locale, title, body, err)
	}

	// A second run changes nothing and adds nothing.
	again := env.mustRun(t)
	if again.Status != freshdesk.StatusOK || env.count(t, "kb_articles") != 6 || env.count(t, "kb_collections") != 7 || env.count(t, "kb_article_versions") != 6 || env.count(t, "kb_images") != 1 {
		t.Fatalf("rerun: status=%s articles=%d collections=%d versions=%d", again.Status, env.count(t, "kb_articles"), env.count(t, "kb_collections"), env.count(t, "kb_article_versions"))
	}
}

func TestSolutionsAreImportedInFullWhateverSinceSays(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	since := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.Since = &since })
	if err != nil {
		t.Fatal(err)
	}
	if report.KnowledgeBase.Articles.Imported != 6 || env.count(t, "kb_articles") != 6 {
		t.Fatalf("articles = %+v", report.KnowledgeBase.Articles)
	}
}

func TestSolutionsAbsentOrRefused(t *testing.T) {
	env := newImportEnv(t)
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || !report.KnowledgeBase.NotAvailable || env.fake.count("/api/v2/settings/helpdesk") != 0 {
		t.Fatalf("no knowledge base: status=%s kb=%+v", report.Status, report.KnowledgeBase)
	}

	refused := newImportEnv(t).withSolutions()
	refused.fake.failPathStatus = map[string]int{"/api/v2/solutions/categories": http.StatusForbidden}
	report = refused.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || report.Verification.KnowledgeBase.Verified || !strings.Contains(report.Verification.KnowledgeBase.Detail, "right to read solutions") {
		t.Fatalf("refused: status=%s verification=%+v", report.Status, report.Verification.KnowledgeBase)
	}
	if report.Tickets.Imported != report.Tickets.Source || refused.count(t, "kb_articles") != 0 {
		t.Fatalf("refused knowledge base stopped the tickets: %+v", report.Tickets)
	}
}

func TestSolutionsUnmatchedCounts(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixture["category_folders"].(map[string]any)["2"].([]any)[0].(map[string]any)["articles_count"] = 2.0
	})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || !strings.Contains(report.Verification.KnowledgeBase.Detail, "folder 21 reports 2 articles, 1 were listed") {
		t.Fatalf("status=%s verification=%+v", report.Status, report.Verification.KnowledgeBase)
	}

	translations := newImportEnv(t).withSolutions()
	translations.fake.failPathStatus = map[string]int{"/api/v2/solutions/folders/11/articles/es": http.StatusBadRequest}
	report = translations.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || !strings.Contains(report.Verification.KnowledgeBase.Detail, "es translations of folder 11") {
		t.Fatalf("status=%s verification=%+v", report.Status, report.Verification.KnowledgeBase)
	}

	settings := newImportEnv(t).withSolutions()
	settings.fake.failPathStatus = map[string]int{"/api/v2/settings/helpdesk": http.StatusForbidden}
	report = settings.mustRun(t)
	if report.Status != freshdesk.StatusUnverified || !strings.Contains(report.Verification.KnowledgeBase.Detail, "languages could not be read") {
		t.Fatalf("status=%s verification=%+v", report.Status, report.Verification.KnowledgeBase)
	}
}

func TestSolutionsAFolderListThatFailsIsAFailureAndTheRestImports(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.fake.failPathStatus = map[string]int{"/api/v2/solutions/folders/12/articles": http.StatusBadRequest}
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || env.count(t, "kb_articles") != 5 {
		t.Fatalf("status=%s articles=%d failures=%+v", report.Status, env.count(t, "kb_articles"), report.Failures)
	}
	found := false
	for _, failure := range report.Failures {
		found = found || (failure.Entity == "kb_folder" && failure.FreshdeskID == "12")
	}
	if !found {
		t.Fatalf("failures = %+v", report.Failures)
	}
}

func setPortals(fixture map[string]any, portals ...any) {
	for index, category := range fixture["categories"].([]any) {
		if portals[index] != nil {
			category.(map[string]any)["visible_in_portals"] = portals[index]
		}
	}
}

func TestSolutionsArePublishedOnlyInTheHelpCenterPortal(t *testing.T) {
	cases := []struct {
		name      string
		portals   []any
		chosen    int64
		published string
		drafts    string
	}{
		// One portal: the docs give visible_in_portals only to accounts with several portals.
		{"one portal, no field", []any{nil, nil}, 0, "101,104,105,106", "map[draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]"},
		{"one portal named", []any{[]any{5.0}, []any{5.0}}, 0, "101,104,105,106", "map[draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]"},
		{"several portals, none chosen", []any{[]any{5.0}, []any{6.0}}, 0, "",
			"map[category_in_another_portal:[101 104 105 106] draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]"},
		{"several portals, one chosen", []any{[]any{5.0}, []any{6.0}}, 5, "101,104,105", "map[category_in_another_portal:[106] draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]"},
		{"a category in no portal", []any{[]any{5.0}, []any{}}, 0, "101,104,105", "map[category_not_in_any_portal:[106] draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]"},
		{"a category without the field among others", []any{[]any{5.0}, nil}, 0, "101,104,105", "map[category_portal_unknown:[106] draft_in_freshdesk:[102] folder_not_visible_to_all_users:[103]]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newImportEnv(t).withSolutions()
			env.editSolutions(func(fixture map[string]any) { setPortals(fixture, tc.portals...) })
			report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.HelpCenterPortal = tc.chosen; o.DryRun = true })
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(report.KnowledgeBase.Published, ","); got != tc.published || fmt.Sprint(report.KnowledgeBase.Drafts) != tc.drafts {
				t.Fatalf("published=%s drafts=%v", got, report.KnowledgeBase.Drafts)
			}
		})
	}
}

func TestSolutionsAPortalTheAccountDoesNotHaveIsRefusedBeforeWriting(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) { setPortals(fixture, []any{5.0}, []any{6.0}) })
	_, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.HelpCenterPortal = 9 })
	if err == nil || !strings.Contains(err.Error(), "--help-center-portal 9 is not a portal") || !strings.Contains(err.Error(), "5, 6") {
		t.Fatalf("err = %v", err)
	}
	if env.count(t, "contacts") != 0 || env.count(t, "kb_articles") != 0 {
		t.Fatalf("written: %s", env.tableCounts(t))
	}
}

func TestSolutionsOverTheLimitsAreRefusedBeforeAnythingIsWritten(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 102)["description"] = "<p>" + strings.Repeat("a", 100001) + "</p>"
	})
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.DryRun = true })
	if err != nil || len(report.KnowledgeBase.Refused) != 1 || !strings.HasPrefix(report.KnowledgeBase.Refused[0], "article 102: The text holds at most 100000 characters") {
		t.Fatalf("dry run: %v %+v", err, report.KnowledgeBase.Refused)
	}
	_, err = env.run(t, context.Background(), nil)
	var limit *freshdesk.KnowledgeBaseLimitError
	if !errors.As(err, &limit) || !strings.Contains(err.Error(), "nothing was imported") {
		t.Fatalf("err = %v", err)
	}
	if env.count(t, "contacts") != 0 || env.count(t, "kb_articles") != 0 || env.count(t, "kb_collections") != 0 {
		t.Fatalf("written: %s", env.tableCounts(t))
	}
}

func TestSolutionsOverTheArticleCapAreRefused(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	// 1,995 articles of the workspace's own (the cap is 2,000) leave no room for six more.
	if _, err := env.pool.Exec(ctx, `WITH c AS (INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Own','own') RETURNING id)
		INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md) SELECT $1,c.id,n,'Own '||n,'own-'||n,'' FROM c, generate_series(1,1995) n`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	_, err := env.run(t, ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "holds 1995 knowledge base articles (the trash counts) and the import adds 6, over the limit of 2000") {
		t.Fatalf("err = %v", err)
	}
	if env.count(t, "contacts") != 0 || env.count(t, "kb_articles") != 1995 {
		t.Fatalf("written: %s", env.tableCounts(t))
	}
}

func TestSolutionsDryRunCountsAndWritesNothing(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	report, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.DryRun = true })
	if err != nil {
		t.Fatal(err)
	}
	kb := report.KnowledgeBase
	if kb.Collections.Source != 7 || kb.Articles.Source != 6 || kb.Translations.Source != 1 || kb.Images.Source != 2 || kb.Articles.Imported != 0 {
		t.Fatalf("dry run = %+v", kb)
	}
	if env.count(t, "kb_articles") != 0 || env.count(t, "kb_collections") != 0 || env.count(t, "import_items") != 0 || env.fake.count("/files/inline-kb-7001") != 0 {
		t.Fatalf("dry run wrote: articles=%d items=%d", env.count(t, "kb_articles"), env.count(t, "import_items"))
	}
}

func TestSolutionsAnImageThatFailsMakesTheRunFailedAndTheRerunBringsItOver(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.fake.fileStatus["inline-kb-7001"] = http.StatusInternalServerError
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || report.KnowledgeBase.Images.Failed != 1 || env.count(t, "kb_articles") != 6 {
		t.Fatalf("status=%s images=%+v", report.Status, report.KnowledgeBase.Images)
	}
	if body := env.kbArticles(t, env.workspaceID)["104"].Body; !strings.Contains(body, "inline-kb-7001") {
		t.Fatalf("the failed image's link was not kept: %q", body)
	}
	env.fake.mu.Lock()
	delete(env.fake.fileStatus, "inline-kb-7001")
	env.fake.mu.Unlock()
	report = env.mustRun(t)
	body := env.kbArticles(t, env.workspaceID)["104"].Body
	if report.Status != freshdesk.StatusOK || !strings.Contains(body, "/api/v1/kb/images/") || env.count(t, "kb_articles") != 6 {
		t.Fatalf("rerun status=%s failures=%+v body=%q", report.Status, report.Failures, body)
	}
}

// editArticle changes an imported article through the API store, as an agent would.
func (e *importEnv) editArticle(t *testing.T, articleID, body string) {
	t.Helper()
	s := store.New(e.pool)
	article, err := s.GetKBArticle(context.Background(), e.workspaceID, articleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateKBArticle(context.Background(), e.workspaceID, store.Actor{}, articleID, article.UpdatedAt, store.KBArticleChange{BodyMD: &body}); err != nil {
		t.Fatal(err)
	}
}

func TestSolutionsRerunUpdatesFreshdeskChangesAndKeepsColliedeskEdits(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.mustRun(t)
	before := env.kbArticles(t, env.workspaceID)
	env.editArticle(t, before["101"].ID, "Edited in Colliedesk.")
	env.editSolutions(func(fixture map[string]any) {
		for _, id := range []float64{101, 102} {
			article := fixtureArticle(fixture, "11", id)
			article["description"], article["updated_at"] = "<p>Changed in Freshdesk.</p>", "2024-06-01T10:00:00Z"
		}
		fixtureArticle(fixture, "11", 102)["status"] = 2.0
	})
	report := env.mustRun(t)
	after := env.kbArticles(t, env.workspaceID)
	if report.Status != freshdesk.StatusOK || len(after) != 6 || strings.Join(report.KnowledgeBase.ChangedInBoth, ",") != "101" || warningIDs(report, "kb_article_changed_in_both") != "101" {
		t.Fatalf("status=%s articles=%d changed=%v", report.Status, len(after), report.KnowledgeBase.ChangedInBoth)
	}
	if after["101"].Body != "Edited in Colliedesk." {
		t.Fatalf("the Colliedesk edit was overwritten: %q", after["101"].Body)
	}
	if upcoming := after["102"]; upcoming.Body != "Changed in Freshdesk." || upcoming.Status != "published" || !upcoming.UpdatedAt.Equal(time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)) || upcoming.ID != before["102"].ID {
		t.Fatalf("article 102 = %+v", upcoming)
	}
}

func TestSolutionsUndoRemovesTheImportKeepsEditedArticlesAndNeverTouchesAnotherWorkspace(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.mustRun(t)
	beta, _, err := env.auth.CreateWorkspace(ctx, "Beta Support", "admin@beta.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.run(t, ctx, func(o *freshdesk.Options) { o.WorkspaceSlug = "beta-support" }); err != nil {
		t.Fatal(err)
	}
	betaArticles := len(env.kbArticles(t, beta.ID))
	articles := env.kbArticles(t, env.workspaceID)
	env.editArticle(t, articles["101"].ID, "Edited in Colliedesk.")
	var basics string
	if err := env.pool.QueryRow(ctx, `SELECT collection_id::text FROM kb_articles WHERE id=$1`, articles["101"].ID).Scan(&basics); err != nil {
		t.Fatal(err)
	}
	own, err := store.New(env.pool).CreateKBArticle(ctx, env.workspaceID, store.Actor{}, store.KBArticleInput{CollectionID: basics, Title: "Our own", BodyMD: "Written here."})
	if err != nil {
		t.Fatal(err)
	}
	var imageKey string
	if err := env.pool.QueryRow(ctx, `SELECT storage_key FROM kb_images WHERE workspace_id=$1`, env.workspaceID).Scan(&imageKey); err != nil {
		t.Fatal(err)
	}

	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.KBArticles != 5 || strings.Join(result.KeptKBArticles, ",") != "101" || result.KBCollections != 5 || result.KBImages != 1 {
		t.Fatalf("undo = %+v", result)
	}
	if got := env.kbTree(t); got != "Getting started | Getting started > Basics" {
		t.Fatalf("tree after undo = %s", got)
	}
	left := env.kbArticles(t, env.workspaceID)
	if len(left) != 2 || left["101"].Body != "Edited in Colliedesk." || left[own.ID].Title != "Our own" {
		t.Fatalf("articles after undo = %+v", left)
	}
	// The image of article 104 goes with it; beta keeps its own copy.
	if _, err := os.Stat(filepath.Join(env.storageRoot, filepath.FromSlash(imageKey))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the imported image file is still stored: %v", err)
	}
	if len(env.kbArticles(t, beta.ID)) != betaArticles || betaArticles != 6 || env.countIn(t, beta.ID, "kb_images") != 1 {
		t.Fatalf("beta articles %d -> %d", betaArticles, len(env.kbArticles(t, beta.ID)))
	}
	// The kept article keeps its ledger row, so importing again does not add it twice.
	report := env.mustRun(t)
	if env.count(t, "kb_articles") != 7 || report.KnowledgeBase.Articles.Imported != 6 {
		t.Fatalf("reimport after undo: articles=%d report=%+v", env.count(t, "kb_articles"), report.KnowledgeBase.Articles)
	}
}

func TestSolutionsImagesCountTowardTheStorageCap(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.setPlan(t, env.workspaceID, "founding")
	kbBytes := int64(len(kbImageBody)) // the gone image counts nothing
	dry, err := env.run(t, context.Background(), func(o *freshdesk.Options) { o.DryRun, o.Cloud, o.PaidStorageBytes = true, true, 30+kbBytes-1 })
	if err != nil || dry.StorageCap == nil || !dry.StorageCap.WouldRefuse || dry.StorageCap.NeededBytes != 30+kbBytes {
		t.Fatalf("dry run: %+v, %v", dry.StorageCap, err)
	}
	var capErr *freshdesk.StorageCapError
	if _, err := env.runCapped(t, 30+kbBytes-1); !errors.As(err, &capErr) || env.count(t, "kb_articles") != 0 {
		t.Fatalf("over the cap: %v, articles=%d", err, env.count(t, "kb_articles"))
	}
	report, err := env.runCapped(t, 30+kbBytes)
	if err != nil || report.Status != freshdesk.StatusOK || env.storedBytes(t, env.workspaceID) != 30+kbBytes {
		t.Fatalf("fits: %v status=%v stored=%d", err, report, env.storedBytes(t, env.workspaceID))
	}
}

func TestSolutionsMarkupMarkdownCannotHoldIsNamed(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 101)["description"] = "<table><tr><th>A</th><th>B</th></tr><tr><td><p>one</p><p>two</p></td><td>x</td></tr></table><p>H<sub>2</sub>O</p>"
		fixtureArticle(fixture, "11", 102)["attachments"] = []any{map[string]any{"id": 1.0, "name": "guide.pdf", "size": 3.0}}
	})
	report := env.mustRun(t)
	if warningIDs(report, "kb_article_markup_dropped") != "101,106" || warningIDs(report, "kb_article_attachments_not_imported") != "102" {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	var meta string
	if err := env.pool.QueryRow(context.Background(), `SELECT source_meta->>'markup_dropped' FROM kb_articles WHERE workspace_id=$1 AND source_meta->>'freshdesk_id'='101'`, env.workspaceID).Scan(&meta); err != nil ||
		!strings.Contains(meta, "table with lines or lists inside a cell") || !strings.Contains(meta, "subscript or superscript") {
		t.Fatalf("markup_dropped = %s, %v", meta, err)
	}
	if body := env.kbArticles(t, env.workspaceID)["101"].Body; !strings.Contains(body, "one") || !strings.Contains(body, "two") || strings.Contains(body, "<") {
		t.Fatalf("body = %q", body)
	}
}

func TestSolutionsAnArticleDeletedForGoodIsNotBroughtBack(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.mustRun(t)
	if _, err := env.pool.Exec(context.Background(), `DELETE FROM kb_articles WHERE workspace_id=$1 AND source_meta->>'freshdesk_id'='102'`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	env.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 102)["updated_at"] = "2024-06-01T10:00:00Z"
	})
	report := env.mustRun(t)
	if strings.Join(report.KnowledgeBase.DeletedInColliedesk, ",") != "102" || len(report.KnowledgeBase.ChangedInBoth) != 0 || env.count(t, "kb_articles") != 5 {
		t.Fatalf("deleted=%v changed=%v articles=%d", report.KnowledgeBase.DeletedInColliedesk, report.KnowledgeBase.ChangedInBoth, env.count(t, "kb_articles"))
	}
}

func TestSolutionsAFolderNoLongerPublicMakesItsArticlesDraftsAgain(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.mustRun(t)
	articles := env.kbArticles(t, env.workspaceID)
	// An agent edits 104 (still published); 101 and 105 nobody touches.
	env.editArticle(t, articles["104"].ID, "Edited in Colliedesk.")
	// Only the folder changes in Freshdesk: Basics is now for agents only. No article's updated_at moves.
	env.editSolutions(func(fixture map[string]any) {
		fixture["category_folders"].(map[string]any)["1"].([]any)[0].(map[string]any)["visibility"] = 3.0
	})
	report := env.mustRun(t)
	after := env.kbArticles(t, env.workspaceID)
	if after["101"].Status != "draft" || after["101"].PublishedAt != nil || after["105"].Status != "draft" || after["106"].Status != "published" {
		t.Fatalf("statuses: 101=%s 105=%s 106=%s", after["101"].Status, after["105"].Status, after["106"].Status)
	}
	if after["104"].Status != "published" || warningIDs(report, "kb_article_published_but_not_public") != "104" {
		t.Fatalf("104=%s warnings=%+v", after["104"].Status, report.Warnings)
	}
	if strings.Join(report.KnowledgeBase.Unpublished, ",") != "101,105" || strings.Join(report.KnowledgeBase.Published, ",") != "106" || report.Status != freshdesk.StatusOK {
		t.Fatalf("status=%s unpublished=%v published=%v", report.Status, report.KnowledgeBase.Unpublished, report.KnowledgeBase.Published)
	}
	// Making it public again in Freshdesk never publishes by itself.
	env.editSolutions(nil)
	env.mustRun(t)
	if again := env.kbArticles(t, env.workspaceID); again["101"].Status != "draft" {
		t.Fatalf("101 was published again by an import: %s", again["101"].Status)
	}
}

func TestSolutionsCollapsibleSectionsAndEmptyAnchorsConvertCleanly(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 101)["description"] = `<p><a name="top"></a>Intro</p><details><summary>More</summary><p>Hidden text</p></details>`
	})
	report := env.mustRun(t)
	body := env.kbArticles(t, env.workspaceID)["101"].Body
	if body != "Intro\n\nMore\n\nHidden text" || !strings.Contains(warningIDs(report, "kb_article_markup_dropped"), "101") {
		t.Fatalf("body = %q warnings=%+v", body, report.Warnings)
	}
}

// A YouTube or Vimeo player is kept as the editor's Insert video writes it, so the help center shows it
// after the import; any other iframe or a video file is left out and named, as before.
func TestSolutionsKeepYouTubeAndVimeoPlayers(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	env.editSolutions(func(fixture map[string]any) {
		fixtureArticle(fixture, "11", 101)["description"] = `<p>Intro</p><iframe width="560" height="315" src="https://www.youtube.com/embed/jNQXAC9IVRw?si=x&amp;start=7" title="Tour" frameborder="0" onload="alert(1)" allowfullscreen></iframe>` +
			`<p>Watch <iframe src="https://player.vimeo.com/video/76979871"></iframe> inline.</p><iframe src="https://evil.example/embed/x"></iframe><video src="https://cdn.example/a.mp4" controls></video>`
		// Text beside a player keeps showing: a player gets a paragraph of its own (a table cell keeps it in its row).
		fixtureArticle(fixture, "11", 102)["description"] = `<p>See</p><div class="video"><iframe src="https://player.vimeo.com/video/1?h=ab12cd34ef"></iframe><br>Step 1: press play</div>` +
			`<ul><li><iframe src="https://player.vimeo.com/video/2"></iframe> item text</li></ul><table><tr><th>A</th></tr><tr><td><iframe src="https://player.vimeo.com/video/3"></iframe> cell</td></tr></table>`
	})
	report := env.mustRun(t)
	if warningIDs(report, "kb_article_markup_dropped") != "101,106" {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	articles := env.kbArticles(t, env.workspaceID)
	want := "Intro\n\n" +
		`<iframe src="https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=7" title="Tour" width="560" height="315" allowfullscreen="" loading="lazy"></iframe>` + "\n\n" +
		"Watch\n\n" + `<iframe src="https://player.vimeo.com/video/76979871" title="Vimeo video" loading="lazy"></iframe>` + "\n\ninline."
	if body := articles["101"].Body; body != want {
		t.Fatalf("body =\n%q\nwant\n%q", body, want)
	}
	for _, want := range []string{
		"See\n\n" + `<iframe src="https://player.vimeo.com/video/1?h=ab12cd34ef" title="Vimeo video" loading="lazy"></iframe>` + "\n\nStep 1: press play",
		"- " + `<iframe src="https://player.vimeo.com/video/2" title="Vimeo video" loading="lazy"></iframe>` + "\n  \n  item text",
		"| " + `<iframe src="https://player.vimeo.com/video/3" title="Vimeo video" loading="lazy"></iframe>` + " cell |",
	} {
		if body := articles["102"].Body; !strings.Contains(body, want) {
			t.Fatalf("102 body = %q\nwant it to hold %q", body, want)
		}
	}
	var meta string
	if err := env.pool.QueryRow(context.Background(), `SELECT source_meta->>'markup_dropped' FROM kb_articles WHERE workspace_id=$1 AND source_meta->>'freshdesk_id'='101'`, env.workspaceID).Scan(&meta); err != nil ||
		meta != `["iframe", "video"]` {
		t.Fatalf("markup_dropped = %s, %v", meta, err)
	}
	if !strings.Contains(articles["101"].SourceHTML, "evil.example") || !strings.Contains(articles["101"].SourceHTML, "<video") {
		t.Fatalf("source_html = %q", articles["101"].SourceHTML)
	}
}
