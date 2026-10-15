package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

// findThread matches In-Reply-To, then References (newest first), then the reply token; all within the
// workspace. The candidate lists are capped by the parser and resolved in one query.
func findThread(ctx context.Context, tx pgx.Tx, workspaceID string, in email.Inbound) (string, error) {
	candidates := append([]string{}, in.InReplyTo...)
	for index := len(in.References) - 1; index >= 0; index-- {
		candidates = append(candidates, in.References[index])
	}
	if len(candidates) > 0 {
		rows, err := tx.Query(ctx, `SELECT email_message_id,conversation_id::text FROM messages
			WHERE workspace_id=$1 AND email_message_id=ANY($2)`, workspaceID, candidates)
		if err != nil {
			return "", fmt.Errorf("match thread references: %w", err)
		}
		found := map[string]string{}
		for rows.Next() {
			var id, conversationID string
			if err := rows.Scan(&id, &conversationID); err != nil {
				rows.Close()
				return "", fmt.Errorf("scan thread reference: %w", err)
			}
			found[id] = conversationID
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("iterate thread references: %w", err)
		}
		for _, candidate := range candidates {
			if conversationID, ok := found[candidate]; ok {
				return conversationID, nil
			}
		}
	}
	token := email.ReplyToken(in.Recipients)
	if token == "" {
		return "", nil
	}
	var conversationID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND email_reply_token=$2`, workspaceID, token).Scan(&conversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("match reply token: %w", err)
	}
	return conversationID, nil
}

// markBounced fails the deliveries of our replies named in a delivery status report.
func markBounced(ctx context.Context, tx pgx.Tx, workspaceID string, messageIDs []string) error {
	if _, err := tx.Exec(ctx, `UPDATE email_outbox o SET status='failed',last_error=$3,locked_until=NULL,claim_token=NULL,updated_at=now()
		FROM messages m WHERE o.workspace_id=$1 AND m.workspace_id=o.workspace_id AND m.id=o.message_id
		AND m.kind='outgoing' AND m.email_message_id=ANY($2)`, workspaceID, messageIDs, bounceReason); err != nil {
		return fmt.Errorf("record bounce: %w", err)
	}
	return nil
}

// createEmailConversation stores a new conversation received at now.
func createEmailConversation(ctx context.Context, tx pgx.Tx, workspaceID string, in email.Inbound, now time.Time) (string, error) {
	name := in.FromName
	if name == "" {
		name = in.FromAddress[:strings.LastIndex(in.FromAddress, "@")]
	}
	var contactID string
	err := tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,$3)
		ON CONFLICT(workspace_id,email) DO UPDATE SET email=contacts.email RETURNING id::text`, workspaceID, name, in.FromAddress).Scan(&contactID)
	if err != nil {
		return "", fmt.Errorf("find or create contact: %w", err)
	}
	subject := in.Subject
	if subject == "" {
		subject = "(no subject)"
	}
	var conversationID string
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at,updated_at) VALUES($1,$2,$3,$4,$4) RETURNING id::text`,
		workspaceID, contactID, subject, now).Scan(&conversationID); err != nil {
		return "", fmt.Errorf("create conversation: %w", err)
	}
	return conversationID, nil
}

// linkFiles records stored files as attachments of the message and points the email metadata at the
// stored original.
func linkFiles(ctx context.Context, tx pgx.Tx, workspaceID, messageID string, files []EmailFile) error {
	for _, file := range files {
		item, err := InsertAttachment(ctx, tx, workspaceID, NewAttachment{MessageID: messageID, Filename: file.Filename,
			ContentType: file.ContentType, Object: file.Object})
		if err != nil {
			return err
		}
		if file.Original {
			if _, err := tx.Exec(ctx, `UPDATE messages SET email_meta=email_meta||jsonb_build_object('original_attachment_id',$3::text)
				WHERE workspace_id=$1 AND id=$2`, workspaceID, messageID, item.ID); err != nil {
				return fmt.Errorf("link original message: %w", err)
			}
		}
	}
	return nil
}
