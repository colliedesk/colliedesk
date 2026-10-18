package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// ProviderDomainDeletion is a provider domain waiting to be deleted at the email provider.
type ProviderDomainDeletion struct {
	WorkspaceID, ProviderDomainID string
	Attempts                      int
}

// QueueProviderDomainDeletion records a provider domain the workspace will not use, so a background sweep
// deletes it. Queuing twice is harmless.
func (s *Store) QueueProviderDomainDeletion(ctx context.Context, workspaceID, providerDomainID string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, workspaceID, providerDomainID)
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("queue provider domain deletion: %w", err)
	}
	return nil
}

// DueProviderDomainDeletions lists the queued provider domains whose next attempt is due, across
// workspaces, including those of workspaces that no longer exist.
func (s *Store) DueProviderDomainDeletions(ctx context.Context, now time.Time, limit int) ([]ProviderDomainDeletion, error) {
	var due []ProviderDomainDeletion
	err := s.db.Privileged(ctx, database.PrivilegeProviderDomainSweep, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,provider_domain_id,attempts FROM email_provider_domain_trash
			WHERE next_attempt_at<=$1 ORDER BY next_attempt_at LIMIT $2`, now, limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProviderDomainDeletion, error) {
			var item ProviderDomainDeletion
			err := row.Scan(&item.WorkspaceID, &item.ProviderDomainID, &item.Attempts)
			return item, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list provider domains to delete: %w", err)
	}
	return due, nil
}

// ProviderDomainDeleted removes the record once the provider confirmed the domain is gone.
func (s *Store) ProviderDomainDeleted(ctx context.Context, item ProviderDomainDeletion) error {
	err := s.db.Privileged(ctx, database.PrivilegeProviderDomainSweep, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM email_provider_domain_trash WHERE workspace_id=$1 AND provider_domain_id=$2`, item.WorkspaceID, item.ProviderDomainID)
		return err
	})
	if err != nil {
		return fmt.Errorf("settle provider domain: %w", err)
	}
	return nil
}

// ProviderDomainDeleteFailed keeps the record, counts the attempt and schedules the next one.
func (s *Store) ProviderDomainDeleteFailed(ctx context.Context, item ProviderDomainDeletion, reason string, next time.Time) error {
	err := s.db.Privileged(ctx, database.PrivilegeProviderDomainSweep, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE email_provider_domain_trash SET attempts=attempts+1,last_error=$3,next_attempt_at=$4
			WHERE workspace_id=$1 AND provider_domain_id=$2`, item.WorkspaceID, item.ProviderDomainID, reason, next)
		return err
	})
	if err != nil {
		return fmt.Errorf("record provider domain delete failure: %w", err)
	}
	return nil
}
