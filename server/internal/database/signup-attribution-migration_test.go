package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const signupAttributionMigration = "20261101101037-signup-attribution.up.sql"

func execPrivileged(t *testing.T, pool *DB, sql string, args ...any) error {
	t.Helper()
	return pool.Privileged(context.Background(), PrivilegeMigrate, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), sql, args...)
		return err
	})
}

func TestSignupAttributionUpgradeKeepsOnboardingAnswersAndStoresTheLink(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, signupAttributionMigration); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	tiktok, reddit := "tiktok", "reddit"
	if err := storeOnboardingSource(t, db, "before-tiktok", &tiktok); err != nil {
		t.Fatal(err)
	}
	if err := storeOnboardingSource(t, db, "before-reddit", &reddit); err != nil {
		t.Fatal(err)
	}
	if err := storeOnboardingSource(t, db, "before-unanswered", nil); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT string_agg(slug||'='||COALESCE(source,'NULL')||'/'||num_nonnulls(utm_source, utm_medium, utm_campaign, utm_content, utm_term, ttclid, fbclid, click_id_seen_at),
		',' ORDER BY slug) FROM workspace_onboarding JOIN workspaces ON workspaces.id=workspace_id`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "before-reddit=reddit/0,before-tiktok=tiktok/0,before-unanswered=NULL/0" {
		t.Fatalf("rows after upgrade=%q, want the three answers unchanged and no attribution", got)
	}

	var workspace string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE slug='before-tiktok'`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := execPrivileged(t, db, `UPDATE workspace_onboarding SET utm_source='tiktok', utm_content='post-07', fbclid='IwAR_x-1.y', click_id_seen_at=now() WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatalf("store attribution: %v", err)
	}
	refused := map[string]string{
		"a value with a space":            `UPDATE workspace_onboarding SET utm_campaign='spring sale' WHERE workspace_id=$1`,
		"markup":                          `UPDATE workspace_onboarding SET utm_term='<b>' WHERE workspace_id=$1`,
		"a click id without its time":     `UPDATE workspace_onboarding SET click_id_seen_at=NULL WHERE workspace_id=$1`,
		"a click time without a click id": `UPDATE workspace_onboarding SET fbclid=NULL WHERE workspace_id=$1`,
	}
	for name, sql := range refused {
		if err := execPrivileged(t, db, sql, workspace); err == nil {
			t.Fatalf("the database accepted %s", name)
		}
	}

	if err := rollbackThrough(ctx, pool, signupAttributionMigration); err == nil || !strings.Contains(err.Error(), "1 onboarding row") {
		t.Fatalf("down=%v, want a refusal naming the one row with attribution", err)
	}
	if err := pool.QueryRow(ctx, `SELECT utm_source||'/'||utm_content||'/'||fbclid FROM workspace_onboarding WHERE workspace_id=$1`, workspace).Scan(&got); err != nil || got != "tiktok/post-07/IwAR_x-1.y" {
		t.Fatalf("attribution after refused down=%q err=%v, want it untouched", got, err)
	}
}

func TestSignupAttributionDownDropsTheColumnsWhenUnused(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	source := "github"
	if err := storeOnboardingSource(t, New(pool), "unattributed", &source); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, signupAttributionMigration); err != nil {
		t.Fatal(err)
	}
	var columns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='workspace_onboarding'
		AND column_name IN ('utm_source','utm_medium','utm_campaign','utm_content','utm_term','ttclid','fbclid','click_id_seen_at')`).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("attribution columns after down=%d err=%v, want 0", columns, err)
	}
	if err := pool.QueryRow(ctx, `SELECT source FROM workspace_onboarding`).Scan(&source); err != nil || source != "github" {
		t.Fatalf("source after down=%q err=%v, want github", source, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
