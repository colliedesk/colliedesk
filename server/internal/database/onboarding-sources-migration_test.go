package database

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const onboardingSourcesMigration = "20261027104933-expand-onboarding-sources.up.sql"

func storeOnboardingSource(t *testing.T, pool *DB, slug string, source *string) error {
	t.Helper()
	return pool.Privileged(context.Background(), PrivilegeMigrate, func(tx pgx.Tx) error {
		var workspace string
		if err := tx.QueryRow(context.Background(), `INSERT INTO workspaces(slug,name) VALUES($1,$1) RETURNING id::text`, slug).Scan(&workspace); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `INSERT INTO workspace_onboarding(workspace_id,source) VALUES($1,$2)`, workspace, source)
		return err
	})
}

func TestOnboardingSourcesUpgradePreservesOldAnswersAndAcceptsNewSources(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, onboardingSourcesMigration); err != nil {
		t.Fatal(err)
	}
	db := New(pool)
	old := []string{"x", "email", "search", "reddit", "github", "referral", "other"}
	for _, source := range old {
		if err := storeOnboardingSource(t, db, "old-"+source, &source); err != nil {
			t.Fatal(err)
		}
	}
	if err := storeOnboardingSource(t, db, "unanswered", nil); err != nil {
		t.Fatal(err)
	}
	claude := "ai_claude"
	if err := storeOnboardingSource(t, db, "before-up", &claude); err == nil {
		t.Fatal("old constraint accepted ai_claude")
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT string_agg(COALESCE(source,'NULL'),',' ORDER BY COALESCE(source,'NULL') COLLATE "C") FROM workspace_onboarding`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "NULL,email,github,other,reddit,referral,search,x" {
		t.Fatalf("old answers=%q, want all seven original answers and NULL unchanged", got)
	}
	for _, source := range []string{"ai_chatgpt", "ai_claude", "ai_gemini", "ai_perplexity", "ai_other", "tiktok", "youtube"} {
		if err := storeOnboardingSource(t, db, "new-"+strings.ReplaceAll(source, "_", "-"), &source); err != nil {
			t.Fatalf("new source %s: %v", source, err)
		}
	}
	unknown := "ai_unknown"
	if err := storeOnboardingSource(t, db, "unknown", &unknown); err == nil {
		t.Fatal("constraint accepted ai_unknown")
	}
	if err := rollbackThrough(ctx, pool, onboardingSourcesMigration); err == nil || !strings.Contains(err.Error(), "7 onboarding") {
		t.Fatalf("down=%v, want refusal naming the seven new answers", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workspace_onboarding`).Scan(&count); err != nil || count != 15 {
		t.Fatalf("rows after refused down=%d err=%v, want 15 untouched", count, err)
	}
}

func TestOnboardingSourcesDownRestoresOldConstraintWhenUnused(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	source := "reddit"
	if err := storeOnboardingSource(t, New(pool), "legacy", &source); err != nil {
		t.Fatal(err)
	}
	if err := rollbackThrough(ctx, pool, onboardingSourcesMigration); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"ai_chatgpt", "ai_claude", "ai_gemini", "ai_perplexity", "ai_other", "tiktok", "youtube"} {
		if err := storeOnboardingSource(t, New(pool), "refused-"+strings.ReplaceAll(source, "_", "-"), &source); err == nil {
			t.Fatalf("down accepted %s", source)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
