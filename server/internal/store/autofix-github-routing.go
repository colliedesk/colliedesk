package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// The only lookups of Autofix that cross workspaces (PrivilegeGitHubInstallation): routing a GitHub
// webhook to the workspace that owns its installation, the delivery ids already applied, and the list of
// workspaces the reconcile pass visits. Everything else runs scoped.

// WebhookDeliveriesKept is how long an applied delivery id is remembered (GitHub redelivers within days).
const WebhookDeliveriesKept = 30 * 24 * time.Hour

// GitHubWebhookRoute finds the workspace that owns a GitHub installation and whether the delivery was
// applied already, in one privileged read. found is false when no workspace connected the installation.
func (s *Store) GitHubWebhookRoute(ctx context.Context, installationID int64, deliveryID string) (workspaceID string, found, seen bool, err error) {
	err = s.db.Privileged(ctx, database.PrivilegeGitHubInstallation, func(tx pgx.Tx) error {
		if deliveryID != "" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM autofix_webhook_deliveries WHERE source = 'github' AND delivery_id = $1)`,
				deliveryID).Scan(&seen); err != nil {
				return err
			}
		}
		if installationID <= 0 {
			return nil
		}
		err := tx.QueryRow(ctx, `SELECT workspace_id::text FROM autofix_github_installations WHERE installation_id = $1`, installationID).Scan(&workspaceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		return "", false, false, fmt.Errorf("route the GitHub webhook: %w", err)
	}
	return workspaceID, found, seen, nil
}

// RecordGitHubDelivery remembers a delivery once its event was applied, so GitHub's redelivery of it has no
// second effect. Recorded after applying: a delivery that failed half way is applied again when redelivered
// (every move is idempotent), never lost.
func (s *Store) RecordGitHubDelivery(ctx context.Context, deliveryID string) error {
	if deliveryID == "" {
		return nil
	}
	return s.db.Privileged(ctx, database.PrivilegeGitHubInstallation, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO autofix_webhook_deliveries (source, delivery_id) VALUES ('github', $1) ON CONFLICT DO NOTHING`, deliveryID)
		return err
	})
}

// GitHubInstallationRef is one installation and the workspace that owns it.
type GitHubInstallationRef struct {
	WorkspaceID    string
	InstallationID int64
}

// GitHubInstallationsToReconcile lists every connected installation of the server with its workspace, and
// forgets delivery ids older than WebhookDeliveriesKept on the way.
func (s *Store) GitHubInstallationsToReconcile(ctx context.Context, now time.Time) ([]GitHubInstallationRef, error) {
	var refs []GitHubInstallationRef
	err := s.db.Privileged(ctx, database.PrivilegeGitHubInstallation, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM autofix_webhook_deliveries WHERE received_at < $1`, now.Add(-WebhookDeliveriesKept)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT workspace_id::text, installation_id FROM autofix_github_installations ORDER BY workspace_id, installation_id`)
		if err != nil {
			return err
		}
		refs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (GitHubInstallationRef, error) {
			var ref GitHubInstallationRef
			return ref, row.Scan(&ref.WorkspaceID, &ref.InstallationID)
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list GitHub installations: %w", err)
	}
	return refs, nil
}
