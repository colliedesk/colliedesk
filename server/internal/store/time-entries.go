package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ListTimeEntries returns the time logged on one conversation of a workspace.
func (s *Store) ListTimeEntries(ctx context.Context, workspaceID, conversationID string) ([]domain.TimeEntry, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TimeEntry, error) {
		return listTimeEntries(ctx, tx, workspaceID, conversationID)
	})
}

func listTimeEntries(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) ([]domain.TimeEntry, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,agent_id::text,agent_name,minutes,note,billable,executed_at
		FROM time_entries WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY executed_at,id`, workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list time entries: %w", err)
	}
	defer rows.Close()
	items := make([]domain.TimeEntry, 0)
	for rows.Next() {
		var item domain.TimeEntry
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.AgentID, &item.AgentName, &item.Minutes, &item.Note, &item.Billable, &item.ExecutedAt); err != nil {
			return nil, fmt.Errorf("scan time entry: %w", err)
		}
		item.ExecutedAt = domain.UTC(item.ExecutedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate time entries: %w", err)
	}
	return items, nil
}
