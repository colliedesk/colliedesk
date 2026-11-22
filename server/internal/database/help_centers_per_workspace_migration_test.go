package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const helpCentersPerWorkspaceMigration = "20261122032256-help-centers-per-workspace.up.sql"

// A workspace with a help center keeps it through the move to several help centers per workspace:
// the row becomes the workspace's primary help center with every setting it had, and its custom
// domain, languages and analytics belong to that primary. Every top-level collection, trashed ones
// too, is placed on the primary in today's home order; nested collections follow their top. A
// workspace that only had collections gets a primary help center that is still off.
func TestHelpCentersPerWorkspaceMigrationKeepsEachWorkspacesHelpCenter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, helpCentersPerWorkspaceMigration); err != nil {
		t.Fatalf("roll back: %v", err)
	}
	ids := seedTwoWorkspaces(t, pool)
	seedHelpCenterOnPreviousSchema(t, pool, ids)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertPrimaryKeptHelpCenter(t, pool, ids)

	if err := rollbackThrough(ctx, pool, helpCentersPerWorkspaceMigration); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got := query(t, pool, `SELECT h.title||'|'||d.hostname||'|'||
		(SELECT string_agg(locale, ',' ORDER BY locale) FROM help_center_languages l
			WHERE l.workspace_id=h.workspace_id)||'|'||a.ga4_measurement_id
		FROM help_centers h JOIN help_center_domains d USING (workspace_id)
		JOIN help_center_analytics a USING (workspace_id) WHERE h.workspace_id=$1`, ids.a)
	if want := "Alpha help|help.alpha.example|es,fr|G-ALPHA1234"; got != want {
		t.Fatalf("after rollback = %q, want %q", got, want)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
	assertPrimaryKeptHelpCenter(t, pool, ids)
}

