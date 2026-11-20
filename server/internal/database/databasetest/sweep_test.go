package databasetest

import (
	"context"
	"fmt"
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
	// This test's clock runs a year ahead of the real one: to a sweep on the real clock, every one of
	// its databases was created in the future, so it is never old enough to drop.
	now := time.Now().AddDate(1, 0, 0).Truncate(time.Second)
	named := func(n int, created time.Time, suffix string) string {
		return fmt.Sprintf("ctest_4242_%d_%d_%s", n, created.Unix(), suffix)
	}
	leaked := named(1, now.Add(-8*time.Hour-40*time.Minute), "aaaaaa") // older than a run can last
	inUse := named(2, now.Add(-8*time.Hour-40*time.Minute), "bbbbbb")  // as old, but a connection is still open
	running := named(3, now.Add(-2*time.Hour), "cccccc")               // a run still going
	oldFormat := "ctest_4242_4_31982_dddddd"                           // seconds mod 100000: its age is unknown, so it stays
	for _, name := range []string{leaked, inUse, running, oldFormat} {
		if _, err := pool.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") }) //nolint:errcheck
	}
	// The other test packages run at the same time on the same server, and each one sweeps on the real
	// clock. Their sweep must leave this test's databases alone, or inUse is gone before it is opened.
	sweepLeakedClones(ctx, pool, time.Now())
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
