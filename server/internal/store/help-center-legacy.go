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

// HelpCenterLegacyItem is one imported Freshdesk knowledge base item of an account and the public page
// it became: an article (Number and Slug) or a collection (CollectionID).
type HelpCenterLegacyItem struct {
	Entity, SourceID string
	Number           int64
	Slug             string
	CollectionID     string
}

// HelpCenterLegacyItems lists the imported knowledge base items of one Freshdesk account (the ledger
// source, freshdesk:account:<id>) that are public now: a published article outside the trash, or a
// collection outside the trash (the caller decides whether it shows anything). Only done rows count.
func (s *Store) HelpCenterLegacyItems(ctx context.Context, workspaceID, accountSource string) ([]HelpCenterLegacyItem, error) {
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]HelpCenterLegacyItem, error) {
		rows, err := tx.Query(ctx, `SELECT i.entity, i.source_id, a.number, a.slug, c.id::text FROM import_items i
			LEFT JOIN kb_articles a ON i.entity='kb_article' AND a.workspace_id=i.workspace_id AND a.id=i.target_id AND a.status='published' AND a.deleted_at IS NULL
			LEFT JOIN kb_collections c ON i.entity IN ('kb_category','kb_folder') AND c.workspace_id=i.workspace_id AND c.id=i.target_id AND c.deleted_at IS NULL
			WHERE i.workspace_id=$1 AND i.source=$2 AND i.source LIKE 'freshdesk:%' AND i.status='done'
				AND i.entity IN ('kb_article','kb_category','kb_folder') AND (a.id IS NOT NULL OR c.id IS NOT NULL)
			ORDER BY i.entity, length(i.source_id), i.source_id`, workspaceID, accountSource)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (HelpCenterLegacyItem, error) {
			var item HelpCenterLegacyItem
			var number *int64
			var slug, collection *string
			if err := row.Scan(&item.Entity, &item.SourceID, &number, &slug, &collection); err != nil {
				return item, err
			}
			if number != nil && slug != nil {
				item.Number, item.Slug = *number, *slug
			}
			if collection != nil {
				item.CollectionID = *collection
			}
			return item, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list imported help center items: %w", err)
	}
	return items, nil
}
