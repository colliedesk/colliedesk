package database

import (
	"context"
	"strings"
	"testing"
)

const signupLandingPathMigration = "20261104034601-signup-landing-path.up.sql"

func TestSignupLandingPathUpgradeKeepsOnboardingAttributionAndStoresThePage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, signupLandingPathMigration); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	tiktok, search := "tiktok", "search"
	if err := storeOnboardingSource(t, db, "before-ad", &tiktok); err != nil {
		t.Fatal(err)
	}
	if err := storeOnboardingSource(t, db, "before-search", &search); err != nil {
		t.Fatal(err)
	}
	if err := execPrivileged(t, db, `UPDATE workspace_onboarding SET utm_source='tiktok', utm_content='post-07', fbclid='IwAR_x-1.y', click_id_seen_at='2026-11-01 09:30:00+00'
		WHERE workspace_id=(SELECT id FROM workspaces WHERE slug='before-ad')`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT string_agg(slug||'='||concat_ws('/', source, COALESCE(utm_source,'-'), COALESCE(utm_content,'-'), COALESCE(fbclid,'-'),
		COALESCE(to_char(click_id_seen_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI'),'-'), COALESCE(landing_path,'no-page')), ',' ORDER BY slug)
		FROM workspace_onboarding JOIN workspaces ON workspaces.id=workspace_id`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := "before-ad=tiktok/tiktok/post-07/IwAR_x-1.y/2026-11-01T09:30/no-page,before-search=search/-/-/-/-/no-page"; got != want {
		t.Fatalf("rows after upgrade=%q, want %q", got, want)
	}

	var workspace string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE slug='before-search'`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/vs/zendesk", "/" + strings.Repeat("a", 200)} {
		if err := execPrivileged(t, db, `UPDATE workspace_onboarding SET landing_path=$2 WHERE workspace_id=$1`, workspace, path); err != nil {
			t.Fatalf("store landing path %q: %v", path, err)
		}
	}
	for name, path := range map[string]string{
		"a full address":                 "https://evil.com",
		"markup":                         "/vs/<script>",
		"a path without a leading slash": "vs/zendesk",
		"capital letters":                "/VS",
		"a path of 202 characters":       "/" + strings.Repeat("a", 201),
		"an empty path":                  "",
	} {
		if err := execPrivileged(t, db, `UPDATE workspace_onboarding SET landing_path=$2 WHERE workspace_id=$1`, workspace, path); err == nil {
			t.Fatalf("the database accepted %s as the landing path", name)
		}
	}
	if err := execPrivileged(t, db, `UPDATE workspace_onboarding SET landing_path='/vs/zendesk' WHERE workspace_id=$1`, workspace); err != nil {
		t.Fatal(err)
	}

	if err := rollbackThrough(ctx, pool, signupLandingPathMigration); err == nil || !strings.Contains(err.Error(), "1 onboarding row") {
		t.Fatalf("down=%v, want a refusal naming the one row with a landing page", err)
	}
	if err := pool.QueryRow(ctx, `SELECT landing_path FROM workspace_onboarding WHERE workspace_id=$1`, workspace).Scan(&got); err != nil || got != "/vs/zendesk" {
		t.Fatalf("landing path after refused down=%q err=%v, want /vs/zendesk", got, err)
	}
}

func TestSignupLandingPathDownDropsTheColumnWhenUnused(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	source := "github"
	db := New(pool)
	if err := storeOnboardingSource(t, db, "no-page", &source); err != nil {
		t.Fatal(err)
	}
	if err := execPrivileged(t, db, `UPDATE workspace_onboarding SET utm_source='reddit'`); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, signupLandingPathMigration); err != nil {
		t.Fatal(err)
	}
	var columns int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='workspace_onboarding'
		AND column_name='landing_path'`).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("landing_path columns after down=%d err=%v, want 0", columns, err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT source||'/'||utm_source FROM workspace_onboarding`).Scan(&got); err != nil || got != "github/reddit" {
		t.Fatalf("answers after down=%q err=%v, want github/reddit", got, err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
