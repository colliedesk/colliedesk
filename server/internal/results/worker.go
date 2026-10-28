package results

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// Worker computes every workspace's numbers in the background: the page only reads them.
type Worker struct {
	db  *database.DB
	now func() time.Time
	// Every is how often the worker looks for due workspaces; Batch how many it takes per look.
	Every time.Duration
	Batch int
	mail  *Mail
	// weeklyAt is when the weekly email was last looked at; it is looked at every few minutes.
	weeklyAt time.Time
}

// NewWorker returns a worker that looks every 15 seconds: a look that finds nothing due is one small
// query, and a finished import's baseline then shows within seconds, not minutes.
func NewWorker(db *database.DB) *Worker {
	return &Worker{db: db, now: time.Now, Every: 15 * time.Second, Batch: 20}
}

// Run computes due workspaces until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Every)
	defer ticker.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Error("compute results", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// lease is how long a claimed workspace stays with one server; a failed one waits as long.
const lease = 10 * time.Minute

// weeklyLook is how often the worker looks for a weekly email that is due.
const weeklyLook = 5 * time.Minute

// RunOnce computes every due workspace, a batch at a time; it returns how many it computed.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	computed := 0
	for {
		done, claimed, err := w.runBatch(ctx)
		computed += done
		if err != nil {
			return computed, err
		}
		if claimed < w.Batch {
			// The weekly email reads the numbers just computed; looking every 5 minutes is enough for 8:00.
			now := w.now().UTC()
			if now.Sub(w.weeklyAt) < weeklyLook && !w.weeklyAt.After(now) {
				return computed, nil
			}
			w.weeklyAt = now
			return computed, w.sendWeekly(ctx, now)
		}
	}
}

func (w *Worker) runBatch(ctx context.Context) (int, int, error) {
	now := w.now().UTC()
	var due []string
	err := w.db.Privileged(ctx, database.PrivilegeResultsClaim, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO results_state(workspace_id) SELECT id FROM workspaces w
			WHERE NOT EXISTS (SELECT 1 FROM results_state s WHERE s.workspace_id=w.id) ON CONFLICT DO NOTHING`); err != nil {
			return fmt.Errorf("add results state: %w", err)
		}
		// Due: the baseline changed, never computed, or an hour old (the compute then skips a workspace
		// whose inputs did not change, unless a day passed: resolutions settle with time alone).
		rows, err := tx.Query(ctx, `UPDATE results_state SET claimed_until=$1::timestamptz + $3::interval WHERE workspace_id IN (
				SELECT workspace_id FROM results_state
				WHERE (claimed_until IS NULL OR claimed_until<$1) AND (baseline_dirty OR computed_at IS NULL OR computed_at<$1::timestamptz - interval '1 hour')
				ORDER BY baseline_dirty DESC, computed_at NULLS FIRST LIMIT $2 FOR UPDATE SKIP LOCKED)
			RETURNING workspace_id::text`, now, w.Batch, lease.String())
		if err != nil {
			return fmt.Errorf("claim results: %w", err)
		}
		due, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	computed := 0
	for _, workspaceID := range due {
		if err := ctx.Err(); err != nil {
			return computed, len(due), err
		}
		done, err := w.compute(ctx, workspaceID, now)
		if err != nil {
			slog.Error("compute results", "workspace_id", workspaceID, "error", err)
			w.recordError(ctx, workspaceID, now, err)
			continue
		}
		if done {
			computed++
		}
	}
	return computed, len(due), nil
}

// inputs is what the current numbers depend on besides the clock: when any changes, or a day passes,
// they are computed again.
type inputs struct {
	Day           string     `json:"day"`
	Conversations *time.Time `json:"conversations"`
	Changes       *time.Time `json:"changes"`
	Drafts        *time.Time `json:"drafts"`
}

func (w *Worker) compute(ctx context.Context, workspaceID string, now time.Time) (bool, error) {
	done := false
	err := w.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		// A compute reads at most a few weeks of one workspace; it must never hold a connection long.
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='30s'`); err != nil {
			return err
		}
		var dirty bool
		var computedAt *time.Time
		var stored []byte
		if err := tx.QueryRow(ctx, `SELECT baseline_dirty,computed_at,inputs FROM results_state WHERE workspace_id=$1`, workspaceID).Scan(&dirty, &computedAt, &stored); err != nil {
			return fmt.Errorf("read results state: %w", err)
		}
		if computedAt == nil {
			backfilled, err := backfillBaseline(ctx, tx, workspaceID)
			if err != nil {
				return err
			}
			dirty = dirty || backfilled
		}
		current := inputs{Day: now.Format(time.DateOnly)}
		if err := tx.QueryRow(ctx, `SELECT (SELECT max(updated_at) FROM conversations WHERE workspace_id=$1),
			(SELECT max(changed_at) FROM conversation_status_changes WHERE workspace_id=$1),
			(SELECT max(decided_at) FROM ai_drafts WHERE workspace_id=$1)`, workspaceID).Scan(&current.Conversations, &current.Changes, &current.Drafts); err != nil {
			return fmt.Errorf("read results inputs: %w", err)
		}
		encoded, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if dirty {
			if err := ComputeBaseline(ctx, tx, workspaceID, now); err != nil {
				return err
			}
		}
		if dirty || computedAt == nil || string(stored) != string(encoded) || now.Sub(*computedAt) >= 24*time.Hour {
			if _, err := ComputeCurrent(ctx, tx, workspaceID, now); err != nil {
				return err
			}
			done = true
		}
		// Nothing changed: look again in an hour (the lease doubles as the next check).
		_, err = tx.Exec(ctx, `UPDATE results_state SET computed_at=CASE WHEN $4 THEN $2 ELSE computed_at END,inputs=$3,
			claimed_until=CASE WHEN $4 THEN NULL ELSE $2::timestamptz + interval '1 hour' END,error=NULL,error_at=NULL
			WHERE workspace_id=$1`, workspaceID, now, encoded, done)
		return err
	})
	return done, err
}

