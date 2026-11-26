package zalo

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// addedStaff are the ids of staff that the current list lacks.
func addedStaff(current, staff []string) []string {
	var added []string
	for _, id := range staff {
		if !contains(current, id) {
			added = append(added, id)
		}
	}
	return added
}

// closeTeamOnlyConversations closes, without a word to the chat, the open and pending conversations of
// the channel that only the team members added wrote in: before they were on the list, their messages
// in a group were taken for a customer's and opened conversations that are nobody's request. A
// conversation a customer wrote in, or one with a message from elsewhere, stays as it is. Each can be
// reopened; the channel's history records how many were closed.
func closeTeamOnlyConversations(ctx context.Context, tx pgx.Tx, channel channels.Channel, added []string, change channels.ConfigChange) (int, error) {
	if len(added) == 0 {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT c.id::text FROM channel_thread_items i
			JOIN messages m ON m.workspace_id=i.workspace_id AND m.id=i.message_id
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
		WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.role='customer' AND i.sender_kind=$3 AND i.sender_value=ANY($4)
			AND c.status IN ('open','pending')
			AND NOT EXISTS (SELECT 1 FROM messages o WHERE o.workspace_id=c.workspace_id AND o.conversation_id=c.id AND o.kind='incoming'
				AND NOT EXISTS (SELECT 1 FROM channel_thread_items x WHERE x.workspace_id=o.workspace_id AND x.message_id=o.id))
			AND NOT EXISTS (SELECT 1 FROM channel_thread_items x
					JOIN messages o ON o.workspace_id=x.workspace_id AND o.id=x.message_id
				WHERE x.workspace_id=c.workspace_id AND o.conversation_id=c.id AND x.role='customer'
					AND NOT (x.sender_kind=$3 AND x.sender_value=ANY($4)))`,
		channel.WorkspaceID, channel.ID, IdentityUser, added)
	if err != nil {
		return 0, fmt.Errorf("find conversations only the team wrote in: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, fmt.Errorf("find conversations only the team wrote in: %w", err)
	}
	closed := "closed"
	for _, id := range ids {
		if _, err := store.UpdateConversationIn(ctx, tx, channel.WorkspaceID, id, store.ConversationUpdate{
			Status: &closed, ActorID: change.ActorAgentID, TokenID: change.ActorTokenID, Quiet: true}); err != nil {
			return 0, fmt.Errorf("close a conversation only the team wrote in: %w", err)
		}
	}
	return len(ids), nil
}
