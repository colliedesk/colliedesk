package results

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Data is a snapshot's numbers as stored in results_snapshots.data and returned by the API.
type Data struct {
	Windows map[string]*Numbers `json:"windows"`
	// HistorySince (current) is when Colliedesk's status history became complete.
	HistorySince *time.Time `json:"history_since,omitempty"`
	// Source, ImportStatus and End describe a baseline: the import it is measured from, that import's
	// reconcile status (empty for an import made before results existed) and where its windows end.
	Source       string     `json:"source,omitempty"`
	ImportStatus string     `json:"import_status,omitempty"`
	End          *time.Time `json:"end,omitempty"`
	// StatsRead counts the baseline conversations whose ticket stats were read; zero with conversations
	// means the import predates them and must run again for resolve times.
	StatsRead int `json:"stats_read"`
}

// keepCurrent is how long a replaced snapshot stays, so a number opened from a page loaded before a
// refresh still lists its conversations.
const keepCurrent = 24 * time.Hour

// ComputeCurrent computes the Colliedesk numbers measured at now and stores them, with the
// conversations each counts, in the workspace's transaction.
func ComputeCurrent(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (string, error) {
	in, err := LoadLive(ctx, tx, workspaceID, now)
	if err != nil {
		return "", err
	}
	numbers := Compute(in)
	data := Data{Windows: numbers, HistorySince: &in.HistorySince}
	id, err := writeSnapshot(ctx, tx, workspaceID, "current", now, data, Members(numbers))
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM results_snapshots WHERE workspace_id=$1 AND kind='current' AND id<>$2 AND computed_at<$3`,
		workspaceID, id, now.Add(-keepCurrent)); err != nil {
		return "", fmt.Errorf("remove old results: %w", err)
	}
	return id, nil
}

// ComputeBaseline computes the baseline of the workspace's import, or removes it when there is none.
// A baseline replaces the previous one at once: its import was run again or undone.
func ComputeBaseline(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) error {
	var source, status *string
	var anchor *time.Time
	err := tx.QueryRow(ctx, `SELECT baseline_source,baseline_anchor_at,baseline_import_status FROM results_state WHERE workspace_id=$1`, workspaceID).Scan(&source, &anchor, &status)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read the results baseline: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM results_snapshots WHERE workspace_id=$1 AND kind='baseline'`, workspaceID); err != nil {
		return fmt.Errorf("remove the old baseline: %w", err)
	}
	if source != nil {
		in, err := LoadBaseline(ctx, tx, workspaceID, *source, *anchor)
		if err != nil {
			return err
		}
		numbers := Compute(in)
		data := Data{Windows: numbers, Source: *source, End: &in.End}
		if status != nil {
			data.ImportStatus = *status
		}
		for _, c := range in.Conversations {
			if c.Stats != nil {
				data.StatsRead++
			}
		}
		if _, err := writeSnapshot(ctx, tx, workspaceID, "baseline", *anchor, data, Members(numbers)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE results_state SET baseline_dirty=false WHERE workspace_id=$1`, workspaceID); err != nil {
		return fmt.Errorf("mark the baseline computed: %w", err)
	}
	return nil
}

func writeSnapshot(ctx context.Context, tx pgx.Tx, workspaceID, kind string, measured time.Time, data Data, members map[string][]string) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("encode results: %w", err)
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO results_snapshots(workspace_id,kind,measured_at,data) VALUES($1,$2,$3,$4) RETURNING id::text`,
		workspaceID, kind, measured, encoded).Scan(&id); err != nil {
		return "", fmt.Errorf("store results: %w", err)
	}
	var metrics, conversations []string
	for metric, ids := range members {
		for _, conversation := range ids {
			metrics, conversations = append(metrics, metric), append(conversations, conversation)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO results_members(workspace_id,snapshot_id,metric,conversation_id)
		SELECT $1,$2,m.metric,m.conversation_id FROM unnest($3::text[],$4::uuid[]) AS m(metric,conversation_id)
		ON CONFLICT DO NOTHING`, workspaceID, id, metrics, conversations); err != nil {
		return "", fmt.Errorf("store the conversations behind the results: %w", err)
	}
	return id, nil
}
