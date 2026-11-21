package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbArticleVisibilityColumns read article a's own visibility and its effective one, from the view every
// help center reader goes through.
const kbArticleVisibilityColumns = `a.visibility,kb_visibility_name((SELECT w.visibility_rank FROM kb_article_visibility w
	WHERE w.workspace_id=a.workspace_id AND w.id=a.id))`

// kbCollectionVisibilityColumns read collection c's own visibility and its effective one; a collection
// in the trash, which the view leaves out, has its own.
const kbCollectionVisibilityColumns = `c.visibility,kb_visibility_name(COALESCE((SELECT v.visibility_rank FROM kb_collection_visibility v
	WHERE v.workspace_id=c.workspace_id AND v.id=c.id),kb_visibility_rank(c.visibility)))`

// readKBVisibilityFrom sets article.VisibilityFrom to the most restrictive live collection the article
// is in, or a live parent of one, when it is more restrictive than the article itself.
func readKBVisibilityFrom(ctx context.Context, tx pgx.Tx, workspaceID string, article *domain.KBArticle) error {
	var from domain.KBVisibilityCollection
	err := tx.QueryRow(ctx, `WITH RECURSIVE listed AS (
			SELECT collection_id AS id FROM kb_articles WHERE workspace_id=$1 AND id=$2
			UNION SELECT collection_id FROM kb_article_collections WHERE workspace_id=$1 AND article_id=$2),
		chain AS (
			SELECT c.id, c.name, c.visibility, c.parent_id, 1 AS depth FROM kb_collections c JOIN listed l ON c.id=l.id
			WHERE c.workspace_id=$1 AND c.deleted_at IS NULL
			UNION ALL SELECT p.id, p.name, p.visibility, p.parent_id, chain.depth+1 FROM kb_collections p JOIN chain ON p.id=chain.parent_id
			WHERE p.workspace_id=$1 AND p.deleted_at IS NULL AND chain.depth < 10)
		SELECT id::text, name, visibility FROM chain WHERE kb_visibility_rank(visibility) > kb_visibility_rank($3)
		ORDER BY kb_visibility_rank(visibility) DESC, depth, lower(name) LIMIT 1`, workspaceID, article.ID, article.Visibility).Scan(&from.ID, &from.Name, &from.Visibility)
	if errors.Is(err, pgx.ErrNoRows) {
		article.VisibilityFrom = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("read knowledge base article visibility: %w", err)
	}
	article.VisibilityFrom = &from
	return nil
}

// kbVisibilitySnapshot returns the effective visibility rank of each published article outside the
// trash: those of ids, or every one with nil ids. guardKBVisibilityRaise compares it after a write.
func kbVisibilitySnapshot(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT id::text, visibility_rank FROM kb_article_visibility
		WHERE workspace_id=$1 AND status='published' AND deleted_at IS NULL AND ($2::uuid[] IS NULL OR id=ANY($2::uuid[]))`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base visibility: %w", err)
	}
	ranks := map[string]int{}
	var id string
	var rank int
	if _, err := pgx.ForEachRow(rows, []any{&id, &rank}, func() error {
		ranks[id] = rank
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read knowledge base visibility: %w", err)
	}
	return ranks, nil
}

// guardKBVisibilityRaise runs after a write that may show published articles to more people (a
// visibility, a collection's parent, a collection in the trash) and refuses it with a
// *domain.KBVisibilityRaiseError when an article that is now seen more widely is in a collection that
// needs approval and no review approved its published text. The caller made sure the person may
// publish; lowering a visibility always passes.
func guardKBVisibilityRaise(ctx context.Context, tx pgx.Tx, workspaceID string, before map[string]int) error {
	ids := make([]string, 0, len(before))
	for id := range before {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	after, err := kbVisibilitySnapshot(ctx, tx, workspaceID, ids)
	if err != nil {
		return err
	}
	var unapproved []string
	for id, rank := range after {
		if rank >= before[id] {
			continue
		}
		article, err := readKBArticle(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		required, err := kbApprovalRequired(ctx, tx, workspaceID, article.CollectionIDs)
		if err != nil {
			return err
		}
		if !required {
			continue
		}
		if err := checkKBApproval(ctx, tx, workspaceID, id, nil, article.Content().Hash()); errors.Is(err, domain.ErrKBApprovalRequired) {
			unapproved = append(unapproved, id)
		} else if err != nil {
			return err
		}
	}
	if len(unapproved) > 0 {
		slices.Sort(unapproved)
		return &domain.KBVisibilityRaiseError{ArticleIDs: unapproved}
	}
	return nil
}

// setKBArticleVisibility sets an article's own visibility, guarded like any raise.
func setKBArticleVisibility(ctx context.Context, tx pgx.Tx, workspaceID, id, visibility string) error {
	before, err := kbVisibilitySnapshot(ctx, tx, workspaceID, []string{id})
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE kb_articles SET visibility=$3 WHERE workspace_id=$1 AND id=$2 AND visibility<>$3`, workspaceID, id, visibility)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if err := guardKBVisibilityRaise(ctx, tx, workspaceID, before); err != nil {
		return err
	}
	return bumpKB(ctx, tx, workspaceID)
}

// KBArticleVisibilityRaises tells whether setting article id's own visibility would show it, while
// published, to more people than now: the change that needs kb_articles:publish.
func (s *Store) KBArticleVisibilityRaises(ctx context.Context, workspaceID, id, visibility string) (bool, error) {
	if !savedReplyID.MatchString(id) {
		return false, nil
	}
	raises, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var raises bool
		err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT GREATEST(kb_visibility_rank($3), w.collection_rank) < w.visibility_rank
			FROM kb_article_visibility w WHERE w.workspace_id=$1 AND w.id=$2 AND w.status='published' AND w.deleted_at IS NULL), false)`,
			workspaceID, id, visibility).Scan(&raises)
		return raises, err
	})
	return raises, kbError("read knowledge base article visibility", err)
}

// KBCollectionVisibilityRaises tells whether setting collection id's own visibility to visibility
// would let more people see it, or is not a visibility at all: the change that needs
// kb_articles:publish.
func (s *Store) KBCollectionVisibilityRaises(ctx context.Context, workspaceID, id, visibility string) (bool, error) {
	if !savedReplyID.MatchString(id) || domain.CheckKBVisibility(visibility) != nil {
		return true, nil
	}
	raises, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var raises bool
		err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT kb_visibility_rank($3) < kb_visibility_rank(visibility)
			FROM kb_collections WHERE workspace_id=$1 AND id=$2), true)`, workspaceID, id, visibility).Scan(&raises)
		return raises, err
	})
	return raises, kbError("read knowledge base collection visibility", err)
}
