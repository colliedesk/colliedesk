package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// HelpCenterLegacyTarget is where an old Freshdesk help center address of an imported item leads now:
// a published article outside the trash (Number and Slug), or a collection outside the trash
// (CollectionID; the caller decides whether it shows anything). Both are empty when the item was never
// imported here or leads to neither.
type HelpCenterLegacyTarget struct {
	Number       int64
	Slug         string
	CollectionID string
}

// HelpCenterLegacyTarget finds the Colliedesk item an imported Freshdesk knowledge base item (ledger
// entity and Freshdesk id) became in the workspace. Freshdesk ids are unique within one account only:
// when the workspace imported several accounts with the same id, the row written last wins. Only done
// rows count. It is one probe of import_items_source_id_idx, in the workspace's scope.
func (s *Store) HelpCenterLegacyTarget(ctx context.Context, workspaceID, entity, sourceID string) (HelpCenterLegacyTarget, error) {
	target, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (HelpCenterLegacyTarget, error) {
		var target HelpCenterLegacyTarget
		var number *int64
		var slug, collection *string
		err := tx.QueryRow(ctx, `SELECT a.number, a.slug, c.id::text FROM (
				SELECT i.entity, i.target_id FROM import_items i
				WHERE i.workspace_id=$1 AND i.entity=$2 AND i.source_id=$3 AND i.source LIKE 'freshdesk:%' AND i.status='done'
				ORDER BY i.updated_at DESC, i.source LIMIT 1) i
			LEFT JOIN kb_articles a ON i.entity='kb_article' AND a.workspace_id=$1 AND a.id=i.target_id AND a.status='published' AND a.deleted_at IS NULL
			LEFT JOIN kb_collections c ON i.entity IN ('kb_category','kb_folder') AND c.workspace_id=$1 AND c.id=i.target_id AND c.deleted_at IS NULL`,
			workspaceID, entity, sourceID).Scan(&number, &slug, &collection)
		if errors.Is(err, pgx.ErrNoRows) {
			return target, nil
		}
		switch {
		case err != nil:
			return target, err
		case number != nil && slug != nil:
			target.Number, target.Slug = *number, *slug
		case collection != nil:
			target.CollectionID = *collection
		}
		return target, nil
	})
	if err != nil {
		return HelpCenterLegacyTarget{}, fmt.Errorf("find old help center address: %w", err)
	}
	return target, nil
}
