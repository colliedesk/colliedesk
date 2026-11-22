package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// A top-level collection is shown on the help centers it is placed on (kb_collection_help_centers);
// a nested one on those of its top-level collection. A new top-level collection is placed on the
// primary by a trigger, and a collection that moves under another one loses its own placements.

// kbCollectionHelpCenters lists the live help centers collection c is shown on, the primary first,
// as GET /api/v1/help-centers lists them.
const kbCollectionHelpCenters = `COALESCE((SELECT array_agg(p.help_center_id::text ORDER BY h.primary_help_center DESC, h.handle)
		FROM kb_collection_help_centers p JOIN help_centers h ON h.workspace_id=p.workspace_id AND h.id=p.help_center_id AND h.deleted_at IS NULL
		WHERE p.workspace_id=c.workspace_id AND p.collection_id=c.top_collection_id),'{}')`

// cleanKBHelpCenterIDs checks the shape of a list of help center ids: ids, each once, at least one
// and at most domain.MaxHelpCenters. They come back in lowercase, as the database prints them.
func cleanKBHelpCenterIDs(ids *[]string) error {
	if ids == nil {
		return nil
	}
	if len(*ids) == 0 || len(*ids) > domain.MaxHelpCenters {
		return &domain.InvalidKBError{Message: fmt.Sprintf(
			"help_center_ids names 1 to %d help centers to show the collection on; GET /api/v1/help-centers lists them.", domain.MaxHelpCenters)}
	}
	seen := make(map[string]bool, len(*ids))
	cleaned := make([]string, 0, len(*ids))
	for _, id := range *ids {
		if !savedReplyID.MatchString(id) {
			return &domain.InvalidKBError{Message: fmt.Sprintf("%q in help_center_ids is not a help center id; GET /api/v1/help-centers lists them.", id)}
		}
		id = strings.ToLower(id)
		if seen[id] {
			return &domain.InvalidKBError{Message: fmt.Sprintf("%s is in help_center_ids twice.", id)}
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	*ids = cleaned
	return nil
}

// placeKBCollection shows top-level collection id on exactly the live help centers ids names, and on
// no other live one. A collection under another one is refused: it follows its top-level collection.
// A placement it keeps keeps its position in that help center's home order; a new one takes the
// collection's own position.
func placeKBCollection(ctx context.Context, tx pgx.Tx, workspaceID, id string, ids []string) error {
	var topLevel bool
	if err := tx.QueryRow(ctx, `SELECT parent_id IS NULL FROM kb_collections WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&topLevel); err != nil {
		return err
	}
	if !topLevel {
		return &domain.InvalidKBError{Message: "help_center_ids is set on a top-level collection only; a nested collection is shown wherever its top-level collection is."}
	}
	var live int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM help_centers WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND deleted_at IS NULL`,
		workspaceID, ids).Scan(&live); err != nil {
		return err
	}
	if live != len(ids) {
		return &domain.InvalidKBError{Message: "help_center_ids names a help center this workspace does not have; GET /api/v1/help-centers lists them."}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM kb_collection_help_centers p USING help_centers h
		WHERE p.workspace_id=$1 AND p.collection_id=$2 AND h.workspace_id=p.workspace_id AND h.id=p.help_center_id
			AND h.deleted_at IS NULL AND NOT p.help_center_id=ANY($3::uuid[])`, workspaceID, id, ids); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO kb_collection_help_centers(workspace_id,collection_id,help_center_id,position)
		SELECT c.workspace_id, c.id, h, c.position FROM kb_collections c, unnest($3::uuid[]) h WHERE c.workspace_id=$1 AND c.id=$2
		ON CONFLICT DO NOTHING`, workspaceID, id, ids)
	return err
}
