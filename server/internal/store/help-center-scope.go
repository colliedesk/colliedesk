package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// noHelpCenter is the help center a workspace without one is read as: no row belongs to it, so the
// reader sees nothing and every public read answers as for an article that does not exist.
const noHelpCenter = "00000000-0000-0000-0000-000000000000"

// genericPlan makes the statements after it in tx run on the one plan Postgres keeps for each of them
// on the connection. Otherwise the first five runs of a statement on a connection are each planned
// for their values, and planning the row security of a big help center read costs about a third of
// running it, on the pages served right after an edit, when nothing is cached. A plan made for the
// values also goes wrong for a workspace the table statistics do not know yet, a new one: it
// expects a single row and repeats the row security checks for every article. Only a statement
// whose best plan does not depend on its values may take it.
func genericPlan(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SET LOCAL plan_cache_mode = force_generic_plan`)
	return err
}

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
