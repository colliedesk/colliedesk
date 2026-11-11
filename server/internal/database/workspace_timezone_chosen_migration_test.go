package database

import (
	"context"
	"testing"
)

const workspaceTimezoneChosenMigration = "20261110104232-workspace-timezone-chosen.up.sql"

// The creator's first sign-in starts only a workspace whose timezone nobody chose. An upgraded workspace
// counts as chosen, whatever its zone (UTC included), so the upgrade never changes its clock; a workspace
// created after the upgrade starts unchosen.
func TestWorkspaceTimezoneChosenMigrationCountsEveryExistingWorkspaceAsChosen(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < workspaceTimezoneChosenMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(slug,name,timezone) VALUES('old-utc','Old UTC','UTC'),('old-tokyo','Old Tokyo','Asia/Tokyo')`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name >= workspaceTimezoneChosenMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatalf("upgrade: %v", err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO workspaces(slug,name) VALUES('new-desk','New Desk')`); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		zone   string
		chosen bool
	}{
		"old-utc":   {"UTC", true},
		"old-tokyo": {"Asia/Tokyo", true},
		"new-desk":  {"UTC", false},
	}
	for slug, expected := range want {
		var zone string
		var chosen bool
		if err := pool.QueryRow(ctx, `SELECT timezone, timezone_chosen_at IS NOT NULL FROM workspaces WHERE slug=$1`, slug).Scan(&zone, &chosen); err != nil {
			t.Fatalf("read %s: %v", slug, err)
		}
		if zone != expected.zone || chosen != expected.chosen {
			t.Errorf("%s: timezone=%q chosen=%v, want %q chosen=%v", slug, zone, chosen, expected.zone, expected.chosen)
		}
	}
}