// recordError keeps a failed compute's message for admins; the lease makes the next try wait.
func (w *Worker) recordError(ctx context.Context, workspaceID string, now time.Time, cause error) {
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	if err := w.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE results_state SET error=$2,error_at=$3 WHERE workspace_id=$1`, workspaceID, message, now)
		return err
	}); err != nil {
		slog.Error("record results error", "workspace_id", workspaceID, "error", err)
	}
}

// backfillBaseline names the baseline of a workspace that imported before results existed, on its
// first compute only (later, an undone baseline must not come back): the import whose first run
// finished first, by its in-app job, or for the CLI when its ledger last changed. Its reconcile status
// stays unknown. A workspace with an in-app import still to finish is left alone: its tickets are
// still arriving, and the finished run names the baseline itself (MarkImportFinished).
func backfillBaseline(ctx context.Context, tx pgx.Tx, workspaceID string) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE results_state s SET baseline_source=b.source,baseline_anchor_at=b.anchor,baseline_import_status=NULL,baseline_dirty=true
		FROM (SELECT i.source,COALESCE((SELECT min(j.finished_at) FROM import_jobs j
				WHERE j.workspace_id=$1 AND j.kind='import' AND j.account_source=i.source AND j.status IS NOT NULL),max(i.updated_at)) AS anchor
			FROM import_items i WHERE i.workspace_id=$1 AND i.entity='ticket' AND i.status='done' GROUP BY i.source
			ORDER BY 2 LIMIT 1) b
		WHERE s.workspace_id=$1 AND s.baseline_source IS NULL
			AND NOT EXISTS (SELECT 1 FROM import_jobs j WHERE j.workspace_id=$1 AND j.kind='import' AND j.state IN ('queued','running','retry_wait','waiting_key'))`, workspaceID)
	if err != nil {
		return false, fmt.Errorf("find the baseline of an earlier import: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
