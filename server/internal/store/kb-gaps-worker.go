package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

const (
	// kbGapBatch is how many queued signals one transaction reads.
	kbGapBatch = 200
	// kbGapBatchesPerWorkspace bounds one workspace's share of a call; the rest waits for the next.
	kbGapBatchesPerWorkspace = 10
	// kbGapTopicsEvery is how often recurring topics are looked for.
	kbGapTopicsEvery = 24 * time.Hour
)

// BuildKBGaps files the queued knowledge gap signals of every workspace under their gaps, marks the
// gaps a newly published article answers fixed, and returns how many queued rows it finished. A
// workspace another process is grouping is left to it.
func (s *Store) BuildKBGaps(ctx context.Context) (int, error) {
	var workspaces []string
	// Only the ids of workspaces with queued rows cross workspaces; each is grouped in its own scope.
	err := s.db.Privileged(ctx, database.PrivilegeKBGapQueue, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH RECURSIVE w AS (
				(SELECT workspace_id FROM kb_gap_pending ORDER BY workspace_id LIMIT 1)
				UNION ALL
				SELECT (SELECT p.workspace_id FROM kb_gap_pending p WHERE p.workspace_id > w.workspace_id ORDER BY p.workspace_id LIMIT 1) FROM w WHERE w.workspace_id IS NOT NULL)
			SELECT workspace_id::text FROM w WHERE workspace_id IS NOT NULL`)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("list workspaces with knowledge gap signals: %w", err)
	}
	done := 0
	var failed []error
	for _, workspaceID := range workspaces {
		for range kbGapBatchesPerWorkspace {
			n, more, err := s.buildKBGapBatch(ctx, workspaceID)
			done += n
			if err != nil {
				// One workspace that fails (its rows stay queued) does not hold up the others.
				failed = append(failed, fmt.Errorf("group knowledge gaps of workspace %s: %w", workspaceID, err))
				break
			}
			if !more || ctx.Err() != nil {
				break
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return done, errors.Join(append(failed, ctx.Err())...)
}

type kbGapPendingRow struct {
	id             int64
	kind, sourceID string
}

// buildKBGapBatch handles the oldest queued rows of one workspace in queue order and deletes those it
// finished; more says a full batch was read and some of it finished.
func (s *Store) buildKBGapBatch(ctx context.Context, workspaceID string) (done int, more bool, err error) {
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if locked, err := lockKBGaps(ctx, tx, workspaceID); err != nil || !locked {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, kind, source_id FROM kb_gap_pending WHERE workspace_id=$1 ORDER BY id LIMIT $2`,
			workspaceID, kbGapBatch)
		if err != nil {
			return err
		}
		pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (kbGapPendingRow, error) {
			var p kbGapPendingRow
			return p, row.Scan(&p.id, &p.kind, &p.sourceID)
		})
		if err != nil || len(pending) == 0 {
			return err
		}
		// A workspace being deleted takes its queue with it: leave the rows to the delete.
		var held bool
		if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM workspaces WHERE id=$1 FOR KEY SHARE SKIP LOCKED)`, workspaceID).Scan(&held); err != nil || held {
			return err
		}
		var finished []int64
		for _, p := range pending {
			ok, err := handleKBGapPending(ctx, tx, workspaceID, p)
			if err != nil {
				return fmt.Errorf("%s %s: %w", p.kind, p.sourceID, err)
			}
			if ok {
				finished = append(finished, p.id)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM kb_gap_pending WHERE workspace_id=$1 AND id=ANY($2)`, workspaceID, finished); err != nil {
			return err
		}
		done, more = len(finished), len(pending) == kbGapBatch && len(finished) > 0
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return done, more, nil
}

// lockKBGaps lets one transaction at a time change a workspace's gaps; false when another holds it.
func lockKBGaps(ctx context.Context, tx pgx.Tx, workspaceID string) (bool, error) {
	var locked bool
	err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('kb_gaps:' || $1::text, 0))`, workspaceID).Scan(&locked)
	return locked, err
}

// handleKBGapPending reads one queued row's source and files its signal, or marks the gaps a
// published article answers; false leaves the row queued because its source is held by another
// transaction.
func handleKBGapPending(ctx context.Context, tx pgx.Tx, workspaceID string, p kbGapPendingRow) (bool, error) {
	if p.kind == "article_published" {
		return fixKBGapsByArticle(ctx, tx, workspaceID, p.sourceID)
	}
	signal, ok, err := readKBGapSource(ctx, tx, workspaceID, p.kind, p.sourceID)
	if err != nil || !ok || signal == nil {
		return ok, err
	}
	return true, placeKBGapSignal(ctx, tx, workspaceID, *signal)
}

// RunKBGaps groups queued signals every interval and looks for recurring topics once a day, until
// ctx ends. Errors go to logError; the next tick tries again.
func (s *Store) RunKBGaps(ctx context.Context, interval time.Duration, logError func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var topicsAt time.Time
	for {
		if _, err := s.BuildKBGaps(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			logError(err)
		}
		if time.Since(topicsAt) >= kbGapTopicsEvery {
			topicsAt = time.Now()
			if _, err := s.BuildKBGapTopics(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
				logError(err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
