package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
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

// chatChannel is the live chat channel a conversation is in (an item of it linked). ok is false for a
// conversation of no chat (an email, one the team started) or one whose channel was removed or turned off.
func (s *Store) chatChannel(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) (channels.Channel, bool, error) {
	var channelID string
	err := tx.QueryRow(ctx, `SELECT i.channel_id::text FROM conversation_thread_items l
		JOIN channel_thread_items i ON i.workspace_id=l.workspace_id AND i.id=l.item_id
		WHERE l.workspace_id=$1 AND l.conversation_id=$2 AND l.unlinked_at IS NULL ORDER BY l.linked_at DESC,l.id DESC LIMIT 1`,
		workspaceID, conversationID).Scan(&channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return channels.Channel{}, false, nil
	}
	if err != nil {
		return channels.Channel{}, false, fmt.Errorf("find the chat of a conversation: %w", err)
	}
	channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
	if errors.Is(err, channels.ErrNotFound) {
		return channels.Channel{}, false, nil
	}
	if err != nil {
		return channels.Channel{}, false, err
	}
	return channel, telling(channel), nil
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
		// A removed or turned off channel sends nothing: it is as quiet as one with no words for the status.
		kind, text := statusNotice(channel.Settings, status)
		if text == "" || !telling(channel) {
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
// (a live chat channel with a text for it, and a status that changes): a token that may not write to
// customers asks a person first.
func (s *Store) StatusNoticeWouldSend(ctx context.Context, workspaceID string, ids []string, status string) (bool, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		previews, err := s.statusNoticePreview(ctx, tx, workspaceID, ids, status)
		return len(previews) > 0, err
	})
}

// StatusNoticePreview is what setting the conversations to status would tell their customers, one per
// chat channel, for the person who approves it.
func (s *Store) StatusNoticePreview(ctx context.Context, workspaceID string, ids []string, status string) ([]domain.StatusNoticePreview, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.StatusNoticePreview, error) {
		return s.statusNoticePreview(ctx, tx, workspaceID, ids, status)
	})
}

// statusNoticePreview finds the chats of the conversations whose status would change in one query,
// whatever the number of ids, and reads each channel once.
func (s *Store) statusNoticePreview(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, status string) ([]domain.StatusNoticePreview, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (l.conversation_id) i.channel_id::text,c.display_ref,c.subject
		FROM conversation_thread_items l JOIN channel_thread_items i ON i.workspace_id=l.workspace_id AND i.id=l.item_id
		JOIN conversations c ON c.workspace_id=l.workspace_id AND c.id=l.conversation_id
		WHERE l.workspace_id=$1 AND l.conversation_id::text=ANY($2) AND l.unlinked_at IS NULL AND c.status<>$3
		ORDER BY l.conversation_id,l.linked_at DESC,l.id DESC`, workspaceID, ids, status)
	if err != nil {
		return nil, fmt.Errorf("find the chats of conversations: %w", err)
	}
	type chat struct{ Channel, Ref, Subject string }
	chats, err := pgx.CollectRows(rows, pgx.RowToStructByPos[chat])
	if err != nil {
		return nil, err
	}
	var previews []domain.StatusNoticePreview
	at := map[string]int{}
	skip := map[string]bool{}
	for _, item := range chats {
		if index, ok := at[item.Channel]; ok {
			previews[index].Told++
			continue
		}
		if skip[item.Channel] {
			continue
		}
		channel, err := s.channelByID(ctx, tx, workspaceID, item.Channel)
		if err != nil && !errors.Is(err, channels.ErrNotFound) {
			return nil, err
		}
		_, text := statusNotice(channel.Settings, status)
		if err != nil || text == "" || !telling(channel) {
			skip[item.Channel] = true
			continue
		}
		at[item.Channel] = len(previews)
		previews = append(previews, domain.StatusNoticePreview{ChannelID: channel.ID, ChannelName: channel.Name, ExampleRef: item.Ref, Told: 1,
			Text: channels.RenderNotice(text, map[string]string{"ref": item.Ref, "subject": item.Subject})})
	}
	return previews, nil
}

// SendDueStatusNotices tells the customers of the conversations a bulk change marked
// (conversations.status_notice_due), each in a transaction of its own that takes the mark: two servers
// send it once, and a failure keeps the mark for the next round. A conversation whose status changed
// again since lost its mark (migration 20261031170000-status-change-cause) and tells nothing. It returns
// how many marks it took.
func (s *Store) SendDueStatusNotices(ctx context.Context, limit int) (int, error) {
	type marked struct{ WorkspaceID, ConversationID string }
	var due []marked
	err := s.db.Privileged(ctx, database.PrivilegeStatusNoticeClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,id::text FROM conversations WHERE status_notice_due IS NOT NULL ORDER BY updated_at,id LIMIT $1`, limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, pgx.RowToStructByPos[marked])
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("list status notices to send: %w", err)
	}
	taken := 0
	var failed error
	for _, conversation := range due {
		took, err := scoped(ctx, s, conversation.WorkspaceID, func(tx pgx.Tx) (bool, error) {
			var status string
			err := tx.QueryRow(ctx, `UPDATE conversations SET status_notice_due=NULL
				WHERE workspace_id=$1 AND id::text=$2 AND status_notice_due IS NOT NULL AND status_notice_due=status RETURNING status`,
				conversation.WorkspaceID, conversation.ConversationID).Scan(&status)
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, s.sendStatusNotices(ctx, tx, conversation.WorkspaceID, map[string]string{conversation.ConversationID: ""}, status)
		})
		if err != nil {
			// One conversation that fails keeps its mark and does not hold up the others.
			failed = errors.Join(failed, fmt.Errorf("send a status notice: %w", err))
			continue
		}
		if took {
			taken++
		}
	}
	return taken, failed
}

// telling says a channel tells its customers: live and on. A paused one keeps its notices for when it
// resumes (the outbox holds them).
func telling(channel channels.Channel) bool {
	return channel.Live() && channel.Enabled
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
