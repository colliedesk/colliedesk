package freshdesk_test

import (
	"context"
	"testing"
	"time"
)

type storedStats struct {
	present                          bool
	status                           string
	resolved, firstResponded, closed *time.Time
}

func (e *importEnv) ticketStats(t *testing.T, freshdeskID string) storedStats {
	t.Helper()
	var stats storedStats
	if err := e.pool.QueryRow(context.Background(), `SELECT origin ? 'stats',COALESCE(origin->'stats'->>'status',''),(origin->'stats'->>'resolved_at')::timestamptz,
		(origin->'stats'->>'first_responded_at')::timestamptz,(origin->'stats'->>'closed_at')::timestamptz
		FROM conversations WHERE id=$1`, e.target(t, "ticket", freshdeskID)).Scan(&stats.present, &stats.status, &stats.resolved, &stats.firstResponded, &stats.closed); err != nil {
		t.Fatal(err)
	}
	return stats
}

func at(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}

// The list asks for ticket stats; the first values read stay (a later run must not move the baseline the
// results page measured at), a run fills a ticket imported before stats were read without touching its
// updated_at, and an archived ticket, which Freshdesk serves without stats, has none.
func TestTicketStatsAreReadOnceAndOnlyFilledLater(t *testing.T) {
	env := newImportEnv(t)
	env.fake.stats = map[string]map[string]any{
		"2": {"resolved_at": "2025-02-02T09:00:00Z", "first_responded_at": "2025-02-01T11:00:00Z", "closed_at": nil},
		"6": {"resolved_at": "2025-04-01T06:00:00Z", "first_responded_at": nil, "closed_at": "2025-04-01T06:30:00Z"},
	}
	env.mustRun(t)
	two := env.ticketStats(t, "2")
	if !two.present || two.status != "resolved" || two.resolved == nil || !two.resolved.Equal(at("2025-02-02T09:00:00Z")) || !two.firstResponded.Equal(at("2025-02-01T11:00:00Z")) || two.closed != nil {
		t.Fatalf("ticket 2 stats = %+v", two)
	}
	if one := env.ticketStats(t, "1"); !one.present || one.status != "open" || one.resolved != nil || one.firstResponded != nil || one.closed != nil {
		t.Fatalf("ticket 1 stats = %+v, want read with every value null", one)
	}
	if archived := env.ticketStats(t, "3"); archived.present {
		t.Fatalf("archived ticket 3 has stats %+v", archived)
	}

	// Ticket 6 changes in Freshdesk and is imported again with newer stats: the first read stays.
	// Ticket 2 lost its stats (as an import that predates them) and is unchanged: they are filled.
	ctx := context.Background()
	conversation := env.target(t, "ticket", "2")
	var updatedBefore time.Time
	if err := env.pool.QueryRow(ctx, `UPDATE conversations SET origin=origin-'stats' WHERE id=$1 RETURNING updated_at`, conversation).Scan(&updatedBefore); err != nil {
		t.Fatal(err)
	}
	env.fake.mu.Lock()
	env.fake.stats["6"] = map[string]any{"resolved_at": "2025-04-05T10:00:00Z", "first_responded_at": nil, "closed_at": nil}
	env.fake.stats["2"] = map[string]any{"resolved_at": "2025-02-02T09:30:00Z", "first_responded_at": nil, "closed_at": nil}
	env.fake.mu.Unlock()
	env.fake.extraReplyOnTicket6 = true
	env.mustRun(t)
	if six := env.ticketStats(t, "6"); six.resolved == nil || !six.resolved.Equal(at("2025-04-01T06:00:00Z")) || six.closed == nil {
		t.Fatalf("ticket 6 stats = %+v, want the first values read", six)
	}
	two = env.ticketStats(t, "2")
	if two.resolved == nil || !two.resolved.Equal(at("2025-02-02T09:30:00Z")) {
		t.Fatalf("ticket 2 stats = %+v, want filled by the re-run", two)
	}
	var updatedAfter time.Time
	if err := env.pool.QueryRow(ctx, `SELECT updated_at FROM conversations WHERE id=$1`, conversation).Scan(&updatedAfter); err != nil {
		t.Fatal(err)
	}
	if !updatedAfter.Equal(updatedBefore) {
		t.Fatalf("filling stats moved updated_at %s -> %s", updatedBefore, updatedAfter)
	}
}
