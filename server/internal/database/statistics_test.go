package database

import (
	"context"
	"testing"
)

func TestRefreshStatisticsGivesThePlannerTheRowsJustWritten(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE bulk_written(id int); INSERT INTO bulk_written SELECT generate_series(1, 1000)`); err != nil {
		t.Fatal(err)
	}
	estimate := func() float64 {
		var rows float64
		if err := pool.QueryRow(ctx, `SELECT reltuples FROM pg_class WHERE oid='bulk_written'::regclass`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := estimate(); rows == 1000 {
		t.Fatalf("statistics already current before the refresh (%v rows); the test proves nothing", rows)
	}
	if err := New(pool).RefreshStatistics(ctx, "bulk_written"); err != nil {
		t.Fatal(err)
	}
	if rows := estimate(); rows != 1000 {
		t.Fatalf("planner estimate after refresh = %v rows, want 1000", rows)
	}
}
