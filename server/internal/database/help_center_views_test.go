package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestHelpCenterViewsKeepToTheWorkspaceWhoeverOwnsThem: the public-article views read their tables
// as their owner. Restored with --no-owner they belong to a superuser, which row-level security does
// not bind, so the views must still return only the scoped workspace's rows. Verify refuses that
// owner anyway; this is the second line.
func TestHelpCenterViewsKeepToTheWorkspaceWhoeverOwnsThem(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	other := otherDatabase(t, pool, fmt.Sprintf("colliedesk_test_views_%d", time.Now().UnixNano()))
	if err := Migrate(ctx, other); err != nil {
		t.Fatal(err)
	}
	ids := seedTwoWorkspaces(t, other)
	db := New(other)
	for workspace, name := range map[string]string{ids.a: "Alpha", ids.b: "Beta"} {
		if err := db.InWorkspace(ctx, workspace, func(tx pgx.Tx) error { seedKB(t, ctx, tx, workspace, name); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	app := pgx.Identifier{policyRole(t, other)}.Sanitize()
	for _, statement := range []string{
		`UPDATE kb_articles SET status='published', published_at=now()`,
		`UPDATE kb_article_translations SET status='published', published_at=now()`,
		`ALTER VIEW kb_help_center_collection_ranks OWNER TO CURRENT_USER`,
		`ALTER VIEW kb_help_center_public_articles OWNER TO CURRENT_USER`,
		`ALTER VIEW kb_help_center_public_translations OWNER TO CURRENT_USER`,
		`ALTER VIEW kb_help_center_chat_articles OWNER TO CURRENT_USER`,
		`ALTER VIEW kb_help_center_chat_translations OWNER TO CURRENT_USER`,
		`GRANT SELECT ON kb_help_center_collection_ranks, kb_help_center_public_articles, kb_help_center_public_translations,
			kb_help_center_chat_articles, kb_help_center_chat_translations TO ` + app,
	} {
		if _, err := other.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	for _, view := range helpCenterViews {
		var own, foreign int
		err := db.InWorkspace(ctx, ids.a, func(tx pgx.Tx) error {
			// The help chat views return rows only while help chat is on.
			if _, err := tx.Exec(ctx, `SELECT set_config('app.help_chat','on',true)`); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE workspace_id=$1), count(*) FILTER (WHERE workspace_id<>$1) FROM `+view,
				ids.a).Scan(&own, &foreign)
		})
		if err != nil {
			t.Fatalf("%s: %v", view, err)
		}
		if own == 0 || foreign != 0 {
			t.Errorf("%s owned by a superuser: %d own row(s), %d of another workspace; want some own and none foreign", view, own, foreign)
		}
	}
}
