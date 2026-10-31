package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/jackc/pgx/v5"
)

// statusNotice is the notice kind and text a channel tells its customer with when the team sets a
// conversation to status: open is told as opened again.
func statusNotice(settings channels.Settings, status string) (kind, text string) {
	switch status {
	case "open":
		return channels.NoticeReopened, settings.Notices.Reopened
	case "pending":
		return channels.NoticeStatusPending, settings.Notices.StatusPending
	case "resolved":
		return channels.NoticeStatusResolved, settings.Notices.StatusResolved
	case "closed":
		return channels.NoticeStatusClosed, settings.Notices.StatusClosed
	}
	return "", ""
}

// statusNoticeChannel is the channel a conversation's status notice goes through: the chat channel the
// conversation is in (an item of it linked), with a text for status. ok is false for a conversation of no
// chat (an email, one the team started) or a channel quiet for that status.
func (s *Store) statusNoticeChannel(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, status string) (channels.Channel, string, string, bool, error) {
	var channelID string
	err := tx.QueryRow(ctx, `SELECT i.channel_id::text FROM conversation_thread_items l
		JOIN channel_thread_items i ON i.workspace_id=l.workspace_id AND i.id=l.item_id
		WHERE l.workspace_id=$1 AND l.conversation_id=$2 AND l.unlinked_at IS NULL ORDER BY l.linked_at DESC,l.id DESC LIMIT 1`,
		workspaceID, conversationID).Scan(&channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, "", "", false, nil
	}
	if err != nil {
		return channels.Channel{}, "", "", false, fmt.Errorf("find the chat of a conversation: %w", err)
	}
	channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
	if errors.Is(err, channels.ErrNotFound) {
		return channels.Channel{}, "", "", false, nil
	}
	if err != nil {
		return channels.Channel{}, "", "", false, err
	}
	kind, text := statusNotice(channel.Settings, status)
	return channel, kind, text, text != "", nil
}

// sendStatusNotices tells the customer of each conversation whose status changed to status, in its chat,
// as the channel's own message (which quotes what the customer wrote first). before is each one's status
// before the change. The chats are found in one query, so a bulk change costs no more until it tells.
func (s *Store) sendStatusNotices(ctx context.Context, tx pgx.Tx, workspaceID string, before map[string]string, status string) error {
	changed := make([]string, 0, len(before))
	for id, was := range before {
		if was != status {
			changed = append(changed, id)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (l.conversation_id) l.conversation_id::text,i.channel_id::text,c.display_ref,c.subject
		FROM conversation_thread_items l JOIN channel_thread_items i ON i.workspace_id=l.workspace_id AND i.id=l.item_id
		JOIN conversations c ON c.workspace_id=l.workspace_id AND c.id=l.conversation_id
		WHERE l.workspace_id=$1 AND l.conversation_id::text=ANY($2) AND l.unlinked_at IS NULL
		ORDER BY l.conversation_id,l.linked_at DESC,l.id DESC`, workspaceID, changed)
	if err != nil {
		return fmt.Errorf("find the chats of conversations: %w", err)
	}
	type chat struct{ conversation, channel, ref, subject string }
	var chats []chat
	for rows.Next() {
		var item chat
		if err := rows.Scan(&item.conversation, &item.channel, &item.ref, &item.subject); err != nil {
			rows.Close()
			return err
		}
		chats = append(chats, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	loaded := map[string]channels.Channel{}
	for _, item := range chats {
		channel, ok := loaded[item.channel]
		if !ok {
			if channel, err = s.channelByID(ctx, tx, workspaceID, item.channel); errors.Is(err, channels.ErrNotFound) {
				continue
			} else if err != nil {
				return err
			}
			loaded[item.channel] = channel
		}
		kind, text := statusNotice(channel.Settings, status)
		if text == "" {
			continue
		}
		if _, err := s.createMessage(ctx, tx, workspaceID, item.conversation, Actor{NoticeKind: kind}, "outgoing",
			channels.RenderNotice(text, map[string]string{"ref": item.ref, "subject": item.subject}), nil, nil); err != nil {
			return fmt.Errorf("send a status notice: %w", err)
		}
	}
	return nil
}

// StatusNoticeWouldSend says whether setting any of the conversations to status would tell its customer
// (a chat channel with a text for it, and a status that changes): a token that may not write to customers
// asks a person first.
func (s *Store) StatusNoticeWouldSend(ctx context.Context, workspaceID string, ids []string, status string) (bool, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		for _, id := range ids {
			var current string
			err := tx.QueryRow(ctx, `SELECT status FROM conversations WHERE workspace_id=$1 AND id::text=$2`, workspaceID, id).Scan(&current)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return false, err
			}
			if current == status {
				continue
			}
			_, _, _, ok, err := s.statusNoticeChannel(ctx, tx, workspaceID, id, status)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	})
}

// statusesBefore reads, under lock, the status of each conversation about to change.
func statusesBefore(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,status FROM conversations WHERE workspace_id=$1 AND id::text=ANY($2) ORDER BY id FOR NO KEY UPDATE`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read statuses: %w", err)
	}
	defer rows.Close()
	before := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		before[id] = status
	}
	return before, rows.Err()
}
