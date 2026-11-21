package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const kbExportHeader = "\xEF\xBB\xBFtitle,address,collection,status,author,tags,languages,created_at,updated_at,helpful_votes,not_helpful_votes\n"

// newKBExportFixture is the filter fixture with names on its team, a writer who has left, votes on the
// card refund, a sixth article whose title a spreadsheet would read as a formula, and a seventh whose
// title starts with a tab in a collection whose name starts with a carriage return.
func newKBExportFixture(t *testing.T) kbFilterFixture {
	t.Helper()
	f := newKBFilterFixture(t)
	formula := kbArticle(t, f.api, f.admin, map[string]any{"collection_id": f.billing, "slug": "refund-link",
		"title": `=HYPERLINK("https://evil.example","Refund")`, "body_md": "Click."}).ID
	f.ids["formula"] = formula
	fixtureSQL(t, f.pool, `UPDATE kb_articles SET created_at='2026-09-01T00:00:00Z', updated_at='2026-09-01T00:00:00Z' WHERE id=$1`, formula)
	staff := kbCollection(t, f.api, f.admin, map[string]any{"name": "Staff"}).ID
	tab := kbArticle(t, f.api, f.admin, map[string]any{"collection_id": staff, "slug": "tab-first", "title": "Tab first", "body_md": "Hidden."}).ID
	fixtureSQL(t, f.pool, `UPDATE kb_collections SET name=E'\rStaff' WHERE id=$1`, staff)
	fixtureSQL(t, f.pool, `UPDATE kb_articles SET title=E'\tTab first', created_at='2026-08-01T00:00:00Z', updated_at='2026-08-01T00:00:00Z' WHERE id=$1`, tab)
	fixtureSQL(t, f.pool, `UPDATE agents SET display_name=CASE WHEN id=$1 THEN 'Ana Admin' ELSE 'Wes Writer' END WHERE id IN ($1,$2)`, f.adminID, f.writerID)
	fixtureSQL(t, f.pool, `UPDATE agents SET deactivated_at=now() WHERE id=$1`, f.writerID)
	fixtureSQL(t, f.pool, `INSERT INTO kb_article_votes(workspace_id,article_id,voter,address,helpful)
		SELECT a.workspace_id, a.id, decode(repeat(v.voter,32),'hex'), decode(repeat('0a',32),'hex'), v.helpful
		FROM kb_articles a, (VALUES ('01',true),('02',true),('03',false)) v(voter,helpful) WHERE a.id=$1`, f.ids["card"])
	return f
}

func kbExport(t *testing.T, f kbFilterFixture, query string) *httptest.ResponseRecorder {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/kb/articles/export.csv?"+query, nil, f.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export %q = %d: %s", query, response.Code, response.Body.String())
	}
	return response
}

// The export is the list as a spreadsheet: every column filled from the articles as they are, dates
// in the team member's timezone, and a title that starts like a formula kept as text.
func TestKnowledgeBaseExportIsTheListAsCSV(t *testing.T) {
	f := newKBExportFixture(t)
	want, err := os.ReadFile("testdata/kb-articles-export.csv")
	if err != nil {
		t.Fatal(err)
	}
	response := kbExport(t, f, "")
	if got := response.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header().Get("Content-Disposition"); got != `attachment; filename="knowledge-base-articles.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := response.Body.String(); got != string(want) {
		t.Fatalf("export differs from testdata/kb-articles-export.csv:\n%s", got)
	}
}

// The export takes the list's filters: only the articles they keep, in the list's order.
func TestKnowledgeBaseExportTakesTheListFilters(t *testing.T) {
	f := newKBExportFixture(t)
	lines := strings.Split(strings.TrimSuffix(kbExport(t, f, "tag=billing").Body.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[1], "Invoice copy,") || !strings.HasPrefix(lines[2], "Card refund,") {
		t.Fatalf("tag=billing exported %q, want the header, Invoice copy and Card refund", lines)
	}
	refused := request(t, f.api, http.MethodGet, "/api/v1/kb/articles/export.csv?created_after=yesterday", nil, f.admin, nil)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("a bad date = %d, want 400: %s", refused.Code, refused.Body.String())
	}
}

// A failure after the file started breaks the download, so a short file that still reads as a whole
// list never reaches the person: here the vote counts of the first page cannot be read.
func TestKnowledgeBaseExportFailureAfterTheStartAbortsTheDownload(t *testing.T) {
	f := newKBExportFixture(t)
	fixtureSQL(t, f.pool, `DROP TABLE kb_article_vote_resets CASCADE`)
	var aborted any
	func() {
		defer func() { aborted = recover() }()
		response := request(t, f.api, http.MethodGet, "/api/v1/kb/articles/export.csv", nil, f.admin, nil)
		t.Errorf("export ended with %d and %q, want the connection aborted", response.Code, response.Body.String())
	}()
	if aborted != http.ErrAbortHandler {
		t.Fatalf("handler ended with %v, want the connection to be aborted", aborted)
	}
}

// Another workspace's collection or author is no filter value here: the file has its header only.
func TestKnowledgeBaseExportNeverReadsAnotherWorkspace(t *testing.T) {
	f := newKBExportFixture(t)
	for _, query := range []string{"collection_id=" + f.foreignCollectionID, "author=" + f.foreignAgentID} {
		if got := kbExport(t, f, query).Body.String(); got != kbExportHeader {
			t.Errorf("%s exported %q, want the header only", query, got)
		}
	}
}
