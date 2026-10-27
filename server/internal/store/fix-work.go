package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// DueFix is a fix the Autofix worker leased: its workspace, id and connector, nothing else.
type DueFix struct {
	WorkspaceID string
	ID          string
	Connector   autofix.Connector
}

// ClaimDueFixes leases up to limit fixes in progress of every workspace whose next check is due (and
// fixes cancelled with their repository disconnected, whose Cursor run the worker stops), for
// the given connectors, fixes still to send first (a run already sent can wait for the call budget; a
// fix not sent yet should not wait behind them). The lease is next_check_at moved lease ahead, so another process skips them and a
// process that dies mid-check leaves them due again after the lease. Only ids leave the privileged
// transaction; each fix is then read and moved in its own workspace.
func (s *Store) ClaimDueFixes(ctx context.Context, now time.Time, connectors []autofix.Connector, limit int, lease time.Duration) ([]DueFix, error) {
	kinds := make([]string, 0, len(connectors))
	for _, connector := range connectors {
		kinds = append(kinds, string(connector))
	}
	var due []DueFix
	err := s.db.Privileged(ctx, database.PrivilegeAutofixClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
				SELECT id FROM fix_attempts
				WHERE next_check_at IS NOT NULL AND next_check_at <= $1 AND connector = ANY($2)
					AND (state IN ('sending','sent','working','pr_open') OR (state = 'cancelled' AND outcome_reason = 'repo_disconnected'))
				ORDER BY state <> 'sending', next_check_at LIMIT $3 FOR UPDATE SKIP LOCKED)
			UPDATE fix_attempts f SET next_check_at = $1 + $4::interval FROM due WHERE f.id = due.id
			RETURNING f.workspace_id::text, f.id::text, f.connector`, now, kinds, limit, lease.String())
		if err != nil {
			return fmt.Errorf("claim due fixes: %w", err)
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DueFix, error) {
			var item DueFix
			return item, row.Scan(&item.WorkspaceID, &item.ID, &item.Connector)
		})
		return err
	})
	return due, err
}

// FixWork is what the worker needs of a fix beyond what the API shows: what it sends, the vendor's ids,
// the repository's branch and how many checks failed in a row.
type FixWork struct {
	autofix.Fix
	SentBody      string
	VendorRef     string
	VendorRunID   string
	DefaultBranch string
	// RepoName is the connected repository's name now (it may have been renamed since the fix was sent).
	RepoName          string
	CheckFailures     int
	DispatchStartedAt *time.Time
}

// GetFixWork reads a fix for the worker.
func (s *Store) GetFixWork(ctx context.Context, workspaceID, id string) (FixWork, error) {
	if !savedReplyID.MatchString(id) {
		return FixWork{}, autofix.ErrFixNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (FixWork, error) {
		fix, err := getFix(ctx, tx, workspaceID, id, false)
		if err != nil {
			return FixWork{}, err
		}
		work := FixWork{Fix: fix}
		var vendorRef, vendorRunID, branch, repoName *string
		if err := tx.QueryRow(ctx, `SELECT f.sent_body, f.vendor_ref, f.vendor_run_id, r.default_branch, r.full_name, f.check_failures, f.dispatch_started_at
			FROM fix_attempts f LEFT JOIN autofix_repos r ON r.workspace_id = f.workspace_id AND r.id = f.repo_id
			WHERE f.workspace_id = $1 AND f.id = $2`, workspaceID, id).
			Scan(&work.SentBody, &vendorRef, &vendorRunID, &branch, &repoName, &work.CheckFailures, &work.DispatchStartedAt); err != nil {
			return FixWork{}, fmt.Errorf("read the fix's work: %w", err)
		}
		for _, field := range []struct {
			to   *string
			from *string
		}{{&work.VendorRef, vendorRef}, {&work.VendorRunID, vendorRunID}, {&work.DefaultBranch, branch}, {&work.RepoName, repoName}} {
			if field.from != nil {
				*field.to = *field.from
			}
		}
		if work.DispatchStartedAt != nil {
			utc := work.DispatchStartedAt.UTC()
			work.DispatchStartedAt = &utc
		}
		return work, nil
	})
}

// ScheduleFixCheck asks the worker to look at a fix at the given time (now, for a fix just sent to
// Cursor) and clears the count of failed checks: the check before went through.
func (s *Store) ScheduleFixCheck(ctx context.Context, workspaceID, id string, at time.Time) error {
	return s.setFixCheck(ctx, workspaceID, id, `next_check_at = $3, check_failures = 0`, at)
}

// EnsureFixCheck asks the worker to look at a fix at the given time unless a check is already set: a
// send answered again by its key puts back a fix whose first schedule was lost.
func (s *Store) EnsureFixCheck(ctx context.Context, workspaceID, id string, at time.Time) error {
	return s.setFixCheck(ctx, workspaceID, id, `next_check_at = COALESCE(next_check_at, $3)`, at)
}

// PostponeFixCheck moves a fix's next check without touching its failures in a row (waiting for the
// call budget, for Cursor's Retry-After or for a key).
func (s *Store) PostponeFixCheck(ctx context.Context, workspaceID, id string, at time.Time) error {
	return s.setFixCheck(ctx, workspaceID, id, `next_check_at = $3`, at)
}

// RecordFixVendorRef keeps the vendor's ids of a fix the rules did not move (a person cancelled it while
// it was sent), so what was created stays findable; ids already stored stay.
func (s *Store) RecordFixVendorRef(ctx context.Context, workspaceID, id, vendorRef, vendorRunID, vendorURL string) error {
	return s.setFixCheck(ctx, workspaceID, id, `vendor_ref = COALESCE(vendor_ref, $3), vendor_run_id = COALESCE(vendor_run_id, $4),
		vendor_url = COALESCE(vendor_url, $5)`, optionalText(vendorRef), optionalText(vendorRunID), optionalText(vendorURL))
}

// StopFixChecks takes a fix off the worker's list (its vendor run ended; GitHub follows the rest).
func (s *Store) StopFixChecks(ctx context.Context, workspaceID, id string) error {
	return s.setFixCheck(ctx, workspaceID, id, `next_check_at = NULL, check_failures = 0`)
}

// RecordFixCheckFailure counts a check that failed for a passing reason (the vendor or the network) and
// schedules the next one; it returns the failures in a row.
func (s *Store) RecordFixCheckFailure(ctx context.Context, workspaceID, id string, next time.Time) (int, error) {
	if !savedReplyID.MatchString(id) {
		return 0, autofix.ErrFixNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int, error) {
		var failures int
		err := tx.QueryRow(ctx, `UPDATE fix_attempts SET next_check_at = $3, check_failures = check_failures + 1
			WHERE workspace_id = $1 AND id = $2 RETURNING check_failures`, workspaceID, id, next).Scan(&failures)
		if err == pgx.ErrNoRows {
			return 0, autofix.ErrFixNotFound
		}
		return failures, err
	})
}

// MarkFixDispatchStarted records when the fix first started leaving for its vendor; later calls keep
// the first time.
func (s *Store) MarkFixDispatchStarted(ctx context.Context, workspaceID, id string) error {
	return s.setFixCheck(ctx, workspaceID, id, `dispatch_started_at = COALESCE(dispatch_started_at, now())`)
}

func (s *Store) setFixCheck(ctx context.Context, workspaceID, id, set string, args ...any) error {
	if !savedReplyID.MatchString(id) {
		return autofix.ErrFixNotFound
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		tag, err := tx.Exec(ctx, `UPDATE fix_attempts SET `+set+` WHERE workspace_id = $1 AND id = $2`, append([]any{workspaceID, id}, args...)...)
		if err == nil && tag.RowsAffected() == 0 {
			err = autofix.ErrFixNotFound
		}
		return struct{}{}, err
	})
	return err
}

// SendingFixIDs lists the workspace's fixes of a connector still sending: nothing of them left yet.
func (s *Store) SendingFixIDs(ctx context.Context, workspaceID string, connector autofix.Connector) ([]string, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		rows, err := tx.Query(ctx, `SELECT id::text FROM fix_attempts WHERE workspace_id = $1 AND connector = $2 AND state = 'sending' ORDER BY created_at`,
			workspaceID, string(connector))
		if err != nil {
			return nil, fmt.Errorf("list the fixes still sending: %w", err)
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	})
}
