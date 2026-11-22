package database

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAHelpCenterVisitorVotesOnlyOnItsOwnHelpCenter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	db := New(pool)
	vote := func(helpCenter string) error {
		return db.InHelpCenter(ctx, s.ids.a, helpCenter, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO kb_article_votes(workspace_id,article_id,voter,address,helpful)
				VALUES($1,$2,sha256('visitor'::bytea),sha256('address'::bytea),true)`, s.ids.a, s.secondOnly); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO kb_article_vote_versions(workspace_id,article_id,version) VALUES($1,$2,1)
				ON CONFLICT (workspace_id,article_id) DO UPDATE SET version=kb_article_vote_versions.version+1`, s.ids.a, s.secondOnly)
			return err
		})
	}
	if err := vote(s.second); err != nil {
		t.Fatalf("vote on the help center that shows the article: %v", err)
	}
	var pgErr *pgconn.PgError
	if err := vote(s.primary); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("vote through another help center = %v, want a row-level security refusal", err)
	}
}

func TestMovingACollectionMovesItsArticlesToTheOtherHelpCenter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	db := New(pool)
	if err := db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE kb_collections SET parent_id=$2 WHERE workspace_id=$1 AND id=$3`, s.ids.a, s.docs, s.shelf)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	shown := func(helpCenter string) []string {
		var ids []string
		if err := db.InHelpCenter(ctx, s.ids.a, helpCenter, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id::text FROM kb_articles WHERE id=$1`, s.secondOnly)
			if err != nil {
				return err
			}
			ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	if got := shown(s.primary); !slices.Equal(got, []string{s.secondOnly}) {
		t.Fatalf("primary help center after the shelf moved under docs: %v", got)
	}
	if got := shown(s.second); len(got) != 0 {
		t.Fatalf("second help center still shows the moved article: %v", got)
	}
}

// TestHelpCenterPublicViewsMatchTheWorkspaceViews: read as the app role, the views the reader uses list
// exactly the articles and translations kb_public_articles and kb_public_translations list.
func TestHelpCenterPublicViewsMatchTheWorkspaceViews(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	publishAll(t, pool, s.ids.a)
	err := New(pool).InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		for _, pair := range [][2]string{
			{`SELECT id::text FROM kb_public_articles`, `SELECT id::text FROM kb_help_center_public_articles`},
			{`SELECT article_id::text||locale FROM kb_public_translations`,
				`SELECT article_id::text||locale FROM kb_help_center_public_translations`},
		} {
			var sets [2][]string
			for i, query := range pair {
				rows, err := tx.Query(ctx, query+` ORDER BY 1`)
				if err != nil {
					return err
				}
				if sets[i], err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
					return err
				}
			}
			if len(sets[0]) == 0 || !slices.Equal(sets[0], sets[1]) {
				t.Errorf("%s = %v, %s = %v", pair[0], sets[0], pair[1], sets[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestAListingUnderAnAgentsOnlyParentOnAnotherHelpCenterKeepsTheArticlePrivate: an article at home on
// the primary help center is also listed in a collection on the second one whose parent is agents-only.
// The reader of the primary help center cannot see that parent, and still must not show the article.
func TestAListingUnderAnAgentsOnlyParentOnAnotherHelpCenterKeepsTheArticlePrivate(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	publishAll(t, pool, s.ids.a)
	db := New(pool)
	public := func() bool {
		var shown bool
		if err := db.InHelpCenter(ctx, s.ids.a, s.primary, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_help_center_public_articles WHERE id=$1)`, s.first).Scan(&shown)
		}); err != nil {
			t.Fatal(err)
		}
		return shown
	}
	if !public() {
		t.Fatal("the article is not public before its listing's parent became agents-only")
	}
	if err := db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE kb_collections SET visibility='agents' WHERE workspace_id=$1 AND id=$2`, s.ids.a, s.shelf)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if public() {
		t.Fatal("the primary help center shows an article listed under an agents-only collection of the second one")
	}
}

// TestAnAgentsOnlyCollectionSixLevelsUpKeepsTheArticleOffEveryHelpCenter: the views the reader uses
// walk every parent of a collection, as kb_public_articles does, not only the nearest ones.
func TestAnAgentsOnlyCollectionSixLevelsUpKeepsTheArticleOffEveryHelpCenter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	db := New(pool)
	var top, article string
	if err := db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO kb_collections(workspace_id,name,slug,visibility)
			VALUES($1,'Deep','deep','agents') RETURNING id::text`, s.ids.a).Scan(&top)
		parent := top
		for level := 2; err == nil && level <= 6; level++ {
			err = tx.QueryRow(ctx, `INSERT INTO kb_collections(workspace_id,parent_id,name,slug)
				VALUES($1,$2,'Level','deep-'||$3::int) RETURNING id::text`, s.ids.a, parent, level).Scan(&parent)
		}
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO kb_articles(workspace_id,collection_id,number,title,slug,body_md)
			VALUES($1,$2,40,'Deep','deep','text') RETURNING id::text`, s.ids.a, parent).Scan(&article)
	}); err != nil {
		t.Fatal(err)
	}
	publishAll(t, pool, s.ids.a)
	shown := func() (helpCenter, workspace bool) {
		if err := db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM kb_help_center_public_articles WHERE id=$1),
				EXISTS (SELECT 1 FROM kb_public_articles WHERE id=$1)`, article).Scan(&helpCenter, &workspace)
		}); err != nil {
			t.Fatal(err)
		}
		return helpCenter, workspace
	}
	if helpCenter, workspace := shown(); helpCenter || workspace {
		t.Fatalf("six levels under an agents-only collection: help center view %v, workspace view %v", helpCenter, workspace)
	}
	if err := db.InWorkspace(ctx, s.ids.a, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE kb_collections SET visibility='everyone' WHERE workspace_id=$1 AND id=$2`, s.ids.a, top)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if helpCenter, workspace := shown(); !helpCenter || !workspace {
		t.Fatalf("after the top collection opened: help center view %v, workspace view %v", helpCenter, workspace)
	}
}

func publishAll(t *testing.T, pool *pgxpool.Pool, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	if err := New(pool).InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE kb_articles SET status='published',published_at=now() WHERE workspace_id=$1`, workspaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE kb_article_translations SET status='published',published_at=now() WHERE workspace_id=$1`, workspaceID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
