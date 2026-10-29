package serverconfig

import (
	"context"
	"errors"
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

// AnyOpenHelpCenter returns the slug of one workspace whose help center is on, so a trial of the help
// center domain can fetch a real page under the new address; "" when none is.
func (p *Preview) AnyOpenHelpCenter(ctx context.Context) (string, error) {
	var slug string
	err := p.db.Privileged(ctx, database.PrivilegeOperatorPreview, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT w.slug FROM workspaces w JOIN help_centers h ON h.workspace_id = w.id
			WHERE h.enabled ORDER BY w.created_at, w.id LIMIT 1`).Scan(&slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return "", fmt.Errorf("find an open help center: %w", err)
	}
	return slug, nil
}

// MailboxConnections counts the email channels sending through a provider (google, microsoft) and the
// workspaces they belong to, for switching it off.
func (p *Preview) MailboxConnections(ctx context.Context, provider string) (channels, workspaces int, err error) {
	err = p.db.Privileged(ctx, database.PrivilegeOperatorPreview, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), count(DISTINCT workspace_id) FROM mailbox_connections WHERE provider = $1`, provider).Scan(&channels, &workspaces)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("count the mailbox connections: %w", err)
	}
	return channels, workspaces, nil
}

// ServerAIWorkspaces counts the workspaces with an AI feature on that runs on the server's provider.
func (p *Preview) ServerAIWorkspaces(ctx context.Context) (int, error) {
	var count int
	err := p.db.Privileged(ctx, database.PrivilegeOperatorPreview, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(DISTINCT workspace_id) FROM ai_slots WHERE uses_server AND enabled`).Scan(&count)
	})
	if err != nil {
		return 0, fmt.Errorf("count the workspaces using the server's AI provider: %w", err)
	}
	return count, nil
}
