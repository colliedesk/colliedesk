package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Every list of the knowledge base sorts siblings the same way: by the position set by hand, then by
// name or title. Until a parent is ordered by hand its children all have position 0, which is the title
// order the knowledge base always had.
const (
	kbCollectionOrder = `position, lower(name), id`
	kbArticleOrder    = `position, lower(title), number`
)

// kbArticleEnd is the position of an article added to collection (an SQL expression) of workspace $1:
// after the last one once the collection was ordered by hand, else 0, which keeps the title order.
func kbArticleEnd(collection string) string {
	return `(SELECT CASE WHEN max(position) > 0 THEN max(position)+1 ELSE 0 END FROM kb_articles
		WHERE workspace_id=$1 AND collection_id=` + collection + ` AND deleted_at IS NULL)`
}

// kbCollectionEnd is kbArticleEnd for a collection added under parent (an SQL expression; NULL is the
// top), leaving out the collection self (an SQL expression) when it moves.
func kbCollectionEnd(parent, self string) string {
	return `(SELECT CASE WHEN max(position) > 0 THEN max(position)+1 ELSE 0 END FROM kb_collections
		WHERE workspace_id=$1 AND parent_id IS NOT DISTINCT FROM ` + parent + ` AND id IS DISTINCT FROM ` + self + ` AND deleted_at IS NULL)`
}

// kbTreeOrder is a CTE over the live collections of workspace $1: tree(id, path) where path sorts the
// collections as the help center lists them, a collection right before its own collections.
const kbTreeOrder = `WITH RECURSIVE ranked AS (
		SELECT id, parent_id, row_number() OVER (PARTITION BY parent_id ORDER BY ` + kbCollectionOrder + `) AS place
		FROM kb_collections WHERE workspace_id=$1 AND deleted_at IS NULL),
	tree AS (SELECT id, ARRAY[place] AS path FROM ranked WHERE parent_id IS NULL
		UNION ALL SELECT r.id, tree.path || r.place FROM ranked r JOIN tree ON r.parent_id=tree.id) `

// cleanKBOrder checks the shape of an order: ids, each once, and no article at the top. The ids come
// back in lowercase, as the database prints them.
func cleanKBOrder(order *domain.KBOrder) error {
	if order.ParentID != nil {
		if !savedReplyID.MatchString(*order.ParentID) {
			return &domain.InvalidKBError{Message: "parent_id is a collection id, or null for the top."}
		}
		parent := strings.ToLower(*order.ParentID)
		order.ParentID = &parent
	} else if len(order.ArticleIDs) > 0 {
		return &domain.InvalidKBError{Message: "The top holds collections only: send article_ids empty with parent_id null."}
	}
	if len(order.CollectionIDs) > domain.MaxKBCollections || len(order.ArticleIDs) > domain.MaxKBArticles {
		return &domain.InvalidKBError{Message: fmt.Sprintf("A parent holds at most %d collections and %d articles.", domain.MaxKBCollections, domain.MaxKBArticles)}
	}
	for _, list := range []*[]string{&order.CollectionIDs, &order.ArticleIDs} {
		seen := make(map[string]bool, len(*list))
		for index, id := range *list {
			if !savedReplyID.MatchString(id) {
				return &domain.InvalidKBError{Message: fmt.Sprintf("%q is not an id.", id)}
			}
			id = strings.ToLower(id)
			if seen[id] {
				return &domain.InvalidKBError{Message: fmt.Sprintf("%s is in the order twice.", id)}
			}
			seen[id], (*list)[index] = true, id
		}
	}
	return nil
}

