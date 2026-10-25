package store

import (
	"context"
	"errors"
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

// providerDomainClaimLease is how long a claimed deletion is hidden from other sweepers while its provider
// call runs; a call that fails or never finishes comes due again after it.
const providerDomainClaimLease = 5 * time.Minute

// ClaimProviderDomainDeletions takes the queued provider domains whose next attempt is due, across
// workspaces, including those of workspaces that no longer exist. A row another sweeper holds is skipped,
// and claimed rows are hidden for a lease, so replicas do not delete the same domain twice. The claim is
// committed before any provider call.
func (s *Store) ClaimProviderDomainDeletions(ctx context.Context, now time.Time, limit int) ([]ProviderDomainDeletion, error) {
	var due []ProviderDomainDeletion
	err := s.db.Privileged(ctx, database.PrivilegeProviderDomainSweep, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `UPDATE email_provider_domain_trash SET next_attempt_at=$3
			WHERE (workspace_id,provider_domain_id) IN (
				SELECT workspace_id,provider_domain_id FROM email_provider_domain_trash
				WHERE next_attempt_at<=$1 ORDER BY next_attempt_at LIMIT $2 FOR UPDATE SKIP LOCKED)
			RETURNING workspace_id::text,provider_domain_id,attempts`, now, limit, now.Add(providerDomainClaimLease))
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
		return nil, fmt.Errorf("claim provider domains to delete: %w", err)
	}
	return due, nil
}

// ExpireUnverifiedProviderDomains releases the provider domains that stayed unverified since before
// olderThan: each is queued for deletion and cleared from its channel, which records when. A domain the
// provider ever reported verified is never touched, even if it reads as not verified right now. It returns how many were released.
func (s *Store) ExpireUnverifiedProviderDomains(ctx context.Context, olderThan, now time.Time) (int, error) {
	var released int
	err := s.db.Privileged(ctx, database.PrivilegeProviderDomainSweep, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `WITH old AS (
				SELECT workspace_id,channel_id,provider_domain_id FROM email_channels
				WHERE provider_domain_id IS NOT NULL AND provider_domain_first_verified_at IS NULL AND provider_domain_created_at<=$1 FOR UPDATE),
			cleared AS (
				UPDATE email_channels c SET provider_domain_id=NULL,provider_domain_records='[]'::jsonb,provider_domain_created_at=NULL,provider_domain_expired_at=$2,updated_at=$2
				FROM old WHERE c.channel_id=old.channel_id RETURNING c.workspace_id),
			queued AS (
				INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) SELECT workspace_id,provider_domain_id FROM old
				ON CONFLICT DO NOTHING RETURNING 1)
			SELECT count(*) FROM cleared`, olderThan, now).Scan(&released)
	})
	if err != nil {
		return 0, fmt.Errorf("expire unverified provider domains: %w", err)
	}
	return released, nil
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

// ReleasedDomain names what ReleaseVerifiedDomain cleared.
type ReleasedDomain struct {
	WorkspaceID, Slug, ProviderDomainID string
}

// ReleaseVerifiedDomain takes a domain name away from the workspace that holds it verified through the
// email provider, so another workspace can verify it: the holder's provider domain is queued for deletion
// and its channel loses the provider domain and the verification (it sends from the platform address
// again). It returns nil when no workspace holds the name. This is the operator's way out of a holder that
// was abandoned; no API route does it.
func (s *Store) ReleaseVerifiedDomain(ctx context.Context, domainName string, now time.Time) (*ReleasedDomain, error) {
	var released *ReleasedDomain
	err := s.db.Privileged(ctx, database.PrivilegeProviderDomainSweep, func(tx pgx.Tx) error {
		var item ReleasedDomain
		var channelID string
		err := tx.QueryRow(ctx, `SELECT c.workspace_id::text,w.slug,c.provider_domain_id,c.channel_id::text FROM email_channels c JOIN workspaces w ON w.id=c.workspace_id
			WHERE c.provider_domain_id IS NOT NULL AND c.domain_verified_at IS NOT NULL AND split_part(c.support_address,'@',2)=lower($1) FOR UPDATE OF c`,
			domainName).Scan(&item.WorkspaceID, &item.Slug, &item.ProviderDomainID, &channelID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, item.WorkspaceID, item.ProviderDomainID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE email_channels SET provider_domain_id=NULL,provider_domain_records='[]'::jsonb,domain_verified_at=NULL,
				provider_domain_first_verified_at=NULL,provider_domain_created_at=NULL,provider_domain_expired_at=NULL,updated_at=$2 WHERE channel_id::text=$1`, channelID, now); err != nil {
			return err
		}
		released = &item
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("release verified domain: %w", err)
	}
	return released, nil
}
