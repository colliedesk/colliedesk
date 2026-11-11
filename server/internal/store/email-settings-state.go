package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// EmailChannelForwarding is one email channel of the workspace and whether customer mail has arrived
// through its support mailbox (the rule Delivery.Forwarded follows). OldHelpDesk is set for a channel added
// before an import that finished and was not undone: its mail was the old help desk's.
type EmailChannelForwarding struct {
	ChannelID, Name, SupportAddress string
	Enabled, Forwarded, OldHelpDesk bool
}

// EmailSettingsState is what an email channel's settings read besides the channel: the workspace's
// sending pause, whether it finished an import, and every email channel with its forwarding, the default
// channel first.
type EmailSettingsState struct {
	Pause    *domain.EmailPause
	Imported bool
	Channels []EmailChannelForwarding
}

// EmailSettingsState reads it in one query, so the settings cost no extra round trip per channel.
func (s *Store) EmailSettingsState(ctx context.Context, workspaceID string) (EmailSettingsState, error) {
	state, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (EmailSettingsState, error) {
		rows, err := tx.Query(ctx, `SELECT w.email_paused_at,w.email_pause_reason,`+importFinished+`,
				ch.id::text,ch.name,ec.support_address,ch.enabled,`+forwardedRule("ch.id")+`,`+importFinishedAfter("ch.created_at")+`
			FROM workspaces w
			LEFT JOIN (channels ch JOIN email_channels ec ON ec.workspace_id=ch.workspace_id AND ec.channel_id=ch.id)
				ON ch.workspace_id=w.id AND ch.type='email' AND ch.deleted_at IS NULL
			WHERE w.id=$1
			ORDER BY ch.is_default DESC,ch.created_at,ch.id`, workspaceID)
		if err != nil {
			return EmailSettingsState{}, err
		}
		defer rows.Close()
		var state EmailSettingsState
		for rows.Next() {
			var pausedAt *time.Time
			var reason, id, name, address *string
			var enabled *bool
			var forwarded, oldHelpDesk bool
			if err := rows.Scan(&pausedAt, &reason, &state.Imported, &id, &name, &address, &enabled, &forwarded, &oldHelpDesk); err != nil {
				return EmailSettingsState{}, err
			}
			if pausedAt != nil && reason != nil {
				state.Pause = &domain.EmailPause{PausedAt: pausedAt.UTC(), Reason: *reason}
			}
			if id != nil {
				state.Channels = append(state.Channels, EmailChannelForwarding{ChannelID: *id, Name: *name, SupportAddress: *address, Enabled: *enabled, Forwarded: forwarded, OldHelpDesk: oldHelpDesk})
			}
		}
		return state, rows.Err()
	})
	if err != nil {
		return EmailSettingsState{}, fmt.Errorf("read email settings state: %w", err)
	}
	return state, nil
}
