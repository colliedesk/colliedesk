package freshdesk

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// undoKBHelpCenters moves to the trash the help centers the import made from Freshdesk portals, once
// its collections are gone; a collection that stays goes back to the primary help center. The primary
// and the help centers the team made are never in the ledger as created. A help center the team gave
// a custom domain since stays, and is named in KeptKBHelpCenters.
func undoKBHelpCenters(ctx context.Context, tx pgx.Tx, workspaceID, source string, result *UndoResult) error {
	rows, err := tx.Query(ctx, `SELECT h.id::text, h.name FROM import_items i
		JOIN help_centers h ON h.workspace_id=i.workspace_id AND h.id=i.target_id AND h.deleted_at IS NULL AND NOT h.primary_help_center
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity=$3 AND (i.meta->>'created')::boolean
		ORDER BY h.name, h.id`, workspaceID, source, entityKBHelpCenter)
	if err != nil {
		return fmt.Errorf("find imported help centers: %w", err)
	}
	type helpCenter struct{ id, name string }
	centers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (helpCenter, error) {
		var center helpCenter
		return center, row.Scan(&center.id, &center.name)
	})
	if err != nil {
		return fmt.Errorf("find imported help centers: %w", err)
	}
	result.KeptKBHelpCenters = []string{}
	for _, center := range centers {
		trashed, err := store.TrashImportedHelpCenter(ctx, tx, workspaceID, center.id)
		if err != nil {
			return fmt.Errorf("undo help center %s: %w", center.name, err)
		}
		if trashed {
			result.KBHelpCenters++
		} else {
			result.KeptKBHelpCenters = append(result.KeptKBHelpCenters, center.name)
		}
	}
	return nil
}
