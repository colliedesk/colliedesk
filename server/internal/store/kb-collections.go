package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// lockKB serializes the writes of a workspace's knowledge base that count rows or reshape the tree
// (creating, moving, deleting and restoring collections, creating articles), so two of them cannot pass
// a limit or build a loop together.
func lockKB(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('kb|' || $1::text, 0))`, workspaceID); err != nil {
		return fmt.Errorf("lock knowledge base: %w", err)
	}
	return nil
}

// bumpKB grows help_centers.content_version in the transaction of a knowledge base write, creating the
// row (off) on the first write, so a help center page cached under an older version is stale.
func bumpKB(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO help_centers(workspace_id,content_version,updated_at) VALUES($1,1,clock_timestamp())
		ON CONFLICT (workspace_id) DO UPDATE SET content_version=help_centers.content_version+1, updated_at=clock_timestamp()`, workspaceID); err != nil {
		return fmt.Errorf("bump knowledge base version: %w", err)
	}
	return nil
}

func isKBDuplicateSlug(err error) bool {
	var violation *pgconn.PgError
	return errors.As(err, &violation) && violation.Code == uniqueViolation && violation.ConstraintName == "kb_collections_slug_idx"
}

// kbSavepoint runs write in a savepoint, so a duplicate slug does not abort the caller's transaction.
func kbSavepoint[T any](ctx context.Context, tx pgx.Tx, write func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	sub, err := tx.Begin(ctx)
	if err != nil {
		return zero, err
	}
	result, err := write(sub)
	if err != nil {
		_ = sub.Rollback(ctx)
		if isKBDuplicateSlug(err) {
			return zero, domain.ErrKBDuplicateSlug
		}
		return zero, err
	}
	return result, sub.Commit(ctx)
}

const kbCollectionColumns = `c.id::text,c.parent_id::text,c.name,c.slug,c.description,c.position,
	(SELECT count(*) FROM kb_articles a WHERE a.workspace_id=c.workspace_id AND a.collection_id=c.id AND a.deleted_at IS NULL),c.created_at,c.updated_at`

func kbCollectionTargets(collection *domain.KBCollection) []any {
	return []any{&collection.ID, &collection.ParentID, &collection.Name, &collection.Slug, &collection.Description, &collection.Position,
		&collection.ArticleCount, &collection.CreatedAt, &collection.UpdatedAt}
}

func scanKBCollection(row pgx.Row) (domain.KBCollection, error) {
	var collection domain.KBCollection
	err := row.Scan(kbCollectionTargets(&collection)...)
	collection.CreatedAt, collection.UpdatedAt = domain.UTC(collection.CreatedAt), domain.UTC(collection.UpdatedAt)
	return collection, err
}

