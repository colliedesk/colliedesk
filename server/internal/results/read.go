package results

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// Snapshot is a stored snapshot as the API returns it.
type Snapshot struct {
	ID         string    `json:"id"`
	ComputedAt time.Time `json:"computed_at"`
	MeasuredAt time.Time `json:"measured_at"`
	Data
}

// HoursSaved is the upper bound of time AI drafts saved in a window.
type HoursSaved struct {
	Hours               float64 `json:"hours"`
	Conversations       int     `json:"conversations"`
	MedianHandleMinutes int64   `json:"median_handle_minutes"`
}

// ComputeError is why the last compute failed.
type ComputeError struct {
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Page is GET /api/v1/results.
type Page struct {
	Current       *Snapshot              `json:"current"`
	Baseline      *Snapshot              `json:"baseline"`
	BaselineState string                 `json:"baseline_state"`
	HoursSaved    map[string]*HoursSaved `json:"hours_saved"`
	Error         *ComputeError          `json:"error"`
}

// Read returns the newest stored numbers; nothing is computed. The compute error is for admins.
func Read(ctx context.Context, tx pgx.Tx, workspaceID string, admin bool) (Page, error) {
	page := Page{BaselineState: "none", HoursSaved: map[string]*HoursSaved{}}
	var source, message *string
	var errorAt *time.Time
	err := tx.QueryRow(ctx, `SELECT baseline_source,error,error_at FROM results_state WHERE workspace_id=$1`, workspaceID).Scan(&source, &message, &errorAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return page, fmt.Errorf("read results state: %w", err)
	}
	if admin && message != nil {
		page.Error = &ComputeError{Message: *message, At: errorAt.UTC()}
	}
	if page.Current, err = readSnapshot(ctx, tx, workspaceID, "current"); err != nil {
		return page, err
	}
	if source != nil {
		page.BaselineState = "computing"
		if page.Baseline, err = readSnapshot(ctx, tx, workspaceID, "baseline"); err != nil {
			return page, err
		}
		if page.Baseline != nil {
			page.BaselineState = "ready"
		}
	}
	for _, window := range Windows {
		page.HoursSaved[window.Key] = hoursSaved(page.Current, page.Baseline, window.Key)
	}
	return page, nil
}

// hoursSaved is conversations with a draft sent nearly unchanged times the baseline's median handle
// time (docs/user/results.md); nil without a median from at least MinSample conversations.
func hoursSaved(current, baseline *Snapshot, window string) *HoursSaved {
	if current == nil || baseline == nil || current.Windows[window] == nil || baseline.Windows[window] == nil {
		return nil
	}
	ai, handle := current.Windows[window].AI, baseline.Windows[window].Handle
	if ai == nil || handle == nil || handle.MedianMinutes == nil {
		return nil
	}
	hours := math.Round(float64(ai.ConversationsSentUnchanged)*float64(*handle.MedianMinutes)/60*10) / 10
	return &HoursSaved{Hours: hours, Conversations: ai.ConversationsSentUnchanged, MedianHandleMinutes: *handle.MedianMinutes}
}

func readSnapshot(ctx context.Context, tx pgx.Tx, workspaceID, kind string) (*Snapshot, error) {
	var snapshot Snapshot
	var data []byte
	err := tx.QueryRow(ctx, `SELECT id::text,computed_at,measured_at,data FROM results_snapshots WHERE workspace_id=$1 AND kind=$2
		ORDER BY computed_at DESC LIMIT 1`, workspaceID, kind).Scan(&snapshot.ID, &snapshot.ComputedAt, &snapshot.MeasuredAt, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}
	snapshot.ComputedAt, snapshot.MeasuredAt = snapshot.ComputedAt.UTC(), snapshot.MeasuredAt.UTC()
	if err := json.Unmarshal(data, &snapshot.Data); err != nil {
		return nil, fmt.Errorf("read results: %w", err)
	}
	return &snapshot, nil
}
