package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// gmailReadScope is the scope a grant needs for its inbox to be read (google.ScopeGmailRead; the store
// does not import the provider package).
const gmailReadScope = "https://www.googleapis.com/auth/gmail.readonly"

// readableMailboxAs is the condition of a connection whose inbox the reader reads, its table named alias.
func readableMailboxAs(alias string) string {
	return alias + `.provider='google' AND ` + alias + `.status='connected' AND ` + alias + `.inbox_error_code IS NULL
	AND ' ' || ` + alias + `.scopes || ' ' LIKE '% ` + gmailReadScope + ` %'`
}

// MailboxesToRead lists, in every workspace, up to limit connected Gmail mailboxes whose grant may read the
// inbox and whose reading was not refused, the least recently read first (never read before any).
func (s *Store) MailboxesToRead(ctx context.Context, limit int) ([]MailboxRef, error) {
	var due []MailboxRef
	err := s.db.Privileged(ctx, database.PrivilegeMailboxInboxRead, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT m.workspace_id::text,m.channel_id::text FROM mailbox_connections m WHERE `+readableMailboxAs("m")+`
			ORDER BY m.inbox_read_at NULLS FIRST, m.connected_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, pgx.RowToStructByPos[MailboxRef])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list mailboxes to read: %w", err)
	}
	return due, nil
}

// MailboxInbox is a connection whose inbox is read: its grant and where the last read stopped.
type MailboxInbox struct {
	Grant SealedGrant
	// ConnectedAt names this connection: a reconnect changes it, so a read that raced it stores nothing.
	ConnectedAt time.Time
	// HistoryID is where the next read starts; empty before the first read.
	HistoryID string
	// ReadAt is when the last read finished; nil before the first.
	ReadAt *time.Time
	// SupportAddress is the email channel's support address, which decides what of the inbox is the channel's.
	SupportAddress string
}

// ReadableMailbox returns the channel's connection when its inbox is to be read, or nil when it is not
// (removed, failed, sending only, or refused since it was listed).
func (s *Store) ReadableMailbox(ctx context.Context, workspaceID, channelID string) (*MailboxInbox, error) {
	inbox, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MailboxInbox, error) {
		var inbox MailboxInbox
		var history *string
		err := tx.QueryRow(ctx, `SELECT m.token_key_id,m.refresh_token_sealed,m.address,m.provider,m.connected_at,m.inbox_history_id,m.inbox_read_at,e.support_address
			FROM mailbox_connections m JOIN email_channels e ON e.workspace_id=m.workspace_id AND e.channel_id=m.channel_id
			WHERE m.workspace_id=$1 AND m.channel_id::text=$2 AND `+readableMailboxAs("m"), workspaceID, channelID).
			Scan(&inbox.Grant.KeyID, &inbox.Grant.Sealed, &inbox.Grant.Address, &inbox.Grant.Provider, &inbox.ConnectedAt, &history, &inbox.ReadAt, &inbox.SupportAddress)
		if history != nil {
			inbox.HistoryID = *history
		}
		return inbox, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read mailbox inbox state: %w", err)
	}
	inbox.ReadAt = domain.UTCOptional(inbox.ReadAt)
	return &inbox, nil
}

// SaveInboxRead records that every message before historyID is stored, at readAt, only while the
// connection read is still the one stored (a reconnect since starts again). It reports whether it saved.
func (s *Store) SaveInboxRead(ctx context.Context, workspaceID, channelID string, connectedAt time.Time, historyID string, readAt time.Time) (bool, error) {
	saved, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `UPDATE mailbox_connections SET inbox_history_id=$4,inbox_read_at=$5
			WHERE workspace_id=$1 AND channel_id::text=$2 AND connected_at=$3`, workspaceID, channelID, connectedAt, historyID, readAt)
		return result.RowsAffected() == 1, err
	})
	if err != nil {
		return false, fmt.Errorf("save inbox read: %w", err)
	}
	return saved, nil
}

// RefuseInboxRead stops reading the connection's inbox with the reason (domain.MailboxPermission), while
// it still is the connection read; sending is untouched. Reconnecting clears it.
func (s *Store) RefuseInboxRead(ctx context.Context, workspaceID, channelID string, connectedAt time.Time, code string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		_, err := tx.Exec(ctx, `UPDATE mailbox_connections SET inbox_error_code=$4 WHERE workspace_id=$1 AND channel_id::text=$2 AND connected_at=$3`,
			workspaceID, channelID, connectedAt, code)
		return struct{}{}, err
	})
	if err != nil {
		return fmt.Errorf("refuse inbox read: %w", err)
	}
	return nil
}
