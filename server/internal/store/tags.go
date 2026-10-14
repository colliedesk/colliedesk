package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateTag(ctx context.Context, workspaceID, name, color string) (domain.Tag, error) {
	var tag domain.Tag
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO tags(workspace_id,name,color) VALUES($1,$2,$3)
			RETURNING id::text,name,color`, workspaceID, name, color).
			Scan(&tag.ID, &tag.Name, &tag.Color)
	})
	if err != nil {
		return domain.Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return tag, nil
}

func (s *Store) UpdateTag(ctx context.Context, workspaceID, id, name string) (domain.Tag, error) {
	var tag domain.Tag
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE tags SET name=$3 WHERE workspace_id=$1 AND id=$2
			RETURNING id::text,name,color`, workspaceID, id, name).
			Scan(&tag.ID, &tag.Name, &tag.Color)
	})
	if err != nil {
		return domain.Tag{}, fmt.Errorf("update tag: %w", err)
	}
	return tag, nil
}

func (s *Store) DeleteTag(ctx context.Context, workspaceID, id string) (bool, error) {
	deleted, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `DELETE FROM tags WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
		return result.RowsAffected() == 1, err
	})
	if err != nil {
		return false, fmt.Errorf("delete tag: %w", err)
	}
	return deleted, nil
}