func readKBCollection(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBCollection, error) {
	return scanKBCollection(tx.QueryRow(ctx, `SELECT `+kbCollectionColumns+` FROM kb_collections c WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, id))
}

// ListKBCollections returns the live collections of the workspace by parent, position and name.
func (s *Store) ListKBCollections(ctx context.Context, workspaceID string) ([]domain.KBCollection, error) {
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBCollection, error) {
		rows, err := tx.Query(ctx, `SELECT `+kbCollectionColumns+` FROM kb_collections c WHERE c.workspace_id=$1 AND c.deleted_at IS NULL
			ORDER BY c.parent_id NULLS FIRST, c.position, lower(c.name), c.id`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBCollection, error) { return scanKBCollection(row) })
	})
	if err != nil {
		return nil, fmt.Errorf("list knowledge base collections: %w", err)
	}
	return items, nil
}

// ListTrashedKBCollections returns the collections in the trash, most recently deleted first. A
// workspace holds at most domain.MaxKBCollections, so one answer holds them all.
func (s *Store) ListTrashedKBCollections(ctx context.Context, workspaceID string) ([]domain.TrashedKBCollection, error) {
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TrashedKBCollection, error) {
		rows, err := tx.Query(ctx, `SELECT `+kbCollectionColumns+`,c.deleted_at,c.deleted_by_agent_id::text,COALESCE(NULLIF(d.display_name,''),d.email)
			FROM kb_collections c LEFT JOIN agents d ON d.workspace_id=c.workspace_id AND d.id=c.deleted_by_agent_id
			WHERE c.workspace_id=$1 AND c.deleted_at IS NOT NULL ORDER BY c.deleted_at DESC, c.id DESC`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TrashedKBCollection, error) {
			var item domain.TrashedKBCollection
			var deleterID, deleterName *string
			if err := row.Scan(append(kbCollectionTargets(&item.KBCollection), &item.DeletedAt, &deleterID, &deleterName)...); err != nil {
				return item, err
			}
			item.CreatedAt, item.UpdatedAt, item.DeletedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt), domain.UTC(item.DeletedAt)
			item.DeletedBy = savedReplyPerson(deleterID, deleterName)
			item.PurgesAt = item.DeletedAt.Add(domain.KBTrashRetention)
			return item, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list knowledge base collection trash: %w", err)
	}
	return items, nil
}

// KBCollectionInput is a new collection; an empty Slug is made from the name.
type KBCollectionInput struct {
	Name, Slug, Description string
	ParentID                *string
	Position                int
}

func cleanKBCollection(input *KBCollectionInput) error {
	name, err := domain.CleanKBText("name", input.Name, domain.MaxKBCollectionName)
	if err != nil {
		return err
	}
	input.Name = name
	if input.Slug == "" {
		input.Slug = domain.Slugify(name, "collection")
	} else if err := domain.CheckKBSlug(input.Slug); err != nil {
		return err
	}
	if err := domain.CheckKBDescription(input.Description); err != nil {
		return err
	}
	return domain.CheckKBPosition(input.Position)
}

// kbDepth is how many levels a live collection sits below the top (1 for a top collection). A
// collection missing or in the trash is invalid as a parent.
func kbDepth(ctx context.Context, tx pgx.Tx, workspaceID, id string) (int, error) {
	if !savedReplyID.MatchString(id) {
		return 0, &domain.InvalidKBError{Message: "The parent collection was not found."}
	}
	var live bool
	if err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM kb_collections WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&live); errors.Is(err, pgx.ErrNoRows) {
		return 0, &domain.InvalidKBError{Message: "The parent collection was not found."}
	} else if err != nil {
		return 0, err
	}
	if !live {
		return 0, domain.ErrKBParentInTrash
	}
	var depth int
	err := tx.QueryRow(ctx, `WITH RECURSIVE up AS (
			SELECT id, parent_id, 1 AS depth FROM kb_collections WHERE workspace_id=$1 AND id=$2
			UNION ALL SELECT c.id, c.parent_id, up.depth+1 FROM kb_collections c JOIN up ON c.workspace_id=$1 AND c.id=up.parent_id WHERE up.depth < 10)
		SELECT max(depth) FROM up`, workspaceID, id).Scan(&depth)
	return depth, err
}

// kbSubtree returns the ids under a collection (itself included) and the height of its live part.
func kbSubtree(ctx context.Context, tx pgx.Tx, workspaceID, id string) (map[string]bool, int, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE down AS (
			SELECT id, 1 AS height, true AS live FROM kb_collections WHERE workspace_id=$1 AND id=$2
			UNION ALL SELECT c.id, down.height+1, down.live AND c.deleted_at IS NULL FROM kb_collections c
				JOIN down ON c.workspace_id=$1 AND c.parent_id=down.id WHERE down.height < 10)
		SELECT id::text, height, live FROM down`, workspaceID, id)
	if err != nil {
		return nil, 0, err
	}
	ids, height := map[string]bool{}, 0
	for rows.Next() {
		var child string
		var level int
		var live bool
		if err := rows.Scan(&child, &level, &live); err != nil {
			return nil, 0, err
		}
		ids[child] = true
		if live {
			height = max(height, level)
		}
	}
	return ids, height, rows.Err()
}

func tooDeep() error {
	return &domain.InvalidKBError{Message: fmt.Sprintf("Collections nest at most %d levels deep.", domain.MaxKBDepth)}
}

// CreateKBCollection adds a collection. A full workspace is domain.ErrKBCollectionLimit (collections in
// the trash count), a taken slug domain.ErrKBDuplicateSlug, a parent too deep an *domain.InvalidKBError.
func (s *Store) CreateKBCollection(ctx context.Context, workspaceID string, actor Actor, input KBCollectionInput) (domain.KBCollection, error) {
	if err := cleanKBCollection(&input); err != nil {
		return domain.KBCollection{}, err
	}
	collection, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBCollection, error) {
		if err := admitKBCollection(ctx, tx, workspaceID, input.ParentID); err != nil {
			return domain.KBCollection{}, err
		}
		collection, err := kbSavepoint(ctx, tx, func(sub pgx.Tx) (domain.KBCollection, error) {
			var id string
			if err := sub.QueryRow(ctx, `INSERT INTO kb_collections(workspace_id,parent_id,name,slug,description,position) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`,
				workspaceID, input.ParentID, input.Name, input.Slug, input.Description, input.Position).Scan(&id); err != nil {
				return domain.KBCollection{}, err
			}
			return readKBCollection(ctx, sub, workspaceID, id)
		})
		if err != nil {
			return domain.KBCollection{}, err
		}
		return collection, bumpKB(ctx, tx, workspaceID)
	})
	return collection, kbCollectionError("create", err)
}

