package store

import (
	"context"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// readHelpCenterHomeOrder reads the live top-level collections shown on help center helpCenterID in
// its home order and locks their placements FOR NO KEY UPDATE.
func readHelpCenterHomeOrder(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT c.id::text FROM kb_collection_help_centers p
		JOIN kb_collections c ON c.workspace_id=p.workspace_id AND c.id=p.collection_id
		WHERE p.workspace_id=$1 AND p.help_center_id=$2 AND c.parent_id IS NULL AND c.deleted_at IS NULL
		ORDER BY p.position, lower(c.name), c.id FOR NO KEY UPDATE OF p`, workspaceID, helpCenterID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// setHelpCenterHomeOrder is SetKBOrder for the home of one help center: order.CollectionIDs must name
// exactly the live top-level collections shown on it, and get the positions 1, 2, 3 there. The other
// help centers keep their order. On the primary the collections' own positions follow, as they did
// before a workspace could have several help centers.
func setHelpCenterHomeOrder(ctx context.Context, tx pgx.Tx, workspaceID string, order domain.KBOrder) (domain.KBOrder, error) {
	if err := lockKB(ctx, tx, workspaceID); err != nil {
		return domain.KBOrder{}, err
	}
	helpCenterID, err := resolveHelpCenter(ctx, tx, workspaceID, *order.HelpCenterID, false)
	if err != nil {
		return domain.KBOrder{}, err
	}
	order.HelpCenterID = &helpCenterID
	current, err := readHelpCenterHomeOrder(ctx, tx, workspaceID, helpCenterID)
	if err != nil {
		return domain.KBOrder{}, err
	}
	if !sameMembers(current, order.CollectionIDs) {
		return domain.KBOrder{}, &domain.KBOrderStaleError{Current: domain.KBOrder{HelpCenterID: order.HelpCenterID, CollectionIDs: current, ArticleIDs: []string{}}}
	}
	tag, err := tx.Exec(ctx, `UPDATE kb_collection_help_centers p SET position=o.place FROM unnest($3::uuid[]) WITH ORDINALITY o(id, place)
		WHERE p.workspace_id=$1 AND p.help_center_id=$2 AND p.collection_id=o.id AND p.position<>o.place`, workspaceID, helpCenterID, order.CollectionIDs)
	if err != nil {
		return domain.KBOrder{}, err
	}
	moved := tag.RowsAffected()
	var primary bool
	if err := tx.QueryRow(ctx, `SELECT primary_help_center FROM help_centers WHERE workspace_id=$1 AND id=$2`, workspaceID, helpCenterID).Scan(&primary); err != nil {
		return domain.KBOrder{}, err
	}
	if primary {
		tag, err := tx.Exec(ctx, `UPDATE kb_collections c SET position=o.place FROM unnest($2::uuid[]) WITH ORDINALITY o(id, place)
			WHERE c.workspace_id=$1 AND c.id=o.id AND c.parent_id IS NULL AND c.deleted_at IS NULL AND c.position<>o.place`, workspaceID, order.CollectionIDs)
		if err != nil {
			return domain.KBOrder{}, err
		}
		moved += tag.RowsAffected()
	}
	if moved == 0 {
		return order, nil
	}
	return order, bumpKB(ctx, tx, workspaceID)
}
