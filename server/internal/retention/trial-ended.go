package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// recordEndedTrials writes the trial_ended milestone of every workspace whose trial has run out, at the
// moment the trial ended. A trial ends at read time (domain.NewAccess), so no write marks it; this pass,
// which runs only where billing is on, is where the fact is first seen. The milestone is decided at the
// trial's end, not at the time of the pass: a workspace that locked and paid later still gets it. Only a
// workspace with a subscription or a complimentary plan already in force at the trial's end converted and
// gets none: it has a paid event before that moment, or a stored subscription last changed before it and
// still running (a reconcile may store one without a paid event), or a complimentary plan granted before it
// and not revoked or expired by it. The first trial end recorded stays (a trial offer that reopens it
// changes nothing).
func (s *Service) recordEndedTrials(ctx context.Context, now time.Time) error {
	err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source)
			SELECT w.id, 'trial_ended', w.trial_ends_at, 'app' FROM workspaces w
			WHERE w.trial_ends_at <= $1
				AND NOT EXISTS (SELECT 1 FROM workspace_events e WHERE e.workspace_id = w.id AND e.kind = 'trial_ended')
				AND NOT EXISTS (SELECT 1 FROM workspace_events p WHERE p.workspace_id = w.id AND p.kind = 'paid' AND p.at < w.trial_ends_at)
				AND NOT EXISTS (SELECT 1 FROM workspace_billing b WHERE b.workspace_id = w.id AND b.source_modified_at < w.trial_ends_at
					AND b.status IN ('active','trialing','past_due') AND (b.ended_at IS NULL OR b.ended_at > w.trial_ends_at))
				AND NOT EXISTS (SELECT 1 FROM workspace_comp_plans c WHERE c.workspace_id = w.id AND c.granted_at < w.trial_ends_at
					AND (c.revoked_at IS NULL OR c.revoked_at > w.trial_ends_at) AND (c.until IS NULL OR c.until > w.trial_ends_at))
			ORDER BY w.trial_ends_at, w.id
			ON CONFLICT (workspace_id, kind) WHERE kind NOT IN ('paid','cancelled') DO NOTHING`, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("record ended trials: %w", err)
	}
	return nil
}
