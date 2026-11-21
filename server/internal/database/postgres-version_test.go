package database

import (
	"context"
	"strings"
	"testing"
)

func TestCheckPostgresVersionRefusesAServerBelowTheFloorAndNamesTheUpgrade(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	var shown string
	if err := pool.QueryRow(ctx, `SHOW server_version`).Scan(&shown); err != nil {
		t.Fatal(err)
	}
	err := New(pool).CheckPostgresVersion(ctx, 99)
	if err == nil {
		t.Fatalf("PostgreSQL %s passed a floor of 99", shown)
	}
	// SHOW server_version may carry the packager's suffix: "17.11 (Debian 17.11-1.pgdg12+2)".
	release := strings.Fields(shown)[0]
	for _, want := range []string{"PostgreSQL 99 or newer", "this database is PostgreSQL " + release + ".", "sh upgrade-postgres.sh moves a bundled database on PostgreSQL 16, 17 or 18 to 19", "provider", "docs/self-host/upgrade.md#postgresql-versions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestCheckPostgresVersionAcceptsEverySupportedServer(t *testing.T) {
	if err := New(testPool(t)).CheckPostgresVersion(context.Background(), MinPostgresMajor); err != nil {
		t.Fatalf("the test server is supported, yet: %v", err)
	}
	if MinPostgresMajor != 19 {
		t.Fatalf("MinPostgresMajor = %d; Colliedesk runs on PostgreSQL 19 or newer", MinPostgresMajor)
	}
}
