package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// RestrictImportedKBCollection sets a collection's visibility when that is more restrictive than its
// own now, and reports whether it did. It never shows a collection to more people, so a rerun of an
// import can follow the source closing a folder and never undoes a restriction made in Colliedesk.
func RestrictImportedKBCollection(ctx context.Context, tx pgx.Tx, workspaceID, collectionID, visibility string) (bool, error) {
	if err := domain.CheckKBVisibility(visibility); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE kb_collections SET visibility=$3, updated_at=clock_timestamp()
		WHERE workspace_id=$1 AND id=$2 AND kb_visibility_rank(visibility) < kb_visibility_rank($3)`, workspaceID, collectionID, visibility)
	if err != nil {
		return false, fmt.Errorf("restrict knowledge base collection: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	return true, bumpKB(ctx, tx, workspaceID)
}
