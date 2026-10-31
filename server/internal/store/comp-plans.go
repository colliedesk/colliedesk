package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is the SQLSTATE of a unique index violation.
const uniqueViolation = "23505"

// ErrNoCompPlan means the workspace has no complimentary plan in force.
var ErrNoCompPlan = errors.New("the workspace has no complimentary plan in force")

// ErrCompGrantConflict means another grant for the same workspace committed at the same moment.
var ErrCompGrantConflict = errors.New("another change of this workspace's complimentary plan ran at the same time; check the result and run the command again")

// compSubscriptionColumns reads a complimentary plan in the shape of subscriptionColumns: forever
// reads as active, one with an end reads as canceled and paid until that end.
const compSubscriptionColumns = `plan,'` + domain.CompSubscriptionPrefix + `' || id::text,'',
	CASE WHEN until IS NULL THEN 'active' ELSE 'canceled' END,until,false,NULL::timestamptz,granted_at,''`

// PreviousGrant describes the grant a new grant found in force.
type PreviousGrant struct {
	GrantedBy string
	Until     *time.Time
	// Expired is true when the grant had already passed its end; it was not replaced, it ran out.
	Expired bool
}

// GrantCompPlan puts the workspace on a complimentary paid plan until the given instant (nil means
// forever). A grant still running is replaced; one past its end is recorded as expired. Both stay as
// rows, with who granted them and when, so the history is auditable. It returns the grant it found
// in force, or nil.
func (s *Store) GrantCompPlan(ctx context.Context, workspaceID, plan, by string, until *time.Time, now time.Time) (*PreviousGrant, error) {
	if by == "" {
		return nil, errors.New("a complimentary plan needs the name of who grants it")
	}
	if until != nil && !until.After(now) {
		return nil, errors.New("the end of a complimentary plan must be in the future")
	}
	previous, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*PreviousGrant, error) {
		previous, err := endCompPlanInForce(ctx, tx, workspaceID, by, "replaced", now)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO workspace_comp_plans(workspace_id,plan,granted_by,granted_at,until)
			VALUES($1,$2,$3,$4,$5)`, workspaceID, plan, by, now, until)
		return previous, err
	})
	var violation *pgconn.PgError
	if errors.As(err, &violation) && violation.Code == uniqueViolation {
		return nil, ErrCompGrantConflict
	}
	if err != nil {
		return nil, fmt.Errorf("grant complimentary plan: %w", err)
	}
	return previous, nil
}

// endCompPlanInForce closes the grant in force: as expired when it ran out before now, else with
// the given reason, revoked by the given operator. It returns nil when there was none.
func endCompPlanInForce(ctx context.Context, tx pgx.Tx, workspaceID, by, reason string, now time.Time) (*PreviousGrant, error) {
	var id string
	previous := &PreviousGrant{}
	err := tx.QueryRow(ctx, `SELECT id::text,granted_by,until FROM workspace_comp_plans
		WHERE workspace_id=$1 AND revoked_at IS NULL FOR UPDATE`, workspaceID).Scan(&id, &previous.GrantedBy, &previous.Until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	endedAt, endedBy := now, by
	if previous.Until != nil && !previous.Until.After(now) {
		previous.Expired = true
		reason, endedAt, endedBy = "expired", *previous.Until, "expiry"
	}
	_, err = tx.Exec(ctx, `UPDATE workspace_comp_plans SET revoked_at=$2,revoked_by=$3,revoke_reason=$4 WHERE id=$1`, id, endedAt, endedBy, reason)
	return previous, err
}

// RevokeCompPlan ends the complimentary plan in force. With none in force it returns ErrNoCompPlan;
// when the only grant had already run out it says so, and records it as expired.
func (s *Store) RevokeCompPlan(ctx context.Context, workspaceID, by string, now time.Time) error {
	if by == "" {
		return errors.New("revoking a complimentary plan needs the name of who does it")
	}
	previous, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*PreviousGrant, error) {
		return endCompPlanInForce(ctx, tx, workspaceID, by, "revoked", now)
	})
	switch {
	case err != nil:
		return fmt.Errorf("revoke complimentary plan: %w", err)
	case previous == nil:
		return ErrNoCompPlan
	case previous.Expired:
		return fmt.Errorf("%w: the last grant (by %s) expired on %s", ErrNoCompPlan, previous.GrantedBy, previous.Until.UTC().Format(time.RFC3339))
	}
	return nil
}
