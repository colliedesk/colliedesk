package database

import (
	"context"
	"strings"
	"testing"
)

// A message's journal row is found by the message through an index: a quote in a shared scope and the
// export's reason for each message look it up, and the journal only grows.
func TestTheJournalRowOfAMessageIsFoundByIndex(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	var plan string
	if err := tx.QueryRow(ctx, `EXPLAIN SELECT sender_key FROM channel_inbound_events
		WHERE workspace_id='00000000-0000-4000-8000-000000000001' AND message_id='00000000-0000-4000-8000-000000000002'`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "channel_inbound_events_message") {
		t.Fatalf("plan: %s", plan)
	}
}
