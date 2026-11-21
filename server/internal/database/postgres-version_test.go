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
	for _, want := range []string{"PostgreSQL 99 or newer", "PostgreSQL " + shown, "sh upgrade-postgres.sh", "docs/self-host/upgrade.md#postgresql-versions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestCheckPostgresVersionAcceptsEverySupportedServer(t *testing.T) {
	if err := New(testPool(t)).CheckPostgresVersion(context.Background(), MinPostgresMajor); err != nil {
		t.Fatalf("the test server is supported, yet: %v", err)
	}
	if MinPostgresMajor != 16 {
		t.Fatalf("MinPostgresMajor = %d; external databases on 16 stay supported", MinPostgresMajor)
	}
}
