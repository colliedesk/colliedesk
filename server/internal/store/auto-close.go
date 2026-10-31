package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// AutoCloseCandidate is a resolved conversation whose chat channel closes it by itself now.
type AutoCloseCandidate struct {
	WorkspaceID, ConversationID string
}

// AutoCloseDue lists, in every workspace, the resolved conversations of a channel with auto_close_hours
// whose time came: that long after the later of their last resolve and the customer's last message.
func (s *Store) AutoCloseDue(ctx context.Context, now time.Time, limit int) ([]AutoCloseCandidate, error) {
	var due []AutoCloseCandidate
	err := s.db.Privileged(ctx, database.PrivilegeAutoCloseClaim, func(tx pgx.Tx) error {
		// The channels that close by themselves: their settings over their type's defaults.
		rows, err := tx.Query(ctx, `SELECT `+channelRowColumns+` FROM channels WHERE deleted_at IS NULL`)
		if err != nil {
			return err
		}
		var ids []string
		var hours []int32
		for rows.Next() {
			channel, err := s.scanChannelRow(rows)
			if err != nil {
				rows.Close()
				return err
			}
			if h := channel.Settings.AutoCloseHours; h != nil && *h > 0 {
				ids, hours = append(ids, channel.ID), append(hours, int32(*h))
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(ids) == 0 {
			return err
		}
		found, err := tx.Query(ctx, `WITH closing AS (SELECT id::uuid AS id,hours FROM unnest($3::text[],$4::int[]) AS t(id,hours))
			SELECT c.workspace_id::text,c.id::text FROM conversations c JOIN closing ch ON ch.id=c.channel_id
			WHERE c.status='resolved' AND GREATEST(
				COALESCE((SELECT max(h.changed_at) FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.to_status='resolved'), c.updated_at),
				COALESCE((SELECT max(m.created_at) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming'), '-infinity'::timestamptz)
			) + make_interval(hours => ch.hours) <= $1
			ORDER BY c.updated_at,c.id LIMIT $2`, now, limit, ids, hours)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(found, func(row pgx.CollectableRow) (AutoCloseCandidate, error) {
			var item AutoCloseCandidate
			err := row.Scan(&item.WorkspaceID, &item.ConversationID)
			return item, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list conversations to close: %w", err)
	}
	return due, nil
}

// AutoClose closes one conversation while it is still resolved (two servers close it once) and tells its
// customer with the channel's closed notice, unless it was imported resolved and nobody changed its status
// here since: the system before had told them.
func (s *Store) AutoClose(ctx context.Context, conversation AutoCloseCandidate, now time.Time) (bool, error) {
	return scoped(ctx, s, conversation.WorkspaceID, func(tx pgx.Tx) (bool, error) {
		if err := setStatusActor(ctx, tx, ""); err != nil {
			return false, err
		}
		var quiet bool
		err := tx.QueryRow(ctx, `UPDATE conversations c SET status='closed',updated_at=now()
			WHERE c.workspace_id=$1 AND c.id::text=$2 AND c.status='resolved'
			RETURNING NOT EXISTS(SELECT 1 FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.source='colliedesk')`,
			conversation.WorkspaceID, conversation.ConversationID).Scan(&quiet)
		if err == pgx.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("close a resolved conversation: %w", err)
		}
		if !quiet {
			if err := s.sendStatusNotices(ctx, tx, conversation.WorkspaceID, map[string]string{conversation.ConversationID: "resolved"}, "closed"); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}
