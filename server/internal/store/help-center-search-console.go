package store

import (
	"context"
	"errors"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// GetHelpCenterSearchConsole returns the verification tokens of the primary help center
// (GetHelpCenterSearchConsoleIn).
func (s *Store) GetHelpCenterSearchConsole(ctx context.Context, workspaceID string) (domain.SearchConsoleView, error) {
	return s.GetHelpCenterSearchConsoleIn(ctx, workspaceID, "")
}

// GetHelpCenterSearchConsoleIn returns the verification tokens of help center helpCenterID ("" the
// primary one); a workspace without a help center row has none.
func (s *Store) GetHelpCenterSearchConsoleIn(ctx context.Context, workspaceID, helpCenterID string) (domain.SearchConsoleView, error) {
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SearchConsoleView, error) {
		target, err := resolveHelpCenter(ctx, tx, workspaceID, helpCenterID, false)
		if err != nil {
			return domain.SearchConsoleView{}, err
		}
		return readHelpCenterSearchConsole(ctx, tx, workspaceID, target)
	})
	return view, helpCenterStoreError("get help center search console", err)
}

// helpCenterContentVersion is the knowledge base's content version, kept on the primary row of
// workspace $1, for a query on any of its help centers.
const helpCenterContentVersion = `(SELECT p.content_version FROM help_centers p WHERE p.workspace_id=$1 AND p.primary_help_center)`

func readHelpCenterSearchConsole(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string) (domain.SearchConsoleView, error) {
	var view domain.SearchConsoleView
	err := tx.QueryRow(ctx, `SELECT google_site_verification,bing_site_verification,`+helpCenterContentVersion+`,updated_at
		FROM help_centers WHERE workspace_id=$1 AND id=NULLIF($2,'')::uuid`, workspaceID, helpCenterID).
		Scan(&view.Google, &view.Bing, &view.ContentVersion, &view.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.SearchConsoleView{}, err
	}
	view.UpdatedAt = domain.UTCOptional(view.UpdatedAt)
	return view, nil
}

// PutHelpCenterSearchConsole changes the verification tokens of the primary help center
// (PutHelpCenterSearchConsoleIn).
func (s *Store) PutHelpCenterSearchConsole(ctx context.Context, workspaceID string, change domain.SearchConsoleChange) (domain.SearchConsoleView, error) {
	return s.PutHelpCenterSearchConsoleIn(ctx, workspaceID, "", change)
}

// PutHelpCenterSearchConsoleIn changes the verification tokens of help center helpCenterID ("" the
// primary one): a field left out (nil) stays, "" removes it. A save bumps content_version, since every
// page carries the tags.
func (s *Store) PutHelpCenterSearchConsoleIn(ctx context.Context, workspaceID, helpCenterID string, change domain.SearchConsoleChange) (domain.SearchConsoleView, error) {
	if err := domain.CheckSearchConsole(change); err != nil {
		return domain.SearchConsoleView{}, err
	}
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SearchConsoleView, error) {
		target, err := resolveHelpCenter(ctx, tx, workspaceID, helpCenterID, true)
		if err != nil {
			return domain.SearchConsoleView{}, err
		}
		if err := writeSearchConsole(ctx, tx, workspaceID, target, change); err != nil {
			return domain.SearchConsoleView{}, err
		}
		return readHelpCenterSearchConsole(ctx, tx, workspaceID, target)
	})
	return view, helpCenterStoreError("put help center search console", err)
}

// writeSearchConsole applies change to help center helpCenterID of the workspace and bumps the
// knowledge base's content version.
func writeSearchConsole(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string, change domain.SearchConsoleChange) error {
	if _, err := tx.Exec(ctx, `UPDATE help_centers SET
			google_site_verification=CASE WHEN $3 THEN NULLIF($4,'') ELSE google_site_verification END,
			bing_site_verification=CASE WHEN $5 THEN NULLIF($6,'') ELSE bing_site_verification END,
			updated_at=clock_timestamp()
		WHERE workspace_id=$1 AND id=$2`,
		workspaceID, helpCenterID, change.Google != nil, deref(change.Google), change.Bing != nil, deref(change.Bing)); err != nil {
		return err
	}
	return bumpKB(ctx, tx, workspaceID)
}

func deref(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

// CarryOverSearchConsole sets the tokens the workspace has none of yet, in tx, and reports which it set.
// A token already set is never replaced. A change bumps content_version once, since the pages change;
// without one nothing is written.
func CarryOverSearchConsole(ctx context.Context, tx pgx.Tx, workspaceID string, google, bing string) (setGoogle, setBing bool, err error) {
	var current domain.SearchConsoleView
	err = tx.QueryRow(ctx, `SELECT google_site_verification,bing_site_verification FROM help_centers WHERE workspace_id=$1 AND primary_help_center FOR UPDATE`, workspaceID).
		Scan(&current.Google, &current.Bing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, false, err
	}
	var change domain.SearchConsoleChange
	if google != "" && current.Google == nil {
		change.Google, setGoogle = &google, true
	}
	if bing != "" && current.Bing == nil {
		change.Bing, setBing = &bing, true
	}
	if !setGoogle && !setBing {
		return false, false, nil
	}
	primary, err := resolveHelpCenter(ctx, tx, workspaceID, "", true)
	if err != nil {
		return false, false, err
	}
	return setGoogle, setBing, writeSearchConsole(ctx, tx, workspaceID, primary, change)
}
