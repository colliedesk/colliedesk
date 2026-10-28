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

// takenMessage is the check_error of a claim whose host name another workspace holds and still proves.
const takenMessage = "Another workspace's help center serves on this host name, and its DNS records still prove it."

// takenOverMessage is the check_error of an active domain another workspace took over.
const takenOverMessage = "Another workspace's DNS records proved this host name while this domain's records did not, so its help center serves there now."

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

// ActivateHelpCenterDomain records a DNS check that proved the claim (proof is what DNS showed): the
// workspace's custom domain becomes, or stays, active, and any failure and release state is cleared
// (certificate also notes that the reverse proxy was told it may get a certificate). When another
// workspace is active on the host name, the claim takes it over if proof does not prove that
// workspace's claim too: in one transaction the holder goes back to pending (taken_over) and this
// claim becomes active, and released is the holder's workspace id. Otherwise it records the loss and
// returns ErrHelpCenterDomainTaken. ErrHelpCenterDomainMissing means the claim was removed or replaced
// meanwhile, even by the same host name again.
func (s *Store) ActivateHelpCenterDomain(ctx context.Context, workspaceID, claim string, now time.Time, proof domain.HelpCenterDomainProof, certificate bool) (got domain.HelpCenterDomain, released string, err error) {
	// A takeover that loses a race to another one finds a new holder; three rounds settle any race.
	for range 3 {
		got, err = scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterDomain, error) {
			return scanHelpCenterDomain(tx.QueryRow(ctx, `UPDATE help_center_domains SET `+activateColumns+`
				WHERE workspace_id=$1 AND claim=$2 RETURNING `+helpCenterDomainColumns, workspaceID, claim, now, certificate))
		})
		if !isUniqueViolation(err) {
			break
		}
		got, released, err = s.takeOverHelpCenterHost(ctx, workspaceID, claim, now, proof, certificate)
		if !isUniqueViolation(err) {
			break
		}
	}
	switch {
	case errors.Is(err, domain.ErrHelpCenterDomainTaken):
		if _, failed := s.RecordHelpCenterDomainFailure(ctx, workspaceID, claim, now, takenMessage); failed != nil && !errors.Is(failed, domain.ErrHelpCenterDomainMissing) {
			return domain.HelpCenterDomain{}, "", fmt.Errorf("record help center domain check: %w", failed)
		}
		return domain.HelpCenterDomain{}, "", err
	case errors.Is(err, pgx.ErrNoRows) || errors.Is(err, domain.ErrHelpCenterDomainMissing):
		return domain.HelpCenterDomain{}, "", domain.ErrHelpCenterDomainMissing
	case err != nil:
		return domain.HelpCenterDomain{}, "", fmt.Errorf("record help center domain check: %w", err)
	}
	return got, released, nil
}

// activateColumns is the SET list of a check that proved a claim; $3 is the check's time and $4
// whether the reverse proxy was told it may get a certificate.
const activateColumns = `status='active', verified_at=COALESCE(verified_at, $3), checked_at=$3, check_error=NULL, failing_since=NULL, pending_reason=NULL,
	certificate_requested_at=CASE WHEN $4 THEN $3 ELSE certificate_requested_at END`

// takeOverHelpCenterHost moves the host name of the workspace's claim from the workspace active on it
// to this claim, in one transaction, when proof does not prove the holder's claim (its CNAME target or
// its token). The holder's row is locked first, so two takeovers of one host run one after the other.
// It crosses workspaces: the holder's claim and token are compared in the database and never leave it.
func (s *Store) takeOverHelpCenterHost(ctx context.Context, workspaceID, claim string, now time.Time, proof domain.HelpCenterDomainProof, certificate bool) (domain.HelpCenterDomain, string, error) {
	var got domain.HelpCenterDomain
	var holder string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterDomainTakeover, func(tx pgx.Tx) error {
		var holderProves bool
		err := tx.QueryRow(ctx, `SELECT d.workspace_id::text, (d.claim = $3 OR COALESCE(d.token = ANY($4::text[]), false))
			FROM help_center_domains d JOIN help_center_domains mine ON lower(mine.hostname) = lower(d.hostname)
			WHERE mine.workspace_id=$1 AND mine.claim=$2 AND d.status='active' AND d.workspace_id <> $1 FOR UPDATE OF d`,
			workspaceID, claim, proof.Claim, proof.Tokens).Scan(&holder, &holderProves)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The holder went back to pending meanwhile: activate as usual (a unique violation means
			// another claim was faster, and the caller tries again).
			holder = ""
		case err != nil:
			return err
		case holderProves:
			return domain.ErrHelpCenterDomainTaken
		default:
			if _, err := tx.Exec(ctx, `UPDATE help_center_domains SET status='pending', pending_reason='taken_over', failing_since=NULL,
				checked_at=$2, check_error=$3 WHERE workspace_id=$1`, holder, now, takenOverMessage); err != nil {
				return err
			}
		}
		got, err = scanHelpCenterDomain(tx.QueryRow(ctx, `UPDATE help_center_domains SET `+activateColumns+`
			WHERE workspace_id=$1 AND claim=$2 RETURNING `+helpCenterDomainColumns, workspaceID, claim, now, certificate))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrHelpCenterDomainMissing
		}
		return err
	})
	return got, holder, err
}

