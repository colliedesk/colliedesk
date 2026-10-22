package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// maxHostClaims bounds the pending claims of one host name the certificate question checks.
const maxHostClaims = 5

// takenMessage is the check_error of a domain another workspace was verified on first.
const takenMessage = "Another workspace's help center was verified on this host name first."

const helpCenterDomainColumns = `hostname, claim, token, status, created_at, verified_at, checked_at, check_error, certificate_requested_at, failing_since, pending_reason`

func scanHelpCenterDomain(row pgx.Row) (domain.HelpCenterDomain, error) {
	var d domain.HelpCenterDomain
	err := row.Scan(&d.Hostname, &d.Claim, &d.Token, &d.Status, &d.CreatedAt, &d.VerifiedAt, &d.CheckedAt, &d.CheckError, &d.CertificateRequestedAt, &d.FailingSince, &d.PendingReason)
	d.CreatedAt = d.CreatedAt.UTC()
	d.VerifiedAt, d.CheckedAt, d.CertificateRequestedAt = domain.UTCOptional(d.VerifiedAt), domain.UTCOptional(d.CheckedAt), domain.UTCOptional(d.CertificateRequestedAt)
	d.FailingSince = domain.UTCOptional(d.FailingSince)
	return d, err
}

// GetHelpCenterDomain returns the workspace's custom domain; found is false when it has none.
func (s *Store) GetHelpCenterDomain(ctx context.Context, workspaceID string) (domain.HelpCenterDomain, bool, error) {
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterDomain, error) {
		return scanHelpCenterDomain(tx.QueryRow(ctx, `SELECT `+helpCenterDomainColumns+` FROM help_center_domains WHERE workspace_id=$1`, workspaceID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HelpCenterDomain{}, false, nil
	}
	if err != nil {
		return domain.HelpCenterDomain{}, false, fmt.Errorf("get help center domain: %w", err)
	}
	return got, true, nil
}

// SetHelpCenterDomain sets the workspace's custom domain to host (already cleaned by
// helpcenter.CustomHost). The same host name again changes nothing; another one replaces the old
// with a new claim: a new token and a new CNAME target, pending until a check passes. Whether another
// workspace serves on host is the caller's question (HelpCenterHost): pending claims of several
// workspaces may coexist.
func (s *Store) SetHelpCenterDomain(ctx context.Context, workspaceID, host string, now time.Time) (domain.HelpCenterDomain, error) {
	token, err := randomHex(16)
	if err != nil {
		return domain.HelpCenterDomain{}, err
	}
	claim, err := randomHex(12)
	if err != nil {
		return domain.HelpCenterDomain{}, err
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterDomain, error) {
		current, err := scanHelpCenterDomain(tx.QueryRow(ctx, `SELECT `+helpCenterDomainColumns+` FROM help_center_domains WHERE workspace_id=$1 FOR UPDATE`, workspaceID))
		if err == nil && current.Hostname == host {
			return current, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.HelpCenterDomain{}, err
		}
		// The export writes the domain on the help center's line, so the line must exist.
		if _, err := tx.Exec(ctx, `INSERT INTO help_centers (workspace_id) VALUES ($1) ON CONFLICT (workspace_id) DO NOTHING`, workspaceID); err != nil {
			return domain.HelpCenterDomain{}, err
		}
		return scanHelpCenterDomain(tx.QueryRow(ctx, `INSERT INTO help_center_domains (workspace_id, hostname, claim, token, status, created_at)
			VALUES ($1, $2, $3, $4, 'pending', $5)
			ON CONFLICT (workspace_id) DO UPDATE SET hostname=EXCLUDED.hostname, claim=EXCLUDED.claim, token=EXCLUDED.token, status='pending', created_at=EXCLUDED.created_at,
				verified_at=NULL, checked_at=NULL, check_error=NULL, certificate_requested_at=NULL, failing_since=NULL, pending_reason=NULL
			RETURNING `+helpCenterDomainColumns, workspaceID, host, claim, token, now))
	})
	if err != nil {
		return domain.HelpCenterDomain{}, fmt.Errorf("set help center domain: %w", err)
	}
	return got, nil
}

