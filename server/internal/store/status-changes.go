package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// attachStatusChanges gives each conversation its last status change and who made it, in one query: a
// team member by name (their email when they set none), a token by its label (and the person who approved its request, when another than
// its owner did), the customer, the channel and an import by none.
func attachStatusChanges(ctx context.Context, tx pgx.Tx, workspaceID string, items []domain.Conversation) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	at := make(map[string]int, len(items))
	for index, item := range items {
		ids[index], at[item.ID] = item.ID, index
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (h.conversation_id) h.conversation_id::text,h.to_status,h.changed_at,h.cause,
			CASE h.cause WHEN 'agent' THEN COALESCE(NULLIF(a.display_name,''),a.email)
				WHEN 'token' THEN CASE WHEN s.id IS NULL THEN COALESCE(NULLIF(a.display_name,''),a.email)
					WHEN h.actor_agent_id IS DISTINCT FROM s.agent_id AND a.id IS NOT NULL THEN s.label||' · approved by '||COALESCE(NULLIF(a.display_name,''),a.email)
					ELSE s.label END END
		FROM conversation_status_changes h
		LEFT JOIN agents a ON a.workspace_id=h.workspace_id AND a.id=h.actor_agent_id
		LEFT JOIN sessions s ON s.workspace_id=h.workspace_id AND s.id=h.actor_token_id
		WHERE h.workspace_id=$1 AND h.conversation_id=ANY($2::uuid[])
		ORDER BY h.conversation_id,h.changed_at DESC,h.id DESC`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read last status changes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var change domain.StatusChangeView
		var changed time.Time
		if err := rows.Scan(&id, &change.To, &changed, &change.By, &change.Name); err != nil {
			return err
		}
		change.At = domain.UTC(changed)
		items[at[id]].LastStatusChange = &change
	}
	return rows.Err()
}

// conversationStatuses are the statuses the team sets, in the order a person reads them.
var conversationStatuses = []string{"open", "pending", "resolved", "closed"}

// attachStatusNotices gives a conversation the statuses that, set now, would tell its customer: the ones
// its live chat channel has words for, other than its status (as StatusNoticeWouldSend says per status).
func (s *Store) attachStatusNotices(ctx context.Context, tx pgx.Tx, workspaceID string, item *domain.Conversation) error {
	channel, ok, err := s.chatChannel(ctx, tx, workspaceID, item.ID)
	if err != nil || !ok {
		return err
	}
	for _, status := range conversationStatuses {
		if _, text := statusNotice(channel.Settings, status); text != "" && status != item.Status {
			item.StatusNotices = append(item.StatusNotices, status)
		}
	}
	return nil
}

// mayTell says setting a conversation of status to another status may tell its customer through its chat
// channel: live and on, with words for one of them (as StatusNoticeWouldSend and attachStatusNotices read it).
func mayTell(channel *channels.Channel, status string) bool {
	if channel == nil || !telling(*channel) {
		return false
	}
	for _, other := range conversationStatuses {
		if _, text := statusNotice(channel.Settings, other); text != "" && other != status {
			return true
		}
	}
	return false
}
