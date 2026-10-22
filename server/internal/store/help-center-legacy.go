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

// HelpCenterLegacyTargetSQL is the lookup of HelpCenterLegacyTarget for one ledger entity ($1 the
// workspace, $2 the Freshdesk id), or "" for an entity that names no knowledge base item. The entity
// is a literal, not a parameter, so even a generic plan can use the partial index
// import_items_source_id_idx, which holds the knowledge base rows only. Exported for the EXPLAIN of
// the performance test.
func HelpCenterLegacyTargetSQL(entity string) string {
	// What an imported item can be now: a published article outside the trash, or a collection
	// outside the trash.
	article, collection := "false", "false"
	switch entity {
	case "kb_article":
		article = "a.workspace_id=i.workspace_id AND a.id=i.target_id AND a.status='published' AND a.deleted_at IS NULL"
	case "kb_category", "kb_folder":
		collection = "c.workspace_id=i.workspace_id AND c.id=i.target_id AND c.deleted_at IS NULL"
	default:
		return ""
	}
	// A public target first, then the row written last: two accounts imported into one workspace may
	// share an id, and a draft of the newer one must not hide the older one's page.
	return `SELECT a.number, a.slug, c.id::text FROM import_items i
		LEFT JOIN kb_articles a ON ` + article + `
		LEFT JOIN kb_collections c ON ` + collection + `
		WHERE i.workspace_id=$1 AND i.entity='` + entity + `' AND i.source_id=$2 AND i.source LIKE 'freshdesk:%' AND i.status='done'
		ORDER BY (a.id IS NOT NULL OR c.id IS NOT NULL) DESC, i.updated_at DESC, i.source LIMIT 1`
}

// HelpCenterLegacyTarget finds the Colliedesk item an imported Freshdesk knowledge base item (ledger
// entity and Freshdesk id) became in the workspace. Freshdesk ids are unique within one account only:
// when the workspace imported several accounts with the same id, a public target wins, then the row
// written last. Only done rows count. It probes import_items_source_id_idx, in the workspace's scope.
func (s *Store) HelpCenterLegacyTarget(ctx context.Context, workspaceID, entity, sourceID string) (HelpCenterLegacyTarget, error) {
	lookup := HelpCenterLegacyTargetSQL(entity)
	if lookup == "" {
		return HelpCenterLegacyTarget{}, nil
	}
	target, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (HelpCenterLegacyTarget, error) {
		var target HelpCenterLegacyTarget
		var number *int64
		var slug, collection *string
		err := tx.QueryRow(ctx, lookup, workspaceID, sourceID).Scan(&number, &slug, &collection)
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
