package store_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// voteNo writes a "No" vote the way the help center does, inside the workspace's own transaction.
func voteNo(t *testing.T, pool *pgxpool.Pool, workspace, article string, voter byte, missing string) {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.workspace_id', $1, true)`, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_votes(workspace_id, article_id, voter, address, helpful, missing)
		VALUES ($1, $2, $3, $4, false, $5)`, workspace, article, bytes.Repeat([]byte{voter}, 32), bytes.Repeat([]byte{9}, 32), missing); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// setGapStatus marks every gap of the workspace, as a person did daysAgo days ago.
func setGapStatus(t *testing.T, pool *pgxpool.Pool, workspace, status string, daysAgo int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE kb_gaps SET status=$2, status_changed_at=now() - make_interval(days => $3)
		WHERE workspace_id=$1`, workspace, status, daysAgo); err != nil {
		t.Fatal(err)
	}
}

func publishArticle(t *testing.T, s *store.Store, workspace, slug, title string) string {
	t.Helper()
	ctx := context.Background()
	collection, err := s.CreateKBCollection(ctx, workspace, store.Actor{}, store.KBCollectionInput{Name: "Help " + slug, Slug: "help-" + slug})
	if err != nil {
		t.Fatal(err)
	}
	article, err := s.CreateKBArticle(ctx, workspace, store.Actor{}, store.KBArticleInput{
		CollectionID: collection.ID, Title: title, Slug: slug, BodyMD: "Open Billing.", Status: "published",
	})
	if err != nil {
		t.Fatal(err)
	}
	return article.ID
}

// One reader who says "No" on two articles about the same thing is one asker of that gap.
func TestTheSameVoterTwiceIsOneAsker(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-voter")
	first, second := searchArticle(t, s, workspace, "first"), searchArticle(t, s, workspace, "second")
	voteNo(t, pool, workspace, first, 1, "refund policy")
	voteNo(t, pool, workspace, second, 1, "Refund policy")
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, workspace), gapRow{title: "Refund policy", status: "open", askers: 1, signals: 2})
}

// A fixed gap opens again when people still ask more than 7 days after the fix; within 7 days the fix
// stands and the gap only counts them.
func TestAFixedGapReopensWhenAskedMoreThanSevenDaysAfterTheFix(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	old, recent := testWorkspace(t, pool, "kb-gaps-reopen"), testWorkspace(t, pool, "kb-gaps-recent-fix")
	for _, workspace := range []string{old, recent} {
		searchFindingNothing(t, s, workspace, "refund policy")
	}
	groupGaps(t, s)
	setGapStatus(t, pool, old, "fixed", 8)
	setGapStatus(t, pool, recent, "fixed", 6)
	for _, workspace := range []string{old, recent} {
		searchFindingNothing(t, s, workspace, "refund policy")
	}
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, old), gapRow{title: "refund policy", status: "open", askers: 2, signals: 2})
	wantGaps(t, storedGaps(t, pool, recent), gapRow{title: "refund policy", status: "fixed", askers: 2, signals: 2})
}

// A dismissed gap stays dismissed however long after people ask, and still counts them.
func TestADismissedGapStaysDismissedAndKeepsCounting(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-dismissed")
	searchFindingNothing(t, s, workspace, "refund policy")
	groupGaps(t, s)
	setGapStatus(t, pool, workspace, "dismissed", 30)
	searchFindingNothing(t, s, workspace, "refund policy")
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, workspace), gapRow{title: "refund policy", status: "dismissed", askers: 2, signals: 2})
}

// Publishing an article whose title answers an open gap marks that gap fixed by it; a gap it does not
// answer stays open.
func TestPublishingAMatchingArticleMarksTheGapFixed(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-autofix")
	searchFindingNothing(t, s, workspace, "how to export invoices", "how to change my password")
	groupGaps(t, s)
	article := publishArticle(t, s, workspace, "export-invoices", "Export invoices")
	groupGaps(t, s)

	wantGaps(t, storedGaps(t, pool, workspace),
		gapRow{title: "how to export invoices", status: "fixed", askers: 1, signals: 1, fixedBy: article, automatic: true},
		gapRow{title: "how to change my password", status: "open", askers: 1, signals: 1})
}

// An email address or a run of 6 or more digits a reader typed never reaches a signal or a gap.
func TestSignalTextIsStoredMasked(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-mask")
	if _, err := pool.Exec(context.Background(), `WITH c AS (INSERT INTO channels(workspace_id,type,name) VALUES ($1,'help_chat','Chat') RETURNING id),
			s AS (INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface)
				SELECT $1,c.id,sha256(gen_random_uuid()::text::bytea),'help_center' FROM c RETURNING id)
		INSERT INTO ai_chat_turns(workspace_id,session_id,seq,question,status) SELECT $1,s.id,1,$2,'no_source' FROM s`,
		workspace, "My email is jane.doe@example.com and order 1234567 is late"); err != nil {
		t.Fatal(err)
	}
	groupGaps(t, s)

	masked := "My email is [email removed] and order [number removed] is late"
	wantGaps(t, storedGaps(t, pool, workspace), gapRow{title: masked, status: "open", askers: 1, signals: 1})
	var text, kind string
	if err := pool.QueryRow(context.Background(), `SELECT text, kind FROM kb_gap_signals WHERE workspace_id=$1`, workspace).Scan(&text, &kind); err != nil {
		t.Fatal(err)
	}
	if text != masked || kind != "chat_unanswered" {
		t.Fatalf("signal = %q %q, want %q chat_unanswered", text, kind, masked)
	}
}

// A workspace with gaps, signals, a chat and queued signals is deleted at once.
func TestDeletingAWorkspaceWithGapSignalsSucceeds(t *testing.T) {
	pool := databasetest.Pool(t)
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-gaps-delete")
	searchFindingNothing(t, s, workspace, "refund policy")
	if _, err := pool.Exec(context.Background(), `WITH c AS (INSERT INTO channels(workspace_id,type,name) VALUES ($1,'help_chat','Chat') RETURNING id),
			s AS (INSERT INTO ai_chat_sessions(workspace_id,channel_id,token_hash,surface)
				SELECT $1,c.id,sha256(gen_random_uuid()::text::bytea),'help_center' FROM c RETURNING id)
		INSERT INTO ai_chat_turns(workspace_id,session_id,seq,question,status) SELECT $1,s.id,1,'Where is my parcel?','no_source' FROM s`, workspace); err != nil {
		t.Fatal(err)
	}
	groupGaps(t, s)
	searchFindingNothing(t, s, workspace, "still queued")
	if _, err := pool.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, workspace); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	var left int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM kb_gaps WHERE workspace_id=$1)
		+ (SELECT count(*) FROM kb_gap_signals WHERE workspace_id=$1) + (SELECT count(*) FROM kb_gap_pending WHERE workspace_id=$1)`, workspace).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("rows left of the deleted workspace = %d, want 0", left)
	}
}
