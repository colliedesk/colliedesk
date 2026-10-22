package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const replyNumbersMigration = "20261022100554-reply-numbers.up.sql"

// An upgraded database numbers the replies it already holds in the order they were written (time,
// then id), per conversation; notes and customer mail get no number. A reply written afterwards, by
// any code, takes the next number.
func TestReplyNumbersMigrationNumbersEveryReplyInOrder(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrations(t) {
		if name < replyNumbersMigration {
			if err := applyMigration(ctx, pool, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	var workspace, contact, first, second, empty string
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES('numbers','Numbers') RETURNING id::text`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,'C','c@numbers.example') RETURNING id::text`, workspace).Scan(&contact); err != nil {
		t.Fatal(err)
	}
	for _, target := range []*string{&first, &second, &empty} {
		if err := pool.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject) VALUES($1,$2,'S') RETURNING id::text`, workspace, contact).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	// Written out of order on purpose: an import stores old replies after newer ones. Two replies share a
	// time; the id breaks the tie.
	for _, row := range []struct{ conversation, id, kind, at string }{
		{first, "00000000-0000-4000-8000-000000000003", "outgoing", "2026-10-01T10:00:00Z"},
		{first, "00000000-0000-4000-8000-000000000001", "outgoing", "2026-10-01T09:00:00Z"},
		{first, "00000000-0000-4000-8000-000000000002", "outgoing", "2026-10-01T10:00:00Z"},
		{first, "00000000-0000-4000-8000-000000000004", "incoming", "2026-10-01T08:00:00Z"},
		{first, "00000000-0000-4000-8000-000000000005", "note", "2026-10-01T09:30:00Z"},
		{second, "00000000-0000-4000-8000-000000000006", "outgoing", "2026-09-01T09:00:00Z"},
		{second, "00000000-0000-4000-8000-000000000007", "incoming", "2026-09-02T09:00:00Z"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,id,kind,body,created_at) VALUES($1,$2,$3,$4,'B',$5)`,
			workspace, row.conversation, row.id, row.kind, row.at); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	numbers := func() string {
		rows, err := pool.Query(ctx, `SELECT right(id::text,1),COALESCE(reply_number::text,'-') FROM messages ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := ""
		for rows.Next() {
			var id, number string
			if err := rows.Scan(&id, &number); err != nil {
				t.Fatal(err)
			}
			out += id + ":" + number + " "
		}
		return out
	}
	if got := numbers(); got != "1:1 2:2 3:3 4:- 5:- 6:1 7:- " {
		t.Fatalf("reply numbers after the upgrade: %s", got)
	}
	last := func() string {
		var a, b, c int
		latest := `SELECT coalesce(max(reply_number),0) FROM messages WHERE conversation_id=$1`
		if err := pool.QueryRow(ctx, `SELECT (`+latest+`),(`+strings.Replace(latest, "$1", "$2", 1)+`),(`+strings.Replace(latest, "$1", "$3", 1)+`)`, first, second, empty).Scan(&a, &b, &c); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(a, b, c)
	}
	if got := last(); got != "3 1 0" {
		t.Fatalf("latest reply numbers = %s, want 3 1 0", got)
	}

	// Any writer, an importer's plain INSERT included, gets the next number, two in one statement too;
	// a note or mail gets none.
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,id,kind,body) VALUES
		($1,$2,'00000000-0000-4000-8000-000000000008','outgoing','B'),($1,$2,'00000000-0000-4000-8000-000000000009','note','B'),
		($1,$3,'00000000-0000-4000-8000-00000000000a','outgoing','B'),($1,$2,'00000000-0000-4000-8000-00000000000b','outgoing','B')`, workspace, first, empty); err != nil {
		t.Fatal(err)
	}
	if got := numbers(); got != "1:1 2:2 3:3 4:- 5:- 6:1 7:- 8:4 9:- a:1 b:5 " {
		t.Fatalf("reply numbers after new rows: %s", got)
	}
	if got := last(); got != "5 1 1" {
		t.Fatalf("last reply numbers after new rows = %s", got)
	}
	// A reply without a number, a note with one, or two replies with one, is refused.
	if _, err := pool.Exec(ctx, `UPDATE messages SET reply_number=NULL WHERE right(id::text,1)='8'`); err == nil {
		t.Fatal("a reply lost its number")
	}
	if _, err := pool.Exec(ctx, `UPDATE messages SET reply_number=9 WHERE right(id::text,1)='9'`); err == nil {
		t.Fatal("a note got a number")
	}
	if _, err := pool.Exec(ctx, `UPDATE messages SET reply_number=4 WHERE right(id::text,1)='3'`); err == nil {
		t.Fatal("two replies share a number")
	}
}