// admitKBCollection takes the knowledge base lock and refuses a full workspace (the trash counts) or a
// parent that is missing, in the trash or too deep for one more level.
func admitKBCollection(ctx context.Context, tx pgx.Tx, workspaceID string, parentID *string) error {
	if err := lockKB(ctx, tx, workspaceID); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_collections WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
		return err
	}
	if count >= domain.MaxKBCollections {
		return domain.ErrKBCollectionLimit
	}
	if parentID != nil {
		depth, err := kbDepth(ctx, tx, workspaceID, *parentID)
		if err != nil {
			return err
		}
		if depth+1 > domain.MaxKBDepth {
			return tooDeep()
		}
	}
	return nil
}

// KBCollectionChange is a partial update; a nil field stays as it is. MoveParent set moves the
// collection under Parent (nil: to the top).
type KBCollectionChange struct {
	Name, Slug, Description *string
	Position                *int
	MoveParent              bool
	Parent                  *string
}

// UpdateKBCollection changes a live collection. seen is the updated_at the client read.
func (s *Store) UpdateKBCollection(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time, change KBCollectionChange) (domain.KBCollection, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBCollection{}, domain.ErrKBCollectionNotFound
	}
	if change.Name != nil {
		name, err := domain.CleanKBText("name", *change.Name, domain.MaxKBCollectionName)
		if err != nil {
			return domain.KBCollection{}, err
		}
		change.Name = &name
	}
	if change.Slug != nil {
		if err := domain.CheckKBSlug(*change.Slug); err != nil {
			return domain.KBCollection{}, err
		}
	}
	if change.Description != nil {
		if err := domain.CheckKBDescription(*change.Description); err != nil {
			return domain.KBCollection{}, err
		}
	}
	if change.Position != nil {
		if err := domain.CheckKBPosition(*change.Position); err != nil {
			return domain.KBCollection{}, err
		}
	}
	collection, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBCollection, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return domain.KBCollection{}, err
		}
		if err := kbLiveAndCurrent(ctx, tx, `kb_collections`, workspaceID, id, seen, domain.ErrKBCollectionNotFound); err != nil {
			return domain.KBCollection{}, err
		}
		if change.MoveParent && change.Parent != nil {
			subtree, height, err := kbSubtree(ctx, tx, workspaceID, id)
			if err != nil {
				return domain.KBCollection{}, err
			}
			if subtree[*change.Parent] {
				return domain.KBCollection{}, &domain.InvalidKBError{Message: "A collection cannot move under itself or one of its own collections."}
			}
			depth, err := kbDepth(ctx, tx, workspaceID, *change.Parent)
			if err != nil {
				return domain.KBCollection{}, err
			}
			if depth+height > domain.MaxKBDepth {
				return domain.KBCollection{}, tooDeep()
			}
		}
		collection, err := kbSavepoint(ctx, tx, func(sub pgx.Tx) (domain.KBCollection, error) {
			if _, err := sub.Exec(ctx, `UPDATE kb_collections SET name=COALESCE($3,name), slug=COALESCE($4,slug), description=COALESCE($5,description),
				position=COALESCE($6,position), parent_id=CASE WHEN $7 THEN $8::uuid ELSE parent_id END, updated_at=clock_timestamp()
				WHERE workspace_id=$1 AND id=$2`, workspaceID, id, change.Name, change.Slug, change.Description, change.Position, change.MoveParent, change.Parent); err != nil {
				return domain.KBCollection{}, err
			}
			return readKBCollection(ctx, sub, workspaceID, id)
		})
		if err != nil {
			return domain.KBCollection{}, err
		}
		return collection, bumpKB(ctx, tx, workspaceID)
	})
	return collection, kbCollectionError("update", err)
}

// kbLiveAndCurrent locks a live row of table and answers notFound when it is missing or in the trash,
// domain.ErrKBChanged when it was saved after seen.
func kbLiveAndCurrent(ctx context.Context, tx pgx.Tx, table, workspaceID, id string, seen time.Time, notFound error) error {
	var updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT updated_at FROM `+table+` WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, workspaceID, id).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}
	if err != nil {
		return err
	}
	if !updatedAt.Equal(seen) {
		return domain.ErrKBChanged
	}
	return nil
}

