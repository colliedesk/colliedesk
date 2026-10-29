package serverconfig

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// Preview counts what a change would do across workspaces, for the console's previews (XT). It returns
// numbers and yes or no only, never a row of a workspace.
type Preview struct{ db *database.DB }

// NewPreview reads on the owner connection.
func NewPreview(db *database.DB) *Preview { return &Preview{db: db} }

// LockedWithBilling counts the workspaces that lose access the moment billing turns on: their trial ended
// and nothing paying or complimentary is in force (the rule of retention's candidates).
func (p *Preview) LockedWithBilling(ctx context.Context, now time.Time) (int, error) {
	var count int
	err := p.db.Privileged(ctx, database.PrivilegeOperatorPreview, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM workspaces w
			WHERE w.trial_ends_at <= $1
			  AND NOT EXISTS (SELECT 1 FROM workspace_billing b WHERE b.workspace_id = w.id
			      AND (b.ended_at IS NULL OR b.ended_at > $1)
			      AND (b.status IN ('active','trialing','past_due') OR (b.status = 'canceled' AND b.current_period_end > $1)))
			  AND NOT EXISTS (SELECT 1 FROM workspace_comp_plans c WHERE c.workspace_id = w.id
			      AND c.revoked_at IS NULL AND (c.until IS NULL OR c.until > $1))`, now).Scan(&count)
	})
	if err != nil {
		return 0, fmt.Errorf("count the workspaces billing would lock: %w", err)
	}
	return count, nil
}

// WorkspaceExists says whether a workspace has the slug.
func (p *Preview) WorkspaceExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := p.db.Privileged(ctx, database.PrivilegeOperatorPreview, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE slug = $1)`, slug).Scan(&exists)
	})
	if err != nil {
		return false, fmt.Errorf("look up the workspace: %w", err)
	}
	return exists, nil
}