// randomHex is size random bytes in hex: a domain's token (16) or claim (12).
func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make help center domain claim: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// RecordHelpCenterDomainCheck stores the result of a DNS check of the claim, the workspace's custom
// domain when the check started. A check that passed makes it active (certificate also notes that the
// reverse proxy was told it may get a certificate); one that failed keeps its status and stores why,
// so a check never deactivates a domain. It returns ErrHelpCenterDomainMissing when the claim was
// removed or replaced meanwhile, even by the same host name again, and ErrHelpCenterDomainTaken, with
// the failure recorded, when another workspace became active on the host first.
func (s *Store) RecordHelpCenterDomainCheck(ctx context.Context, workspaceID, claim string, now time.Time, failure error, certificate bool) (domain.HelpCenterDomain, error) {
	record := func(query string, args ...any) (domain.HelpCenterDomain, error) {
		return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterDomain, error) {
			return scanHelpCenterDomain(tx.QueryRow(ctx, query, args...))
		})
	}
	failed := func(message string) (domain.HelpCenterDomain, error) {
		return record(`UPDATE help_center_domains SET checked_at=$3, check_error=$4 WHERE workspace_id=$1 AND claim=$2
			RETURNING `+helpCenterDomainColumns, workspaceID, claim, now, clipCheckError(message))
	}
	var got domain.HelpCenterDomain
	var err error
	if failure != nil {
		got, err = failed(failure.Error())
	} else {
		got, err = record(`UPDATE help_center_domains SET status='active', verified_at=COALESCE(verified_at, $3), checked_at=$3, check_error=NULL,
			certificate_requested_at=CASE WHEN $4 THEN $3 ELSE certificate_requested_at END
			WHERE workspace_id=$1 AND claim=$2 RETURNING `+helpCenterDomainColumns, workspaceID, claim, now, certificate)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			if _, err := failed(takenMessage); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return domain.HelpCenterDomain{}, fmt.Errorf("record help center domain check: %w", err)
			}
			return domain.HelpCenterDomain{}, domain.ErrHelpCenterDomainTaken
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HelpCenterDomain{}, domain.ErrHelpCenterDomainMissing
	}
	if err != nil {
		return domain.HelpCenterDomain{}, fmt.Errorf("record help center domain check: %w", err)
	}
	return got, nil
}

func clipCheckError(message string) string {
	runes := []rune(message)
	if len(runes) <= domain.MaxHelpCenterDomainCheckError {
		return message
	}
	return string(runes[:domain.MaxHelpCenterDomainCheckError-1]) + "…"
}

// NoteHelpCenterCertificate records that the reverse proxy was told it may get a certificate for the
// workspace's active custom domain host.
func (s *Store) NoteHelpCenterCertificate(ctx context.Context, workspaceID, host string, now time.Time) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE help_center_domains SET certificate_requested_at=$3 WHERE workspace_id=$1 AND hostname=$2 AND status='active'`,
			workspaceID, host, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("note help center certificate: %w", err)
	}
	return nil
}

// DeleteHelpCenterDomain removes the workspace's custom domain; removing none changes nothing.
func (s *Store) DeleteHelpCenterDomain(ctx context.Context, workspaceID string) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM help_center_domains WHERE workspace_id=$1`, workspaceID)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete help center domain: %w", err)
	}
	return nil
}

// HelpCenterHost finds the workspace whose help center serves on host, an active custom domain; found
// is false for any other name. It crosses workspaces, since a request's host name is all there is;
// only the workspace id leaves the transaction.
func (s *Store) HelpCenterHost(ctx context.Context, host string) (string, bool, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterHost, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM help_center_domains WHERE lower(hostname)=lower($1) AND status='active'`, host).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find help center host: %w", err)
	}
	return workspaceID, true, nil
}

// HelpCenterClaim finds the workspace of the claim whose CNAME target hc-<claim>.<HELP_CENTER_DOMAIN> a
// request named; found is false for a claim nobody holds (never made, removed or replaced). Only the
// workspace id leaves the transaction.
func (s *Store) HelpCenterClaim(ctx context.Context, claim string) (string, bool, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterHost, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM help_center_domains WHERE claim=$1`, claim).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find help center claim: %w", err)
	}
	return workspaceID, true, nil
}

// HelpCenterHostClaims lists the workspaces, oldest claim first and at most five, that set host as
// their custom domain and wait for a check to pass. Only workspace ids leave the transaction.
func (s *Store) HelpCenterHostClaims(ctx context.Context, host string) ([]string, error) {
	var claims []string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterHost, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text FROM help_center_domains WHERE lower(hostname)=lower($1) AND status='pending'
			ORDER BY created_at, workspace_id LIMIT $2`, host, maxHostClaims)
		if err != nil {
			return err
		}
		claims, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("find help center host claims: %w", err)
	}
	return claims, nil
}

// ActiveHelpCenterDomainWorkspaces lists the workspaces with an active custom domain, for the daily
// re-check. Only workspace ids leave the transaction.
func (s *Store) ActiveHelpCenterDomainWorkspaces(ctx context.Context) ([]string, error) {
	var workspaces []string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterDomainRecheck, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text FROM help_center_domains WHERE status='active' ORDER BY workspace_id`)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list active help center domains: %w", err)
	}
	return workspaces, nil
}
