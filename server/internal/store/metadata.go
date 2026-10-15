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
