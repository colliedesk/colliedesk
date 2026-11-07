package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// A quiet mailbox (docs/user/email-channel.md "When no email arrives"): an email channel that usually
// gets mail every day and has had none for a day. Days are 24-hour periods counted back from 24 hours
// before now, so no timezone decides them.
const (
	// QuietAfter is how long without mail makes a busy mailbox quiet.
	QuietAfter = 24 * time.Hour
	// QuietBusyDays of the QuietLookbackDays before the silence must each have had mail: a channel with
	// little or new traffic never counts as quiet.
	QuietBusyDays     = 5
	QuietLookbackDays = 7
)

// QuietCandidate is an email channel whose last email, at Since, began a silence of QuietAfter or more.
type QuietCandidate struct {
	WorkspaceID, ChannelID string
	Since                  time.Time
}

// QuietCandidates lists, in every workspace, the live email channels that are quiet at now. A silence
// is noticed while the busy days still fit the look-back window, so a mailbox abandoned long ago is never
// reported.
func (s *Store) QuietCandidates(ctx context.Context, now time.Time) ([]QuietCandidate, error) {
	var found []QuietCandidate
	err := s.db.Privileged(ctx, database.PrivilegeQuietMailbox, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT c.workspace_id::text,c.id::text,l.last FROM channels c
			CROSS JOIN LATERAL (SELECT max(e.received_at) AS last FROM channel_inbound_events e WHERE e.workspace_id=c.workspace_id AND e.channel_id=c.id) l
			WHERE c.type=$4 AND c.enabled AND c.deleted_at IS NULL
				AND l.last <= $1::timestamptz-$2::interval AND l.last > $1::timestamptz-$2::interval-make_interval(days => $3)
				AND (SELECT count(DISTINCT floor(extract(epoch FROM ($1::timestamptz-$2::interval-e.received_at))/86400)) FROM channel_inbound_events e
					WHERE e.workspace_id=c.workspace_id AND e.channel_id=c.id
						AND e.received_at > $1::timestamptz-$2::interval-make_interval(days => $3) AND e.received_at <= $1::timestamptz-$2::interval) >= $5
			ORDER BY l.last`, now, QuietAfter, QuietLookbackDays, domain.ChannelEmail, QuietBusyDays)
		if err != nil {
			return err
		}
		found, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (QuietCandidate, error) {
			var c QuietCandidate
			err := row.Scan(&c.WorkspaceID, &c.ChannelID, &c.Since)
			return c, err
		})
		return err
	}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("list quiet mailboxes: %w", err)
	}
	return found, nil
}

// QuietSpell is a recorded silence of one channel, with what its admins' email needs.
type QuietSpell struct {
	QuietCandidate
	WorkspaceName, Slug, Timezone string
	SupportAddress, HostedLocal   string
	// Recipients are the admins this pass claimed the email for.
	Recipients []QuietRecipient
}

// QuietRecipient is one admin who gets the email.
type QuietRecipient struct{ AgentID, Email string }

// OpenQuietSpell records the silence of a candidate in its own workspace once it is checked again (the
// channel is still live and on, and no email came since), and, when claim is set, claims the email of
// every active admin who can change channels and has not had it for this spell. It returns nil when the
// channel is no longer quiet.
func (s *Store) OpenQuietSpell(ctx context.Context, candidate QuietCandidate, now time.Time, claim bool) (*QuietSpell, error) {
	return scoped(ctx, s, candidate.WorkspaceID, func(tx pgx.Tx) (*QuietSpell, error) {
		spell := QuietSpell{QuietCandidate: candidate}
		var current bool
		err := tx.QueryRow(ctx, `SELECT c.enabled AND c.deleted_at IS NULL AND (SELECT max(received_at) FROM channel_inbound_events e
				WHERE e.workspace_id=c.workspace_id AND e.channel_id=c.id)=$3, w.name, w.slug, w.timezone
			FROM channels c JOIN workspaces w ON w.id=c.workspace_id WHERE c.workspace_id=$1 AND c.id=$2`,
			candidate.WorkspaceID, candidate.ChannelID, candidate.Since).Scan(&current, &spell.WorkspaceName, &spell.Slug, &spell.Timezone)
		if err == pgx.ErrNoRows || (err == nil && !current) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("check quiet mailbox: %w", err)
		}
		settings, err := EmailChannelTx(ctx, tx, candidate.WorkspaceID, candidate.ChannelID)
		if err != nil || settings == nil {
			return nil, err
		}
		spell.SupportAddress, spell.HostedLocal = settings.SupportAddress, settings.HostedLocal
		if _, err := tx.Exec(ctx, `INSERT INTO channel_quiet_spells(workspace_id,channel_id,since,detected_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			candidate.WorkspaceID, candidate.ChannelID, candidate.Since, now); err != nil {
			return nil, fmt.Errorf("record quiet mailbox: %w", err)
		}
		if !claim {
			return &spell, nil
		}
		rows, err := tx.Query(ctx, `INSERT INTO channel_quiet_emails(workspace_id,channel_id,since,agent_id,status,updated_at)
			SELECT a.workspace_id,$2,$3,a.id,'claimed',$4 FROM agents a WHERE a.workspace_id=$1 AND a.deactivated_at IS NULL AND `+HoldsPermission("$5")+`
			ON CONFLICT DO NOTHING RETURNING agent_id::text,(SELECT lower(x.email) FROM agents x WHERE x.workspace_id=$1 AND x.id=agent_id)`,
			candidate.WorkspaceID, candidate.ChannelID, candidate.Since, now, "channels:update")
		if err != nil {
			return nil, fmt.Errorf("claim quiet mailbox emails: %w", err)
		}
		spell.Recipients, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (QuietRecipient, error) {
			var r QuietRecipient
			return r, row.Scan(&r.AgentID, &r.Email)
		})
		return &spell, err
	})
}

// SettleQuietEmail records how one admin's email ended: sent or rejected; "" gives the claim back for a
// later pass (a send that failed for now).
func (s *Store) SettleQuietEmail(ctx context.Context, spell QuietSpell, agentID, status string, now time.Time) error {
	_, err := scoped(ctx, s, spell.WorkspaceID, func(tx pgx.Tx) (struct{}, error) {
		key := []any{spell.WorkspaceID, spell.ChannelID, spell.Since, agentID}
		if status == "" {
			_, err := tx.Exec(ctx, `DELETE FROM channel_quiet_emails WHERE workspace_id=$1 AND channel_id=$2 AND since=$3 AND agent_id=$4`, key...)
			return struct{}{}, err
		}
		_, err := tx.Exec(ctx, `UPDATE channel_quiet_emails SET status=$5, updated_at=$6 WHERE workspace_id=$1 AND channel_id=$2 AND since=$3 AND agent_id=$4`,
			append(key, status, now)...)
		return struct{}{}, err
	})
	return err
}

// QuietSinceTx is the time of the last email of a channel in a recorded silence that no email has ended
// yet; nil when the channel is not quiet.
func QuietSinceTx(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (*time.Time, error) {
	var since time.Time
	err := tx.QueryRow(ctx, `SELECT q.since FROM channel_quiet_spells q WHERE q.workspace_id=$1 AND q.channel_id=$2
		AND q.since=(SELECT max(e.received_at) FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.channel_id=$2)`,
		workspaceID, channelID).Scan(&since)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read quiet mailbox: %w", err)
	}
	since = since.UTC()
	return &since, nil
}
