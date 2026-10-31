package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// AutoCloseCandidate is a resolved conversation whose chat channel closes it by itself now: Hours after
// its resolve, which came after Since (when the channel's auto_close_hours was turned on).
type AutoCloseCandidate struct {
	WorkspaceID, ConversationID string
	Hours                       int32
	Since                       time.Time
}

// autoCloseDue is the condition, on the conversation c, that it closes now ($1): resolved since the channel
// began closing by itself (since: when auto_close_hours was turned on), and hours (the current setting)
// after the later of that resolve and the customer's last message. A conversation resolved before it was
// turned on never closes by itself: turning it on never closes and tells a backlog. One imported resolved
// and untouched here since closes as the desk it came from would have, its hours after its resolve there,
// quietly (AutoClose). AutoCloseDue lists by it and AutoClose closes by it again, so a conversation
// resolved again or written to in between waits.
func autoCloseDue(hours, since string) string {
	resolved := `COALESCE((SELECT max(h.changed_at) FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.to_status='resolved'),c.updated_at)`
	untouched := `NOT EXISTS(SELECT 1 FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.source='colliedesk')`
	imported := `c.origin IS NOT NULL AND ` + untouched
	return `c.status='resolved' AND (` + resolved + ` >= ` + since + ` OR (` + imported + `)) AND GREATEST(` + resolved + `,
		COALESCE((SELECT max(m.created_at) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='incoming'), '-infinity'::timestamptz)
	) + make_interval(hours => ` + hours + `) <= $1`
}

// AutoCloseDue lists, in every workspace, the resolved conversations of a channel with auto_close_hours
// whose time came. A channel with hours and no time they were turned on (they come from its type's
// defaults) begins now: it is stamped and closes nothing this round.
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
		if _, err := tx.Exec(ctx, `UPDATE channels SET auto_close_since=$2 WHERE id::text=ANY($1) AND auto_close_since IS NULL`, ids, now); err != nil {
			return fmt.Errorf("stamp when channels began closing: %w", err)
		}
		found, err := tx.Query(ctx, `WITH closing AS (SELECT t.id::uuid AS id,t.hours,ch.auto_close_since AS since
				FROM unnest($3::text[],$4::int[]) AS t(id,hours) JOIN channels ch ON ch.id=t.id::uuid)
			SELECT c.workspace_id::text,c.id::text,ch.hours,ch.since FROM conversations c JOIN closing ch ON ch.id=c.channel_id
			WHERE `+autoCloseDue("ch.hours", "ch.since")+`
			ORDER BY c.updated_at,c.id LIMIT $2`, now, limit, ids, hours)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(found, pgx.RowToStructByPos[AutoCloseCandidate])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list conversations to close: %w", err)
	}
	return due, nil
}

// AutoClose closes one conversation while it is still due (two servers close it once, and one resolved
// again or written to since waits) and tells its customer with the channel's closed notice, unless the
// team resolved it without telling them, or it was imported resolved and nobody changed its status here
// since: the system before had told them.
func (s *Store) AutoClose(ctx context.Context, conversation AutoCloseCandidate, now time.Time) (bool, error) {
	return scoped(ctx, s, conversation.WorkspaceID, func(tx pgx.Tx) (bool, error) {
		if err := setStatusCause(ctx, tx, statusChange{Cause: CauseAutoClose}); err != nil {
			return false, err
		}
		var quiet bool
		err := tx.QueryRow(ctx, `UPDATE conversations c SET status='closed',updated_at=now()
			WHERE c.workspace_id=$2 AND c.id::text=$3 AND `+autoCloseDue("$4::int", "$5::timestamptz")+`
			RETURNING NOT EXISTS(SELECT 1 FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.source='colliedesk')
				OR COALESCE((SELECT h.quiet FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id AND h.to_status='resolved'
					ORDER BY h.changed_at DESC,h.id DESC LIMIT 1),false)`,
			now, conversation.WorkspaceID, conversation.ConversationID, conversation.Hours, conversation.Since).Scan(&quiet)
		if errors.Is(err, pgx.ErrNoRows) {
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
