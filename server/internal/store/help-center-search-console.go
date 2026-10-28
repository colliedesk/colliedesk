package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// GetHelpCenterSearchConsole returns the verification tokens of the help center; a workspace without a
// help center row has none.
func (s *Store) GetHelpCenterSearchConsole(ctx context.Context, workspaceID string) (domain.SearchConsoleView, error) {
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SearchConsoleView, error) {
		return readHelpCenterSearchConsole(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.SearchConsoleView{}, fmt.Errorf("get help center search console: %w", err)
	}
	return view, nil
}

func readHelpCenterSearchConsole(ctx context.Context, tx pgx.Tx, workspaceID string) (domain.SearchConsoleView, error) {
	var view domain.SearchConsoleView
	err := tx.QueryRow(ctx, `SELECT google_site_verification,bing_site_verification,content_version,updated_at FROM help_centers WHERE workspace_id=$1`, workspaceID).
		Scan(&view.Google, &view.Bing, &view.ContentVersion, &view.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.SearchConsoleView{}, err
	}
	view.UpdatedAt = domain.UTCOptional(view.UpdatedAt)
	return view, nil
}

// PutHelpCenterSearchConsole changes the verification tokens: a field left out (nil) stays, "" removes
// it. A save bumps content_version, since every page carries the tags.
func (s *Store) PutHelpCenterSearchConsole(ctx context.Context, workspaceID string, change domain.SearchConsoleChange) (domain.SearchConsoleView, error) {
	if err := domain.CheckSearchConsole(change); err != nil {
		return domain.SearchConsoleView{}, err
	}
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SearchConsoleView, error) {
		if err := writeSearchConsole(ctx, tx, workspaceID, change); err != nil {
			return domain.SearchConsoleView{}, err
		}
		return readHelpCenterSearchConsole(ctx, tx, workspaceID)
	})
	if err != nil {
		return domain.SearchConsoleView{}, fmt.Errorf("put help center search console: %w", err)
	}
	return view, nil
}

// writeSearchConsole applies change to the workspace's help center row, creating it (off) when the
// workspace has none yet.
func writeSearchConsole(ctx context.Context, tx pgx.Tx, workspaceID string, change domain.SearchConsoleChange) error {
	_, err := tx.Exec(ctx, `INSERT INTO help_centers(workspace_id,content_version,updated_at,google_site_verification,bing_site_verification)
		VALUES($1,1,clock_timestamp(),NULLIF($3,''),NULLIF($5,''))
		ON CONFLICT (workspace_id) DO UPDATE SET
			google_site_verification=CASE WHEN $2 THEN NULLIF($3,'') ELSE help_centers.google_site_verification END,
			bing_site_verification=CASE WHEN $4 THEN NULLIF($5,'') ELSE help_centers.bing_site_verification END,
			content_version=help_centers.content_version+1, updated_at=clock_timestamp()`,
		workspaceID, change.Google != nil, deref(change.Google), change.Bing != nil, deref(change.Bing))
	return err
}

func deref(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}