// kbTrashedAndCurrent is kbLiveAndCurrent for a row in the trash.
func kbTrashedAndCurrent(ctx context.Context, tx pgx.Tx, table, workspaceID, id string, seen time.Time, notFound error) error {
	var updatedAt time.Time
	err := tx.QueryRow(ctx, `SELECT updated_at FROM `+table+` WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NOT NULL FOR UPDATE`, workspaceID, id).Scan(&updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}
	if err != nil {
		return err
	}
	if !updatedAt.Equal(seen) {
		return domain.ErrKBChanged
	}
	return nil
}

// DeleteKBCollection moves an empty collection to the trash: domain.ErrKBCollectionNotEmpty while it
// holds live articles or collections.
func (s *Store) DeleteKBCollection(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) error {
	if !savedReplyID.MatchString(id) {
		return domain.ErrKBCollectionNotFound
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return struct{}{}, err
		}
		if err := kbLiveAndCurrent(ctx, tx, `kb_collections`, workspaceID, id, seen, domain.ErrKBCollectionNotFound); err != nil {
			return struct{}{}, err
		}
		var full bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND collection_id=$2 AND deleted_at IS NULL)
			OR EXISTS(SELECT 1 FROM kb_collections WHERE workspace_id=$1 AND parent_id=$2 AND deleted_at IS NULL)`, workspaceID, id).Scan(&full); err != nil {
			return struct{}{}, err
		}
		if full {
			return struct{}{}, domain.ErrKBCollectionNotEmpty
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_collections SET deleted_at=$3, deleted_by_agent_id=$4, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, s.now(), optionalID(actor.AgentID)); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, bumpKB(ctx, tx, workspaceID)
	})
	return kbCollectionError("delete", err)
}

// UndeleteKBCollection takes a collection out of the trash: domain.ErrKBParentInTrash while its parent
// is in the trash, domain.ErrKBDuplicateSlug when a sibling took its slug.
func (s *Store) UndeleteKBCollection(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) (domain.KBCollection, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBCollection{}, domain.ErrKBCollectionNotFound
	}
	collection, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBCollection, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return domain.KBCollection{}, err
		}
		if err := kbTrashedAndCurrent(ctx, tx, `kb_collections`, workspaceID, id, seen, domain.ErrKBCollectionNotFound); err != nil {
			return domain.KBCollection{}, err
		}
		var parent *string
		if err := tx.QueryRow(ctx, `SELECT parent_id::text FROM kb_collections WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&parent); err != nil {
			return domain.KBCollection{}, err
		}
		if parent != nil {
			depth, err := kbDepth(ctx, tx, workspaceID, *parent)
			if err != nil {
				return domain.KBCollection{}, err
			}
			if _, height, err := kbSubtree(ctx, tx, workspaceID, id); err != nil {
				return domain.KBCollection{}, err
			} else if depth+height > domain.MaxKBDepth {
				return domain.KBCollection{}, tooDeep()
			}
		}
		collection, err := kbSavepoint(ctx, tx, func(sub pgx.Tx) (domain.KBCollection, error) {
			if _, err := sub.Exec(ctx, `UPDATE kb_collections SET deleted_at=NULL, deleted_by_agent_id=NULL, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`,
				workspaceID, id); err != nil {
				return domain.KBCollection{}, err
			}
			return readKBCollection(ctx, sub, workspaceID, id)
		})
		if err != nil {
			return domain.KBCollection{}, err
		}
		return collection, bumpKB(ctx, tx, workspaceID)
	})
	return collection, kbCollectionError("restore", err)
}

// kbErrors are the refusals the API maps; anything else is wrapped as a store failure.
var kbErrors = []error{domain.ErrKBArticleNotFound, domain.ErrKBArticleVersionNotFound, domain.ErrKBCollectionNotFound, domain.ErrKBImageNotFound, domain.ErrKBMediaNotFound, domain.ErrKBMediaBatchNotFound, domain.ErrKBMediaUploadNotFound, domain.ErrKBMediaUploadLimit, domain.ErrKBMediaUploadIncomplete,
	domain.ErrKBChanged, domain.ErrKBDuplicateSlug, domain.ErrKBCollectionNotEmpty, domain.ErrKBParentInTrash, domain.ErrKBCollectionInTrash,
	domain.ErrKBArticleLimit, domain.ErrKBCollectionLimit, domain.ErrKBMediaLimit}

func kbError(what string, err error) error {
	if err == nil {
		return nil
	}
	var invalid *domain.InvalidKBError
	if errors.As(err, &invalid) {
		return err
	}
	for _, known := range kbErrors {
		if errors.Is(err, known) {
			return known
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func kbCollectionError(action string, err error) error {
	return kbError(action+" knowledge base collection", err)
}
