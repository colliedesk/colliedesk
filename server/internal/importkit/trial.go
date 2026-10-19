package importkit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// trialExtensionHours is what an import adds to a trial that is still running: the original 14 days
// become 30 days in total. It is counted in hours so a session time zone with daylight saving
// cannot move the end by an hour.
const trialExtensionHours = 16 * 24

// ExtendTrial gives a workspace that holds an imported conversation (ledger entity "ticket", any
// importer) its one trial extension. The update is relative to the stored end, so it needs no
// knowledge of the trial start, and its predicates make it atomic: a trial already extended, or
// already over (locked), is left alone, so an import never unlocks a workspace. It returns the new
// end, or nil when nothing changed.
func ExtendTrial(ctx context.Context, db *database.DB, workspaceID string, now time.Time) (*time.Time, error) {
	var extendedTo *time.Time
	err := db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var imported bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM import_items WHERE workspace_id=$1 AND entity='ticket' AND status='done')`,
			workspaceID).Scan(&imported); err != nil {
			return fmt.Errorf("check imported tickets: %w", err)
		}
		if !imported {
			return nil
		}
		var ends time.Time
		err := tx.QueryRow(ctx, `UPDATE workspaces SET trial_ends_at = trial_ends_at + make_interval(hours => $3), trial_extended_at = $2
			WHERE id=$1 AND trial_extended_at IS NULL AND trial_ends_at > $2 RETURNING trial_ends_at`,
			workspaceID, now, trialExtensionHours).Scan(&ends)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("extend trial: %w", err)
		}
		ends = ends.UTC()
		extendedTo = &ends
		return nil
	})
	return extendedTo, err
}
