package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CompSubscriptionPrefix starts the subscription id of a complimentary plan, so it is never
// mistaken for a Polar subscription id.
const CompSubscriptionPrefix = "comp:"

// ErrNoCompPlan means the workspace has no complimentary plan in force.
var ErrNoCompPlan = errors.New("the workspace has no complimentary plan in force")

// compSubscriptionColumns reads a complimentary plan in the shape of subscriptionColumns: forever
// reads as active, one with an end reads as canceled and paid until that end.
const compSubscriptionColumns = `plan,'` + CompSubscriptionPrefix + `' || id::text,'',
	CASE WHEN until IS NULL THEN 'active' ELSE 'canceled' END,until,false,NULL::timestamptz,granted_at`

// GrantCompPlan puts the workspace on a complimentary paid plan until the given instant (nil means
// forever), replacing a plan already in force. The replaced and the new grant stay as rows, with
// who granted them and when, so the history is auditable.
func (s *Store) GrantCompPlan(ctx context.Context, workspaceID, plan, by string, until *time.Time, now time.Time) error {
	if by == "" {
		return errors.New("a complimentary plan needs the name of who grants it")
	}
	if until != nil && !until.After(now) {
		return errors.New("the end of a complimentary plan must be in the future")
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if _, err := tx.Exec(ctx, `UPDATE workspace_comp_plans SET revoked_at=$2, revoked_by=$3
			WHERE workspace_id=$1 AND revoked_at IS NULL`, workspaceID, now, by); err != nil {
			return struct{}{}, err
		}
		_, err := tx.Exec(ctx, `INSERT INTO workspace_comp_plans(workspace_id,plan,granted_by,granted_at,until)
			VALUES($1,$2,$3,$4,$5)`, workspaceID, plan, by, now, until)
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("grant complimentary plan: %w", err)
	}
	return nil
}

// RevokeCompPlan ends the complimentary plan in force; ErrNoCompPlan when there is none.
func (s *Store) RevokeCompPlan(ctx context.Context, workspaceID, by string, now time.Time) error {
	if by == "" {
		return errors.New("revoking a complimentary plan needs the name of who does it")
	}
	revoked, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (int64, error) {
		tag, err := tx.Exec(ctx, `UPDATE workspace_comp_plans SET revoked_at=$2, revoked_by=$3
			WHERE workspace_id=$1 AND revoked_at IS NULL`, workspaceID, now, by)
		return tag.RowsAffected(), err
	})
	if err != nil {
		return fmt.Errorf("revoke complimentary plan: %w", err)
	}
	if revoked == 0 {
		return ErrNoCompPlan
	}
	return nil
}