// seedHelpCenterOnPreviousSchema writes what an install has today: alpha has a help center that is
// on, themed, on its own domain, in two more languages and sending page views to GA4, with four
// top-level collections (one trashed) and a nested one. beta never turned its help center on but
// has written a collection.
func seedHelpCenterOnPreviousSchema(t *testing.T, pool *pgxpool.Pool, ids twoWorkspaces) {
	t.Helper()
	statements := []string{
		`INSERT INTO help_centers(workspace_id,enabled,title,locale,color_primary,custom_css,
			google_site_verification,content_version,last_article_number)
			VALUES($1,true,'Alpha help','de','#0b5cad','.x{color:red}','abcdefghijklmnop1234',7,42)`,
		`INSERT INTO help_center_domains(workspace_id,hostname,token,claim,status,verified_at)
			VALUES($1,'help.alpha.example',md5('alpha'),'aaaaaaaaaaaaaaaaaaaaaaaa','active',
			'2026-11-01T00:00:00Z')`,
		`INSERT INTO help_center_languages(workspace_id,locale,visible,position)
			VALUES($1,'fr',true,0),($1,'es',false,1)`,
		`INSERT INTO help_center_analytics(workspace_id,provider,enabled,ga4_measurement_id)
			VALUES($1,'ga4',true,'G-ALPHA1234')`,
		`INSERT INTO kb_collections(id,workspace_id,name,slug,position,deleted_at) VALUES
			('20000000-0000-4000-8000-000000000001',$1,'Setup','setup',2,NULL),
			('20000000-0000-4000-8000-000000000002',$1,'Billing','billing',0,NULL),
			('20000000-0000-4000-8000-000000000003',$1,'Old','old',1,now()),
			('20000000-0000-4000-8000-000000000004',$1,'Api','api',2,NULL)`,
		`INSERT INTO kb_collections(id,workspace_id,parent_id,name,slug,position) VALUES
			('20000000-0000-4000-8000-000000000005',$1,'20000000-0000-4000-8000-000000000001',
			'Install','install',0)`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(context.Background(), statement, ids.a); err != nil {
			t.Fatalf("seed alpha: %v\n%s", err, statement)
		}
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO kb_collections(id,workspace_id,name,slug)
		VALUES('20000000-0000-4000-8000-000000000006',$1,'Docs','docs')`, ids.b); err != nil {
		t.Fatalf("seed beta: %v", err)
	}
}

// Booleans read as PostgreSQL writes them in text: t and f.
func assertPrimaryKeptHelpCenter(t *testing.T, pool *pgxpool.Pool, ids twoWorkspaces) {
	t.Helper()
	checks := []struct{ name, sql, want string }{
		{"alpha help centers", `SELECT string_agg(concat_ws('|', primary_help_center, handle IS NULL,
			deleted_at IS NULL, enabled, title, locale, color_primary, custom_css, google_site_verification,
			content_version, last_article_number), ';') FROM help_centers WHERE workspace_id=$1`,
			"t|t|t|t|Alpha help|de|#0b5cad|.x{color:red}|abcdefghijklmnop1234|7|42"},
		{"alpha domain", `SELECT concat_ws('|', d.hostname, d.status, d.help_center_id=h.id)
			FROM help_center_domains d JOIN help_centers h ON h.workspace_id=d.workspace_id
			WHERE d.workspace_id=$1`, "help.alpha.example|active|t"},
		{"alpha languages", `SELECT string_agg(concat_ws('|', l.locale, l.visible, l.position,
			l.help_center_id=h.id), ';' ORDER BY l.position) FROM help_center_languages l
			JOIN help_centers h ON h.workspace_id=l.workspace_id WHERE l.workspace_id=$1`,
			"fr|t|0|t;es|f|1|t"},
		{"alpha analytics", `SELECT concat_ws('|', a.provider, a.enabled, a.ga4_measurement_id,
			a.help_center_id=h.id) FROM help_center_analytics a
			JOIN help_centers h ON h.workspace_id=a.workspace_id WHERE a.workspace_id=$1`,
			"ga4|t|G-ALPHA1234|t"},
		{"alpha home order", homeOrderSQL, "Billing 0;Old 1;Api 2;Setup 2"},
		{"beta help center", `SELECT concat_ws('|', primary_help_center, enabled, title, locale)
			FROM help_centers WHERE workspace_id=$1`, "t|f||en"},
		{"beta home order", homeOrderSQL, "Docs 0"},
	}
	for _, check := range checks {
		id := ids.a
		if strings.HasPrefix(check.name, "beta") {
			id = ids.b
		}
		if got := query(t, pool, check.sql, id); got != check.want {
			t.Errorf("%s = %q, want %q", check.name, got, check.want)
		}
	}
}

// homeOrderSQL lists the collections placed on a workspace's primary help center in home order.
const homeOrderSQL = `SELECT string_agg(c.name||' '||l.position, ';' ORDER BY l.position, c.name)
	FROM kb_collection_help_centers l JOIN help_centers h
		ON h.workspace_id=l.workspace_id AND h.id=l.help_center_id AND h.primary_help_center
	JOIN kb_collections c ON c.workspace_id=l.workspace_id AND c.id=l.collection_id
	WHERE l.workspace_id=$1`

func query(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var got *string
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&got); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
	if got == nil {
		return "<null>"
	}
	return fmt.Sprint(*got)
}

// Code written for one help center per workspace keeps working: a new top-level collection goes on
// the primary at its position, a collection moved to the top joins it and one nested under another
// leaves it, and a domain, language or analytics row written without a help center belongs to the
// primary, which is made when the workspace has none yet.
func TestOneHelpCenterWritesLandOnThePrimary(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, pool)
	statements := []string{
		`INSERT INTO help_center_languages(workspace_id,locale) VALUES($1,'fr')`,
		`INSERT INTO help_center_domains(workspace_id,hostname,token,claim)
			VALUES($1,'help.b.example',md5('b'),'bbbbbbbbbbbbbbbbbbbbbbbb')`,
		`INSERT INTO help_center_analytics(workspace_id,provider) VALUES($1,'plausible')`,
		`INSERT INTO kb_collections(id,workspace_id,name,slug,position) VALUES
			('20000000-0000-4000-8000-000000000011',$1,'Docs','docs',3),
			('20000000-0000-4000-8000-000000000012',$1,'Faq','faq',1)`,
		`INSERT INTO kb_collections(id,workspace_id,parent_id,name,slug) VALUES
			('20000000-0000-4000-8000-000000000013',$1,'20000000-0000-4000-8000-000000000011','Sub','sub')`,
		`UPDATE kb_collections SET parent_id=NULL, position=0
			WHERE workspace_id=$1 AND id='20000000-0000-4000-8000-000000000013'`,
		`UPDATE kb_collections SET parent_id='20000000-0000-4000-8000-000000000011'
			WHERE workspace_id=$1 AND id='20000000-0000-4000-8000-000000000012'`,
		`UPDATE kb_collections SET position=5 WHERE workspace_id=$1 AND id='20000000-0000-4000-8000-000000000011'`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, ids.b); err != nil {
			t.Fatalf("%v\n%s", err, statement)
		}
	}
	if got, want := query(t, pool, homeOrderSQL, ids.b), "Sub 0;Docs 5"; got != want {
		t.Errorf("home order = %q, want %q", got, want)
	}
	got := query(t, pool, `SELECT count(*)::text||' '||bool_and(x.help_center_id=h.id)::text FROM (
		SELECT workspace_id, help_center_id FROM help_center_languages UNION ALL
		SELECT workspace_id, help_center_id FROM help_center_domains UNION ALL
		SELECT workspace_id, help_center_id FROM help_center_analytics) x
		JOIN help_centers h ON h.workspace_id=x.workspace_id WHERE x.workspace_id=$1`, ids.b)
	if got != "3 true" {
		t.Errorf("rows on the primary = %q, want %q", got, "3 true")
	}
}
