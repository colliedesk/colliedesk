package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// Inbound limits: a loop guard per sender address and an abuse ceiling on new conversations per
// workspace. The sender is the header From, which a sender controls, so the workspace ceiling is the
// bound that holds when From rotates.
const (
	InboundRateLimit            = 10
	InboundNewConversationLimit = 200
	InboundRateWindow           = 10 * time.Minute
	bounceReason                = "The recipient's mail server bounced this reply."
)

// ErrRateLimited means a sender or the workspace exceeded an inbound limit in InboundRateWindow.
var ErrRateLimited = errors.New("inbound email rate limit reached")

// InboundResult says what happened to one inbound message.
type InboundResult struct {
	Status         string  `json:"status"`
	Reason         *string `json:"reason"`
	ConversationID *string `json:"conversation_id"`
	MessageID      *string `json:"message_id"`
	Authentication string  `json:"authentication"`
}

// EmailFile is a file of an inbound message already written to attachment storage.
type EmailFile struct {
	Filename    string
	ContentType string
	// Original marks the stored raw message; its attachment id is recorded in the email metadata.
	Original bool
	// Inline marks an image whose bytes were embedded in the message's HTML.
	Inline bool
	Object attachments.Object
}

// WorkspaceForReplyToken finds the workspace of the conversation a reply token belongs to.
func (s *Store) WorkspaceForReplyToken(ctx context.Context, token string) (string, bool, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeReplyToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM conversations WHERE email_reply_token=$1`, token).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find reply token: %w", err)
	}
	return workspaceID, true, nil
}

// WorkspaceForMailSlug finds the workspace whose slug is the mail subdomain of a hosted address.
func (s *Store) WorkspaceForMailSlug(ctx context.Context, slug string) (string, bool, error) {
	workspaceID, err := s.db.WorkspaceIDBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return workspaceID, true, nil
}
