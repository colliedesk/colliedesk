package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// recordEndedTrials writes the trial_ended milestone of every workspace whose trial ran out with nothing
// paying or complimentary in force now, at the moment the trial ended. A trial ends at read time
// (domain.NewAccess), so no write marks it; this pass, which runs only where billing is on, is where the
// fact is first seen. A workspace that paid before its trial ended converted and gets none, even when it
// cancels later. The first trial end recorded stays (a trial offer that reopens it changes nothing).
func (s *Service) recordEndedTrials(ctx context.Context, now time.Time) error {
	err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source)
			SELECT w.id, 'trial_ended', w.trial_ends_at, 'app' FROM workspaces w
			WHERE w.trial_ends_at <= $1 AND `+nothingInForce+`
				AND NOT EXISTS (SELECT 1 FROM workspace_events e WHERE e.workspace_id = w.id AND e.kind = 'trial_ended')
				AND NOT EXISTS (SELECT 1 FROM workspace_events p WHERE p.workspace_id = w.id AND p.kind = 'paid' AND p.at < w.trial_ends_at)
			ORDER BY w.trial_ends_at, w.id
			ON CONFLICT (workspace_id, kind) WHERE kind NOT IN ('paid','cancelled') DO NOTHING`, now, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("record ended trials: %w", err)
	}
	return nil
}
