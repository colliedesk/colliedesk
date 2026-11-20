package freshdesk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

func (e *importEnv) withArticleFiles(files ...map[string]any) {
	list := make([]any, len(files))
	for index, file := range files {
		list[index] = file
	}
	e.editSolutions(func(fixture map[string]any) { fixtureArticle(fixture, "11", 102)["attachments"] = list })
}

// kbFileList is the live list of an imported article (locale "" for its own list), as file names in order.
func (e *importEnv) kbFileList(t *testing.T, freshdeskID, locale string) string {
	t.Helper()
	var names string
	if err := e.pool.QueryRow(context.Background(), `SELECT COALESCE(string_agg(m.name, ',' ORDER BY l.position), '')
		FROM kb_article_attachments l JOIN kb_articles a ON a.workspace_id=l.workspace_id AND a.id=l.article_id
		JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id
		WHERE l.workspace_id=$1 AND a.source_meta->>'freshdesk_id'=$2 AND COALESCE(l.locale, '')=$3`, e.workspaceID, freshdeskID, locale).Scan(&names); err != nil {
		t.Fatal(err)
	}
	return names
}

func (e *importEnv) kbFileCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_media WHERE workspace_id=$1 AND kind='file'`, e.workspaceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The files of a Freshdesk article come over in their order, as files of the media library the
// article lists under its body; the text of the PDF is kept for search.
func TestSolutionsArticleFilesAreImportedInOrder(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	manual, parts := string(pdfsample.PDF("torque wrench settings", 3000)), "part,count\nscrew,4\n"
	env.fake.fileBody["kb-8001"], env.fake.fileBody["kb-8002"] = manual, parts
	env.withArticleFiles(
		map[string]any{"id": 1.0, "name": "Manual.pdf", "content_type": "application/pdf", "size": float64(len(manual)), "attachment_url": env.fake.files.URL + "/kb-8001"},
		map[string]any{"id": 2.0, "name": "Parts.csv", "content_type": "text/csv", "size": float64(len(parts)), "attachment_url": env.fake.files.URL + "/kb-8002"})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusOK || report.KnowledgeBase.Files != (freshdesk.EntityCount{Source: 2, Imported: 2}) {
		t.Fatalf("status=%s files=%+v failures=%+v", report.Status, report.KnowledgeBase.Files, report.Failures)
	}
	if got := env.kbFileList(t, "102", ""); got != "Manual.pdf,Parts.csv" {
		t.Fatalf("article 102 lists %q, want Manual.pdf,Parts.csv", got)
	}
	var text string
	if err := env.pool.QueryRow(context.Background(), `SELECT COALESCE(text, '') FROM kb_media WHERE workspace_id=$1 AND name='Manual.pdf'`, env.workspaceID).Scan(&text); err != nil ||
		!strings.Contains(text, "torque wrench settings") {
		t.Fatalf("the manual's text = %q, %v", text, err)
	}
	if warningIDs(report, "kb_article_attachments_not_imported") != "" {
		t.Fatalf("warnings = %+v", report.Warnings)
	}
	if again := env.mustRun(t); again.Status != freshdesk.StatusOK || env.kbFileCount(t) != 2 || env.kbFileList(t, "102", "") != "Manual.pdf,Parts.csv" {
		t.Fatalf("rerun: status=%s files=%d list=%q", again.Status, env.kbFileCount(t), env.kbFileList(t, "102", ""))
	}
}

// A file Colliedesk refuses (a program, one over 25 MB, one whose bytes are not what its name says)
// is a failure with its reason; the article itself is imported, and the import is not OK.
func TestSolutionsArticleFilesColliedeskRefusesAreReported(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	program := "MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff\x00\x00"
	env.fake.fileBody["kb-8101"], env.fake.fileBody["kb-8103"] = program, program
	env.withArticleFiles(
		map[string]any{"id": 1.0, "name": "setup.exe", "size": float64(len(program)), "attachment_url": env.fake.files.URL + "/kb-8101"},
		map[string]any{"id": 2.0, "name": "Big.pdf", "size": float64(26 << 20), "attachment_url": env.fake.files.URL + "/kb-8102"},
		map[string]any{"id": 3.0, "name": "Report.pdf", "size": float64(len(program)), "attachment_url": env.fake.files.URL + "/kb-8103"})
	report := env.mustRun(t)
	if report.Status != freshdesk.StatusFailed || report.KnowledgeBase.Files != (freshdesk.EntityCount{Source: 3, Failed: 3}) {
		t.Fatalf("status=%s files=%+v", report.Status, report.KnowledgeBase.Files)
	}
	reasons := map[string]string{}
	for _, failure := range report.Failures {
		if failure.Entity == "kb_file" {
			reasons[failure.FreshdeskID] = failure.Error
		}
	}
	if !strings.Contains(reasons["102:1"], "PDF") || !strings.Contains(reasons["102:2"], "25 MiB") || !strings.Contains(reasons["102:3"], "PDF") {
		t.Fatalf("file failures = %v", reasons)
	}
	if _, imported := env.kbArticles(t, env.workspaceID)["102"]; !imported || env.kbFileCount(t) != 0 {
		t.Fatalf("article imported=%v files=%d", imported, env.kbFileCount(t))
	}
}

// Undo removes the files the import brought, with the articles that list them.
func TestSolutionsUndoRemovesImportedFiles(t *testing.T) {
	env := newImportEnv(t).withSolutions()
	ctx := context.Background()
	env.fake.fileBody["kb-8201"] = "part,count\nscrew,4\n"
	env.withArticleFiles(map[string]any{"id": 1.0, "name": "Parts.csv", "size": 19.0, "attachment_url": env.fake.files.URL + "/kb-8201"})
	if report := env.mustRun(t); report.Status != freshdesk.StatusOK || env.kbFileCount(t) != 1 {
		t.Fatalf("status=%s files=%d failures=%+v", report.Status, env.kbFileCount(t), report.Failures)
	}
	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.KBFiles != 1 || env.kbFileCount(t) != 0 || env.count(t, "kb_article_attachments") != 0 {
		t.Fatalf("undo = %+v, files left %d", result, env.kbFileCount(t))
	}
}
