package store

import (
	"context"
	"fmt"

	"github.com/hclabsco/colliedesk/server/internal/domain"
)

func (s *Store) CreateTag(ctx context.Context, workspaceID, name, color string) (domain.Tag, error) {
	var tag domain.Tag
	err := s.pool.QueryRow(ctx, `INSERT INTO tags(workspace_id,name,color) VALUES($1,$2,$3)
		RETURNING id::text,name,color`, workspaceID, name, color).
		Scan(&tag.ID, &tag.Name, &tag.Color)
	if err != nil {
		return domain.Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return tag, nil
}

func (s *Store) UpdateTag(ctx context.Context, workspaceID, id, name string) (domain.Tag, error) {
	var tag domain.Tag
	err := s.pool.QueryRow(ctx, `UPDATE tags SET name=$3 WHERE workspace_id=$1 AND id=$2
		RETURNING id::text,name,color`, workspaceID, id, name).
		Scan(&tag.ID, &tag.Name, &tag.Color)
	if err != nil {
		return domain.Tag{}, fmt.Errorf("update tag: %w", err)
	}
	return tag, nil
}

func (s *Store) DeleteTag(ctx context.Context, workspaceID, id string) (bool, error) {
	result, err := s.pool.Exec(ctx, `DELETE FROM tags WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
	if err != nil {
		return false, fmt.Errorf("delete tag: %w", err)
	}
	return result.RowsAffected() == 1, nil
}
