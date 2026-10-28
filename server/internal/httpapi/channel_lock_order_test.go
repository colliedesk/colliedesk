package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Every change of a channel takes its type's lock before the channel's row, as making a channel the
// default and removing one do: a change of a mailbox's address made while another one becomes the
// default waits for it, instead of holding the row the other needs (a deadlock Postgres breaks by
// refusing one of them).
func TestAChannelChangeTakesTheTypeLockBeforeTheRow(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Lock Order", "admin@lock-order.example.com")
	connectEmail(t, api, admin, "support@lock-order.example.com")
	support := emailChannelOf(t, api, admin, "").Channel.ChannelID
	addEmailChannel(t, api, admin, "billing@lock-order.example.com")
	workspace := queryString(t, pool, `SELECT workspace_id::text FROM channels WHERE id=$1`, support)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// What making another mailbox the default does first: the type's lock.
	other, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback(ctx) }()
	if _, err := other.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text||':channels:email',0))`, workspace); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(t, api, http.MethodPatch, "/api/v1/channels/"+support, map[string]any{"config": map[string]any{"support_address": "help@lock-order.example.com"}}, admin, nil)
	}()
	// The address change now waits for the type's lock.
	for waiting := false; !waiting; {
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatal("the address change never waited for the type's lock")
		}
	}
	// Then the row: free, since the waiting change holds nothing yet.
	if _, err := other.Exec(ctx, `SET LOCAL lock_timeout='3s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Exec(ctx, `SELECT 1 FROM channels WHERE id=$1 FOR UPDATE`, support); err != nil {
		t.Fatalf("the default change could not take the mailbox's row: %v", err)
	}
	if err := other.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if response := <-done; response.Code != http.StatusOK {
		t.Fatalf("the address change: %d %s", response.Code, response.Body.String())
	}
}
