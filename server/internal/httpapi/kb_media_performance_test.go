package httpapi_test

import (
	"testing"
	"time"
)

// kbMediaLatencyBudget is the media library's budget for a page: the design measured 2–7 ms under
// row-level security at 10,000 items, so 25 ms leaves CI room and still catches a lost index.
const kbMediaLatencyBudget = 25 * time.Millisecond

// seedKBMediaBudget gives workspace "KB Budget A" 10,000 library items (the cap), five links on each
// of its articles (three current), and 20 more workspaces 2,000 items each, so the planner sees a
// table where one workspace is a part, as on Cloud, and A's rows spread over its pages as interleaved
// uploads leave them. 10,000 each took 4 s more to seed against the CI budget.
func seedKBMediaBudget(t *testing.T, exec func(sql string, args ...any) error) {
	t.Helper()
	started := time.Now()
	for _, statement := range []string{
		`INSERT INTO workspaces(slug,name,trial_ends_at) SELECT 'kb-media-noise-'||g, 'KB Media Noise '||g, now()+interval '14 days' FROM generate_series(1,20) g`,
		// One insert, workspace A's rows spread evenly between the others' (each workspace's n-th of its
		// items in turn), so A's 10,000 rows sit on as many pages as uploads that interleave would put them.
		`INSERT INTO kb_media(id,workspace_id,storage_key,sha256,size,content_type,name,alt,width,height,inspected_at,created_at,last_uploaded_at)
			SELECT r.id, r.workspace_id, r.workspace_id::text||'/'||left(r.h,2)||'/'||r.h, r.h, 1000+(r.g*7919)%5000000, 'image/png', r.name, r.alt,
				1280, 720, now(), now()-(r.g||' s')::interval, now()-(r.g||' s')::interval
			FROM (SELECT md5('kbm-'||g)::uuid AS id, w.id AS workspace_id, md5(w.id::text||g)||md5(g||w.id::text) AS h, g, 'Screenshot '||g AS name,
					CASE WHEN g%3=0 THEN '' ELSE 'Settings page '||g END AS alt, g/10000.0 AS place
				FROM workspaces w, generate_series(1,10000) g WHERE w.name='KB Budget A'
				UNION ALL
				SELECT gen_random_uuid(), w.id, md5(w.id::text||g)||md5(g||w.id::text), g, 'Noise '||g, '', g/2000.0
				FROM workspaces w, generate_series(1,2000) g WHERE w.name LIKE 'KB Media Noise %') r
			ORDER BY r.place`,
		`INSERT INTO kb_article_media(workspace_id,article_id,media_id,current)
			SELECT a.workspace_id, a.id, md5('kbm-'||((a.number*5+k)%10000+1))::uuid, k<=3
			FROM kb_articles a JOIN workspaces w ON w.id=a.workspace_id AND w.name='KB Budget A', generate_series(1,5) k`,
		// No VACUUM: right after an import the visibility map is not current yet, and the gate measures that.
		`ANALYZE kb_media`,
		`ANALYZE kb_article_media`,
	} {
		if err := exec(statement); err != nil {
			t.Fatalf("seed the media library: %v", err)
		}
	}
	t.Logf("seeded the media library in %s", time.Since(started))
}

// TestKBMediaLibraryLatencyBudget holds the library to kbMediaLatencyBudget p95 at the item cap, in
// the shared knowledge base fixture: the first page, a search, the unused page with its summary
// (the largest first), the items in use and the items missing alt text.
func TestKBMediaLibraryLatencyBudget(t *testing.T) {
	api, _, token, _ := sharedKBBudget(t)
	for name, path := range map[string]string{
		"first page":  "/api/v1/kb/media",
		"search":      "/api/v1/kb/media?q=settings+page+77",
		"unused":      "/api/v1/kb/media?usage=unused&sort=largest",
		"in use":      "/api/v1/kb/media?usage=in_use",
		"missing alt": "/api/v1/kb/media?usage=missing_alt&sort=name",
	} {
		p95 := inboxP95(t, api, token, path)
		t.Logf("media library %s p95=%s", name, p95)
		if p95 > kbMediaLatencyBudget {
			t.Errorf("media library %s p95 %s exceeds %s", name, p95, kbMediaLatencyBudget)
		}
	}
}
