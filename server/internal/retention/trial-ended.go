package retention

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// trialDecisionGrace is how long a trial must have ended before its milestone is decided: a payment or a
// reconcile that was already on its way when the trial ended reaches the server within it.
const trialDecisionGrace = time.Hour

// recordEndedTrials writes the trial_ended milestone of every workspace whose trial ended at least an hour
// ago, at the moment the trial ended. A trial ends at read time (domain.NewAccess), so no write marks it; this
// pass, which runs only where billing is on, is where the fact is first seen. It writes only when it is proven
// that nothing paid or complimentary was in force before the end; when that cannot be shown it writes nothing,
// because a missing milestone is found by the reconcile and a false one stays for good. A workspace that locked
// and paid later still gets it. It is proven when:
//   - no paid event is dated before the end, and no complimentary plan was granted before it; and
//   - the workspace has no stored subscription, or the first billing event the server ever stored for it is a
//     subscription.created that arrived after the end. A stored subscription has no start time (source_modified_at
//     is its last change), and a reconcile or an update can store one that began before the end, so any other
//     first event, or none, leaves its start unknown.
//
// The first trial end recorded stays (a trial offer that reopens it changes nothing).
func (s *Service) recordEndedTrials(ctx context.Context, now time.Time) error {
	err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO workspace_events(workspace_id,kind,at,source)
			SELECT w.id, 'trial_ended', w.trial_ends_at, 'app' FROM workspaces w
			WHERE w.trial_ends_at <= $1
				AND NOT EXISTS (SELECT 1 FROM workspace_events e WHERE e.workspace_id = w.id AND e.kind = 'trial_ended')
				AND NOT EXISTS (SELECT 1 FROM workspace_events p WHERE p.workspace_id = w.id AND p.kind = 'paid' AND p.at < w.trial_ends_at)
				AND NOT EXISTS (SELECT 1 FROM workspace_comp_plans c WHERE c.workspace_id = w.id AND c.granted_at <= w.trial_ends_at)
				AND (NOT EXISTS (SELECT 1 FROM workspace_billing b WHERE b.workspace_id = w.id)
					OR COALESCE((SELECT f.event_type = 'subscription.created' AND f.received_at > w.trial_ends_at
						FROM billing_webhook_events f WHERE f.workspace_id = w.id ORDER BY f.received_at, f.webhook_id LIMIT 1), false))
			ORDER BY w.trial_ends_at, w.id
			ON CONFLICT (workspace_id, kind) WHERE kind NOT IN ('paid','cancelled') DO NOTHING`, now.Add(-trialDecisionGrace))
		return err
	})
	if err != nil {
		return fmt.Errorf("record ended trials: %w", err)
	}
	return nil
}
