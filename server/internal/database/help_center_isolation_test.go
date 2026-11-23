package database

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// helpCenterSeed is workspace a of seedTwoWorkspaces with the knowledge base of seedKB on its primary
// help center and a second help center over the same knowledge base; workspace b has its own seedKB.
type helpCenterSeed struct {
	ids                        twoWorkspaces
	primary, second            string
	docs, shelf, nested        string
	first, secondOnly, private string
}

// seedHelpCenters places, beside seedKB's docs and more on the primary: "Second shelf" on the second
// help center only, with "Second nested" inside it holding article 2 (a translation, a tag, an image
// and votes), the second help center's logo and language, and article 1 also listed in Second nested.
// "Hidden" is placed on no help center and holds article 3. The primary gets a language, a
// collection translation and a tag of its own.
func seedHelpCenters(t *testing.T, pool *pgxpool.Pool) helpCenterSeed {
	t.Helper()
	ctx := context.Background()
	s := helpCenterSeed{ids: seedTwoWorkspaces(t, pool)}
	db := New(pool)
	for workspace, name := range map[string]string{s.ids.a: "Alpha", s.ids.b: "Beta"} {
		if err := db.InWorkspace(ctx, workspace, func(tx pgx.Tx) error { seedKB(t, ctx, tx, workspace, name); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	err := db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		row := func(sql string, args ...any) string {
			t.Helper()
			var id string
			if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return id
		}
		ws := s.ids.a
		s.primary = row(`SELECT primary_help_center_id($1)::text`, ws)
		s.docs = row(`SELECT id::text FROM kb_collections WHERE workspace_id=$1 AND slug='docs'`, ws)
		s.first = row(`SELECT id::text FROM kb_articles WHERE workspace_id=$1 AND number=1`, ws)
		s.second = row(`INSERT INTO help_centers(workspace_id,handle,primary_help_center,title) VALUES($1,'second',false,'Second') RETURNING id::text`, ws)
		row(`WITH m AS (INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type) VALUES($1::uuid,$1::uuid::text||'/0a/'||repeat('a',64),repeat('a',64),10,'image/png') RETURNING id)
			UPDATE help_centers h SET logo_media_id=m.id FROM m WHERE h.workspace_id=$1::uuid AND h.id=$2::uuid RETURNING h.id::text`, ws, s.second)
		s.shelf = row(`INSERT INTO kb_collections(workspace_id,name,slug) VALUES($1,'Second shelf','second-shelf') RETURNING id::text`, ws)
		row(`WITH d AS (DELETE FROM kb_collection_help_centers WHERE workspace_id=$1 AND collection_id=$2 RETURNING 1)
			INSERT INTO kb_collection_help_centers(workspace_id,collection_id,help_center_id) VALUES($1,$2,$3) RETURNING collection_id::text`, ws, s.shelf, s.second)
		s.nested = row(`INSERT INTO kb_collections(workspace_id,parent_id,name,slug) VALUES($1,$2,'Second nested','second-nested') RETURNING id::text`, ws, s.shelf)
		s.secondOnly = row(`INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md) VALUES($1,$2,2,'Second only','b','text') RETURNING id::text`, ws, s.nested)
		hidden := row(`INSERT INTO kb_collections(workspace_id,name,slug,visibility) VALUES($1,'Hidden','hidden','agents') RETURNING id::text`, ws)
		row(`DELETE FROM kb_collection_help_centers WHERE workspace_id=$1 AND collection_id=$2 RETURNING collection_id::text`, ws, hidden)
		s.private = row(`INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md) VALUES($1,$2,3,'Unplaced','c','text') RETURNING id::text`, ws, hidden)
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO kb_collection_translations(workspace_id,collection_id,locale,name) VALUES($1,$2,'de','Zweite'),($1,$3,'fr','Docs fr')`, []any{ws, s.nested, s.docs}},
			{`INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md,slug) VALUES($1,$2,'de','Zweite','text','z')`, []any{ws, s.secondOnly}},
			{`INSERT INTO kb_article_tags(workspace_id,article_id,tag) VALUES($1,$2,'second'),($1,$3,'alpha')`, []any{ws, s.secondOnly, s.first}},
			{`WITH m AS (INSERT INTO kb_media(workspace_id,storage_key,sha256,size,content_type) VALUES($1::uuid,$1::uuid::text||'/0b/'||repeat('b',64),repeat('b',64),10,'image/png') RETURNING id)
				INSERT INTO kb_article_media(workspace_id,article_id,media_id,current) SELECT $1::uuid,$2::uuid,id,true FROM m`, []any{ws, s.secondOnly}},
			{`WITH r AS (INSERT INTO kb_article_vote_resets(workspace_id,article_id,helpful,not_helpful) VALUES($1::uuid,$2::uuid,0,1) RETURNING id)
				INSERT INTO kb_article_votes(workspace_id,article_id,voter,address,helpful,reset_id) SELECT $1::uuid,$2::uuid,sha256('second'::bytea),sha256('2'::bytea),false,id FROM r`, []any{ws, s.secondOnly}},
			{`INSERT INTO kb_article_vote_versions(workspace_id,article_id,version) VALUES($1,$2,1)`, []any{ws, s.secondOnly}},
			{`INSERT INTO help_center_languages(workspace_id,help_center_id,locale,visible) VALUES($1,$2,'de',true),($1,$3,'fr',true)`, []any{ws, s.second, s.primary}},
			{`INSERT INTO kb_article_collections(workspace_id,article_id,collection_id) VALUES($1,$2,$3)`, []any{ws, s.first, s.nested}},
		} {
			if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
				t.Fatalf("%s: %v", step.sql, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// helpCenterTables are the tables a reader of a help center may read, each with the rows the reader
// of the primary and of the second help center of seedHelpCenters sees.
var helpCenterTables = map[string][2]int{
	"help_centers": {1, 1}, "help_center_languages": {1, 1}, "kb_collection_help_centers": {2, 1},
	"kb_collections": {2, 2}, "kb_collection_translations": {1, 1}, "kb_article_collections": {1, 1},
	"kb_articles": {1, 2}, "kb_article_translations": {1, 2}, "kb_article_media": {1, 2},
	"kb_article_attachments": {1, 1}, "kb_article_tags": {1, 2}, "kb_article_vote_resets": {1, 2},
	"kb_article_votes": {1, 2}, "kb_article_vote_versions": {1, 2}, "kb_media": {2, 4},
}

func readerCounts(t *testing.T, db *DB, workspaceID, helpCenterID string) map[string]int {
	t.Helper()
	got := map[string]int{}
	err := db.InHelpCenter(context.Background(), workspaceID, helpCenterID, func(tx pgx.Tx) error {
		for _, table := range slices.Sorted(maps.Keys(helpCenterTables)) {
			var count int
			// Deliberately unfiltered: row-level security alone must scope it.
			if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil {
				return err
			}
			got[table] = count
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A reader of one help center sees only what that help center shows: collections placed on it and
// those inside them, the articles at home or listed there and what hangs off them. Nothing of the
// other help center, of an unplaced collection or of another workspace reaches it.
func TestAHelpCenterReaderSeesOnlyItsOwnHelpCenter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	db := New(pool)
	for i, helpCenter := range []string{s.primary, s.second} {
		got := readerCounts(t, db, s.ids.a, helpCenter)
		for table, want := range helpCenterTables {
			if got[table] != want[i] {
				t.Errorf("reader of help center %d sees %d rows of %s, want %d", i+1, got[table], table, want[i])
			}
		}
	}
	err := db.InHelpCenter(ctx, s.ids.a, s.primary, func(tx pgx.Tx) error {
		var leaked int
		err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM kb_articles WHERE id IN ($1::uuid,$2::uuid))
			+ (SELECT count(*) FROM kb_collections WHERE id IN ($3::uuid,$4::uuid))
			+ (SELECT count(*) FROM help_centers WHERE id=$5::uuid OR workspace_id<>$6::uuid)`,
			s.secondOnly, s.private, s.shelf, s.nested, s.second, s.ids.a).Scan(&leaked)
		if err == nil && leaked != 0 {
			t.Errorf("reader of the primary sees %d rows only the second help center or no help center shows", leaked)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// The app role still sees the whole knowledge base: unplaced and agents-only collections too.
	var articles, collections int
	err = db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM kb_articles), (SELECT count(*) FROM kb_collections)`).Scan(&articles, &collections)
	})
	if err != nil || articles != 3 || collections != 5 {
		t.Fatalf("app role sees %d articles and %d collections (%v), want 3 and 5", articles, collections, err)
	}
}

// A reader needs both a workspace and a help center: without one the scope is refused, and the role
// switched to by hand with only the workspace set sees no row. Inbox, contact, channel and the
// knowledge base's unpublished tables stay out of its reach.
func TestAHelpCenterReaderWithoutAHelpCenterSeesNothing(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	db := New(pool)
	for _, helpCenter := range []string{"", "not-a-uuid"} {
		if err := db.InHelpCenter(ctx, s.ids.a, helpCenter, func(pgx.Tx) error { return nil }); !errors.Is(err, ErrNoHelpCenter) {
			t.Errorf("help center %q: %v, want ErrNoHelpCenter", helpCenter, err)
		}
	}
	if err := db.InHelpCenter(ctx, "", s.primary, func(pgx.Tx) error { return nil }); !errors.Is(err, ErrNoWorkspace) {
		t.Errorf("no workspace: %v, want ErrNoWorkspace", err)
	}
	reader, err := db.ReaderRole(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := singleConnection(t, pool).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true),set_config('app.workspace_id',$2,true)`, reader, s.ids.a); err != nil {
		t.Fatal(err)
	}
	for _, table := range slices.Sorted(maps.Keys(helpCenterTables)) {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Errorf("%s without a help center = %d (%v), want 0", table, count, err)
		}
	}
	// kb_article_versions: the reader reads which version a text is at, never an older text.
	for _, read := range []string{"workspaces", "conversations", "messages", "contacts", "email_channels", "agents", "sessions",
		"kb_article_drafts", "kb_media_uploads", "help_center_domains", "kb_article_versions.body_md"} {
		table, column, _ := strings.Cut(read, ".")
		if column == "" {
			column = "1"
		}
		err := db.InHelpCenter(ctx, s.ids.a, s.primary, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT `+column+` FROM `+table+` LIMIT 1`)
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("reader reading %s: %v, want permission denied", read, err)
		}
	}
}
