package databasetest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A test run that is killed (timeout, Ctrl-C) never reaches t.Cleanup, so its clone stays: 45 of them
// filled 9.8 GB on one dev server. The next run drops the ones older than a run can last, and leaves
// the clones of runs still going.
func TestSweepDropsCloneLeftByAKilledRunButNotOnesStillInUse(t *testing.T) {
	ctx := context.Background()
	pool, err := adminPool(ctx, BaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)
	leaked := "ctest_4242_1_1794298800_aaaaaa"  // 08:20 UTC the same day: 8h40m old
	inUse := "ctest_4242_2_1794298800_bbbbbb"   // as old, but a connection is still open
	running := "ctest_4242_3_1794322800_cccccc" // 15:00 UTC: two hours old
	oldFormat := "ctest_4242_4_31982_dddddd"    // seconds mod 100000: its age is unknown, so it stays
	for _, name := range []string{leaked, inUse, running, oldFormat} {
		if _, err := pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") }) //nolint:errcheck
	}
	inUseURL, err := withDatabase(BaseURL(t), inUse)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgxpool.New(ctx, inUseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	sweepLeakedClones(ctx, pool, now)

	for name, want := range map[string]bool{leaked: false, oldFormat: true, inUse: true, running: true} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT count(*) > 0 FROM pg_database WHERE datname=$1`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Errorf("%s exists = %v, want %v", name, exists, want)
		}
	}
}

func TestCloneNameCarriesTheFullCreationTime(t *testing.T) {
	before := time.Now().Unix()
	created, ok := cloneCreated(uniqueName("ctest_"))
	if !ok || created.Unix() < before || created.Unix() > time.Now().Unix() {
		t.Fatalf("cloneCreated(uniqueName) = %v, %v; want a time from this run", created, ok)
	}
}
