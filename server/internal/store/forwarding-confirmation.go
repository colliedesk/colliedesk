package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// IncomingEmail is one email a channel received, as its message and conversation keep it.
type IncomingEmail struct {
	ConversationID string
	Subject        string
	Body           string
	From           string
	// Authentication is the parser's verdict on the trusted Authentication-Results: pass, fail or none.
	Authentication string
	ReceivedAt     time.Time
}

// LatestIncomingEmailFrom is the newest email from sender that reached the channel after since, or nil.
// It reads the workspace's own rows only, through the index of a channel's recent incoming messages.
func (s *Store) LatestIncomingEmailFrom(ctx context.Context, workspaceID, channelID, sender string, since time.Time) (*IncomingEmail, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*IncomingEmail, error) {
		var found IncomingEmail
		err := tx.QueryRow(ctx, `SELECT m.conversation_id::text, c.subject, m.body, m.email_meta->>'from', COALESCE(m.email_meta->>'authentication','none'), m.created_at
			FROM messages m JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			WHERE m.workspace_id=$1 AND m.channel_id=$2 AND m.kind='incoming' AND m.created_at>$3
				AND lower(m.email_meta->>'from')=lower($4)
			ORDER BY m.created_at DESC LIMIT 1`, workspaceID, channelID, since, sender).
			Scan(&found.ConversationID, &found.Subject, &found.Body, &found.From, &found.Authentication, &found.ReceivedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read latest email from %s: %w", sender, err)
		}
		found.ReceivedAt = domain.UTC(found.ReceivedAt)
		return &found, nil
	})
}
