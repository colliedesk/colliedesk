package retention

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// workspaceRemover is implemented by storage that keeps one directory per workspace, so the delete
// also clears files no row points to.
type workspaceRemover interface {
	DeleteWorkspace(ctx context.Context, workspaceID string) error
}

// remove deletes the attachment files of the workspace, then the workspace row, which cascades to
// every table that belongs to it. It runs in one transaction under the workspace row lock: when a
// file cannot be deleted the transaction rolls back, the row stays and the next run starts over
// (deleting a file twice is not an error). The decision was just made under the same lock.
func (s *Service) remove(ctx context.Context, tx pgx.Tx, st state) error {
	var conversations, messages, files, agents int
	var bytes int64
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM conversations WHERE workspace_id=$1),
		(SELECT count(*) FROM messages WHERE workspace_id=$1),
		(SELECT count(*) FROM attachments WHERE workspace_id=$1),
		(SELECT COALESCE(sum(size),0) FROM attachments WHERE workspace_id=$1),
		(SELECT count(*) FROM agents WHERE workspace_id=$1)`, st.WorkspaceID).
		Scan(&conversations, &messages, &files, &bytes, &agents); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT storage_key FROM attachments WHERE workspace_id=$1`, st.WorkspaceID)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.config.Storage.Delete(ctx, key); err != nil {
			return fmt.Errorf("delete attachment file: %w", err)
		}
	}
	if remover, ok := s.config.Storage.(workspaceRemover); ok {
		if err := remover.DeleteWorkspace(ctx, st.WorkspaceID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, st.WorkspaceID); err != nil {
		return err
	}
	s.config.Logger.Info("workspace deleted after 12 months locked", "workspace_id", st.WorkspaceID, "slug", st.Slug,
		"locked_since", st.LockedSince.Format(dateLayout), "conversations", conversations, "messages", messages,
		"attachments", files, "attachment_bytes", bytes, "agents", agents)
	return nil
}
