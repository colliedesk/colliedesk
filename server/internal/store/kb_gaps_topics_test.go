package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// resolvedConversation adds a resolved conversation of contact with what AI retrieval keeps of it.
func resolvedConversation(t *testing.T, pool *pgxpool.Pool, workspace, contact, subject string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `WITH known AS (SELECT id FROM contacts WHERE workspace_id=$1 AND name=$2),
		added AS (INSERT INTO contacts(workspace_id, name, email)
			SELECT $1, $2, $2 || '@example.com' WHERE NOT EXISTS (SELECT 1 FROM known) RETURNING id),
		c AS (INSERT INTO conversations(workspace_id, contact_id, subject, status)
			SELECT $1, (SELECT id FROM known UNION ALL SELECT id FROM added LIMIT 1), $3, 'resolved' RETURNING id)
		INSERT INTO ai_retrieval_docs(workspace_id, conversation_id, title, body, folded, tokens)
			SELECT $1, c.id, $3, 'Customer: ' || $3, to_tsvector('simple', $3), 10 FROM c`, workspace, contact, subject); err != nil {
		t.Fatal(err)
	}
}

func findTopics(t *testing.T, s *store.Store) {
	t.Helper()
	if _, err := s.BuildKBGapTopics(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Resolved conversations no published article answers become recurring topics, once a workspace
// has resolved at least 20 in the last 30 days; a conversation an article answers is no signal.
func TestRecurringTopicsNeedTwentyResolvedConversations(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-topics")
	publishArticle(t, s, workspace, "export-invoices", "Export invoices")
	groupGaps(t, s)
	contacts := []string{"ana", "ben", "cai", "dee", "eli", "fay", "gus", "hal", "ivy"}
	for index := range 18 {
		resolvedConversation(t, pool, workspace, contacts[index%len(contacts)], "Where is my parcel")
	}
	resolvedConversation(t, pool, workspace, "jon", "How do I export invoices")
	findTopics(t, s)
	wantGaps(t, storedGaps(t, pool, workspace))

	resolvedConversation(t, pool, workspace, "kim", "Where is my parcel")
	findTopics(t, s)
	findTopics(t, s)
	gap := gapRow{title: "Where is my parcel", status: "open", askers: 10, signals: 19}
	wantGaps(t, storedGaps(t, pool, workspace), gap)
	var kinds int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM kb_gap_signals WHERE workspace_id=$1 AND kind <> 'recurring_topic'`, workspace).Scan(&kinds); err != nil || kinds != 0 {
		t.Fatalf("signals of another kind = %d, %v; want 0", kinds, err)
	}
}
