package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListTags(ctx context.Context, workspaceID string) ([]domain.Tag, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Tag, error) { return listTags(ctx, tx, workspaceID) })
}

// ListTagsWithCounts is ListTags with how many conversations of the workspace carry each tag, in any status.
func (s *Store) ListTagsWithCounts(ctx context.Context, workspaceID string) ([]domain.Tag, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Tag, error) {
		rows, err := tx.Query(ctx, `SELECT t.id::text, t.name, t.color, count(ct.conversation_id)::int FROM tags t
			LEFT JOIN conversation_tags ct ON ct.workspace_id=t.workspace_id AND ct.tag_id=t.id
			WHERE t.workspace_id=$1 GROUP BY t.id ORDER BY t.name`, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list tags with counts: %w", err)
		}
		defer rows.Close()
		tags := make([]domain.Tag, 0)
		for rows.Next() {
			var tag domain.Tag
			var count int
			if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color, &count); err != nil {
				return nil, fmt.Errorf("scan tag with count: %w", err)
			}
			tag.ConversationCount = &count
			tags = append(tags, tag)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate tags with counts: %w", err)
		}
		return tags, nil
	})
}

func listTags(ctx context.Context, tx pgx.Tx, workspaceID string) ([]domain.Tag, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,name,color FROM tags WHERE workspace_id=$1 ORDER BY name`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	tags := make([]domain.Tag, 0)
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}
	return tags, nil
}
