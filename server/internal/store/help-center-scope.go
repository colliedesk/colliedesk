package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// noHelpCenter is the help center a workspace without one is read as: no row belongs to it, so the
// reader sees nothing and every public read answers as for an article that does not exist.
const noHelpCenter = "00000000-0000-0000-0000-000000000000"

// helpCenterScoped runs fn in one transaction as the public reader of help center helpCenterID of the
// workspace ("" its primary one) and returns its result. Row-level security limits every statement of
// fn to the rows that help center shows, and the reader has no grant on the inbox, contacts or
// channels. fn gets the id resolved, so its own $H filters never look up the primary help center as
// the reader, who can read only its own row.
func helpCenterScoped[T any](ctx context.Context, s *Store, workspaceID, helpCenterID string, fn func(tx pgx.Tx, helpCenterID string) (T, error)) (T, error) {
	var zero T
	if helpCenterID == "" {
		primary, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*string, error) {
			var id *string
			err := tx.QueryRow(ctx, `SELECT primary_help_center_id($1)::text`, workspaceID).Scan(&id)
			return id, err
		})
		if err != nil {
			return zero, err
		}
		helpCenterID = noHelpCenter
		if primary != nil {
			helpCenterID = *primary
		}
	}
	var result T
	err := s.db.InHelpCenter(ctx, workspaceID, helpCenterID, func(tx pgx.Tx) error {
		var err error
		result, err = fn(tx, helpCenterID)
		return err
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}
