package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestVerifyRefusesAReaderThatSkipsHelpCenterIsolation runs in a database of its own, because it
// breaks the reader role on purpose and every package test shares the test database's reader.
func TestVerifyRefusesAReaderThatSkipsHelpCenterIsolation(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	for name, breakIt := range map[string]string{
		// A reader that bypasses row-level security reads every help center of every workspace.
		"bypassrls": `ALTER ROLE %[1]s BYPASSRLS`,
		// An app role that inherits the reader gets its RESTRICTIVE policies: staff would lose every
		// unplaced article, so the server refuses to start instead.
		"inherited": `GRANT %[1]s TO %[2]s WITH INHERIT TRUE`,
	} {
		t.Run(name, func(t *testing.T) {
			other := otherDatabase(t, pool, fmt.Sprintf("colliedesk_test_reader_%d", time.Now().UnixNano()))
			if err := Migrate(ctx, other); err != nil {
				t.Fatal(err)
			}
			db := New(other)
			if err := db.Verify(ctx); err != nil {
				t.Fatalf("Verify before: %v", err)
			}
			reader, err := db.ReaderRole(ctx)
			if err != nil {
				t.Fatal(err)
			}
			app := pgx.Identifier{policyRole(t, other)}.Sanitize()
			if _, err := other.Exec(ctx, fmt.Sprintf(breakIt, pgx.Identifier{reader}.Sanitize(), app)); err != nil {
				t.Fatal(err)
			}
			if err := New(other).Verify(ctx); err == nil || !strings.Contains(err.Error(), reader) {
				t.Fatalf("Verify = %v, want a refusal naming %s", err, reader)
			}
		})
	}
}

// TestRequestLoginCannotLeaveTheHelpCenterScope: through a separate request login, a help center
// transaction reads its own help center, and SQL that drops the reader role is left with no rights.
func TestRequestLoginCannotLeaveTheHelpCenterScope(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	s := seedHelpCenters(t, pool)
	split := NewSplit(pool, requestLogin(t, pool))
	if err := split.Verify(ctx); err != nil {
		t.Fatalf("Verify with a request login: %v", err)
	}
	articles := func(escape string) (int, error) {
		var count int
		err := split.InHelpCenter(ctx, s.ids.a, s.second, func(tx pgx.Tx) error {
			if escape != "" {
				if _, err := tx.Exec(ctx, escape); err != nil {
					return err
				}
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM kb_articles`).Scan(&count)
		})
		return count, err
	}
	if count, err := articles(""); err != nil || count != 2 {
		t.Fatalf("second help center through the request login: %d articles (%v), want 2", count, err)
	}
	for _, escape := range []string{`RESET ROLE`, `SET LOCAL ROLE NONE`} {
		count, err := articles(escape)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "42501" && pgErr.Code != "42P01") {
			t.Errorf("%s: request login saw %d article(s) (err=%v), want no access", escape, count, err)
		}
	}
}