// RecordHelpCenterDomainFailure records a DNS check that did not prove the claim, with message saying
// which record to set. A pending domain keeps its status. An active one keeps serving and notes when
// its checks started failing; once they have failed for domain.HelpCenterDomainRelease, it goes back to
// pending (proof_lost). ErrHelpCenterDomainMissing means the claim was removed or replaced meanwhile.
func (s *Store) RecordHelpCenterDomainFailure(ctx context.Context, workspaceID, claim string, now time.Time, message string) (domain.HelpCenterDomain, error) {
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterDomain, error) {
		return scanHelpCenterDomain(tx.QueryRow(ctx, `UPDATE help_center_domains SET checked_at=$3, check_error=$4,
				status = CASE WHEN status='active' AND COALESCE(failing_since, $3) <= $5 THEN 'pending' ELSE status END,
				pending_reason = CASE WHEN status='active' AND COALESCE(failing_since, $3) <= $5 THEN 'proof_lost' ELSE pending_reason END,
				failing_since = CASE WHEN status='active' AND COALESCE(failing_since, $3) > $5 THEN COALESCE(failing_since, $3) END
			WHERE workspace_id=$1 AND claim=$2 RETURNING `+helpCenterDomainColumns,
			workspaceID, claim, now, clipCheckError(message), now.Add(-domain.HelpCenterDomainRelease)))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HelpCenterDomain{}, domain.ErrHelpCenterDomainMissing
	}
	if err != nil {
		return domain.HelpCenterDomain{}, fmt.Errorf("record help center domain check: %w", err)
	}
	return got, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func clipCheckError(message string) string {
	runes := []rune(message)
	if len(runes) <= domain.MaxHelpCenterDomainCheckError {
		return message
	}
	return string(runes[:domain.MaxHelpCenterDomainCheckError-1]) + "…"
}

// NoteHelpCenterCertificate records that the reverse proxy was told it may get a certificate for the
// workspace's active custom domain host, at most once an hour: the question is public, and asking it
// over and over must not write the row each time.
func (s *Store) NoteHelpCenterCertificate(ctx context.Context, workspaceID, host string, now time.Time) error {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE help_center_domains SET certificate_requested_at=$3 WHERE workspace_id=$1 AND hostname=$2 AND status='active'
			AND (certificate_requested_at IS NULL OR certificate_requested_at <= $3::timestamptz - interval '1 hour')`, workspaceID, host, now)
		return err
	})
	if err != nil {
		return fmt.Errorf("note help center certificate: %w", err)
	}
	return nil
}

// DeleteHelpCenterDomain removes the workspace's custom domain and returns it; removing none changes
// nothing (found is false).
func (s *Store) DeleteHelpCenterDomain(ctx context.Context, workspaceID string) (removed domain.HelpCenterDomain, found bool, err error) {
	removed, err = scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.HelpCenterDomain, error) {
		return scanHelpCenterDomain(tx.QueryRow(ctx, `DELETE FROM help_center_domains WHERE workspace_id=$1 RETURNING `+helpCenterDomainColumns, workspaceID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HelpCenterDomain{}, false, nil
	}
	if err != nil {
		return domain.HelpCenterDomain{}, false, fmt.Errorf("delete help center domain: %w", err)
	}
	return removed, true, nil
}

// HelpCenterHost finds the workspace whose help center serves on host, an active custom domain; found
// is false for any other name. It crosses workspaces, since a request's host name is all there is;
// only the workspace id leaves the transaction.
func (s *Store) HelpCenterHost(ctx context.Context, host string) (string, bool, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterHost, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM help_center_domains WHERE lower(hostname)=lower($1) AND status='active'`, host).Scan(&workspaceID)
	}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
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
	}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find help center claim: %w", err)
	}
	return workspaceID, true, nil
}

// HelpCenterHostPending reports whether any workspace has a pending claim of host, so the certificate
// question asks DNS only for names someone claimed. Only the yes or no leaves the transaction.
func (s *Store) HelpCenterHostPending(ctx context.Context, host string) (bool, error) {
	var pending bool
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterHost, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM help_center_domains WHERE lower(hostname)=lower($1) AND status='pending')`, host).Scan(&pending)
	}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return false, fmt.Errorf("find help center host claims: %w", err)
	}
	return pending, nil
}

// HelpCenterHostProvenClaims lists the workspaces, oldest claim first, whose pending claim of host the
// proof proves: its CNAME target or its token. However many workspaces claim a name, only the ones
// DNS proves are returned, so no claim can crowd out another. The claims and tokens are compared in
// the database; only workspace ids leave the transaction.
func (s *Store) HelpCenterHostProvenClaims(ctx context.Context, host string, proof domain.HelpCenterDomainProof) ([]string, error) {
	var claims []string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterHost, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text FROM help_center_domains WHERE lower(hostname)=lower($1) AND status='pending'
			AND (claim = $2 OR token = ANY($3::text[])) ORDER BY created_at, workspace_id`, host, proof.Claim, proof.Tokens)
		if err != nil {
			return err
		}
		claims, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
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