// readKBOrder reads what sits under parent (nil: the top) outside the trash, in its current order, and
// locks those rows FOR NO KEY UPDATE: the articles first, then the collections, the order every write
// that moves an article takes them in (the article's row, then the collection it goes to). A row that
// moved away or went to the trash while this read waited for it is left out, so the order is stale.
func readKBOrder(ctx context.Context, tx pgx.Tx, workspaceID string, parent *string) (domain.KBOrder, error) {
	order := domain.KBOrder{ParentID: parent}
	rows, err := tx.Query(ctx, `SELECT id::text FROM kb_articles WHERE workspace_id=$1 AND collection_id=$2::uuid AND deleted_at IS NULL
		ORDER BY `+kbArticleOrder+` FOR NO KEY UPDATE`, workspaceID, parent)
	if err != nil {
		return order, err
	}
	if order.ArticleIDs, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return order, err
	}
	rows, err = tx.Query(ctx, `SELECT id::text FROM kb_collections WHERE workspace_id=$1 AND parent_id IS NOT DISTINCT FROM $2::uuid AND deleted_at IS NULL
		ORDER BY `+kbCollectionOrder+` FOR NO KEY UPDATE`, workspaceID, parent)
	if err != nil {
		return order, err
	}
	order.CollectionIDs, err = pgx.CollectRows(rows, pgx.RowTo[string])
	return order, err
}

// sameMembers reports whether a and b hold the same ids; neither repeats one.
func sameMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	in := make(map[string]struct{}, len(a))
	for _, id := range a {
		in[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := in[id]; !ok {
			return false
		}
	}
	return true
}

// SetKBOrder gives the collections and articles directly under order.ParentID the positions 1, 2, 3 in
// the order given, and bumps the knowledge base version so help center pages are read again. The lists
// must name exactly what the parent holds outside the trash; otherwise nothing changes and the error is a
// *domain.KBOrderStaleError holding the current lists. A parent that is not a live collection of the
// workspace is domain.ErrKBCollectionNotFound. updated_at stays: an order is not an edit of the items.
func (s *Store) SetKBOrder(ctx context.Context, workspaceID string, order domain.KBOrder) (domain.KBOrder, error) {
	if err := cleanKBOrder(&order); err != nil {
		return domain.KBOrder{}, err
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBOrder, error) {
		if err := lockKB(ctx, tx, workspaceID); err != nil {
			return domain.KBOrder{}, err
		}
		if order.ParentID != nil {
			var live bool
			err := tx.QueryRow(ctx, `SELECT deleted_at IS NULL FROM kb_collections WHERE workspace_id=$1 AND id=$2`, workspaceID, *order.ParentID).Scan(&live)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
				return domain.KBOrder{}, domain.ErrKBCollectionNotFound
			}
			if err != nil {
				return domain.KBOrder{}, err
			}
		}
		current, err := readKBOrder(ctx, tx, workspaceID, order.ParentID)
		if err != nil {
			return domain.KBOrder{}, err
		}
		if !sameMembers(current.CollectionIDs, order.CollectionIDs) || !sameMembers(current.ArticleIDs, order.ArticleIDs) {
			return domain.KBOrder{}, &domain.KBOrderStaleError{Current: current}
		}
		// The rows are locked and checked above; the parent filter keeps even a missed race from placing a
		// row that sits elsewhere. Articles first, as they were locked.
		moved := int64(0)
		for _, table := range []struct {
			name, parent string
			ids          []string
		}{{"kb_articles", "collection_id=$3::uuid", order.ArticleIDs}, {"kb_collections", "parent_id IS NOT DISTINCT FROM $3::uuid", order.CollectionIDs}} {
			tag, err := tx.Exec(ctx, `UPDATE `+table.name+` t SET position=o.place FROM unnest($2::uuid[]) WITH ORDINALITY o(id, place)
				WHERE t.workspace_id=$1 AND t.id=o.id AND t.`+table.parent+` AND t.deleted_at IS NULL AND t.position<>o.place`, workspaceID, table.ids, order.ParentID)
			if err != nil {
				return domain.KBOrder{}, err
			}
			moved += tag.RowsAffected()
		}
		// An order that moves nothing leaves the cached help center pages valid.
		if moved == 0 {
			return order, nil
		}
		return order, bumpKB(ctx, tx, workspaceID)
	})
	return result, kbError("order knowledge base", err)
}
