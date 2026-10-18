package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// messageSelect returns timeline rows with their email delivery (outgoing) and envelope (incoming).
// author_name is resolved on read: the stored name (imported messages) wins, then the authoring team
// member's display name or email, then the conversation's contact name when the contact sent the email.
// A copied colleague who answers the thread is not the contact, so that message keeps no name.
const messageSelect = `SELECT m.id::text,m.conversation_id::text,m.author_agent_id::text,
	COALESCE(m.author_name,NULLIF(aa.display_name,''),aa.email,
		CASE WHEN m.kind='incoming' AND lower(ct.email)=lower(m.email_meta->>'from') THEN NULLIF(ct.name,'') END),
	m.kind,m.body,m.body_html,m.origin,m.created_at,m.email_meta,m.cc,
	o.status,o.attempts,o.last_error,o.sent_at,t.id::text,t.label,t.scope,COALESCE(NULLIF(ta.display_name,''),ta.email),m.agent_request_id::text
	FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.workspace_id=m.workspace_id
	LEFT JOIN agents aa ON aa.workspace_id=m.workspace_id AND aa.id=m.author_agent_id
	LEFT JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
	LEFT JOIN email_outbox o ON o.workspace_id=m.workspace_id AND o.message_id=m.id
	LEFT JOIN sessions t ON t.workspace_id=m.workspace_id AND t.id=m.agent_token_id
	LEFT JOIN agents ta ON ta.workspace_id=t.workspace_id AND ta.id=t.agent_id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMessage(row rowScanner) (domain.Message, error) {
	var message domain.Message
	var meta []byte
	var status, lastError, tokenID, tokenLabel, tokenScope, tokenAgent *string
	var attempts *int32
	var sentAt *time.Time
	if err := row.Scan(&message.ID, &message.ConversationID, &message.AuthorAgentID, &message.AuthorName, &message.Kind, &message.Body,
		&message.BodyHTML, &message.Origin, &message.CreatedAt, &meta, &message.CC, &status, &attempts, &lastError, &sentAt,
		&tokenID, &tokenLabel, &tokenScope, &tokenAgent, &message.AgentRequestID); err != nil {
		return domain.Message{}, err
	}
	message.CreatedAt = domain.UTC(message.CreatedAt)
	if tokenID != nil && tokenLabel != nil && tokenScope != nil && tokenAgent != nil {
		message.AgentToken = &domain.MessageToken{ID: *tokenID, Label: *tokenLabel, Scope: *tokenScope, AgentName: *tokenAgent}
	}
	message.Attachments = []domain.Attachment{}
	if message.CC == nil {
		message.CC = []string{}
	}
	if status != nil && attempts != nil {
		message.Delivery = &domain.MessageDelivery{Status: *status, Attempts: int(*attempts), LastError: lastError, SentAt: domain.UTCOptional(sentAt)}
	}
	if meta != nil {
		var envelope domain.IncomingEmail
		if err := json.Unmarshal(meta, &envelope); err != nil {
			return domain.Message{}, fmt.Errorf("decode email metadata: %w", err)
		}
		if envelope.Attachments == nil {
			envelope.Attachments = []domain.EmailAttachment{}
		}
		message.Email = &envelope
	}
	return message, nil
}

func (s *Store) ListMessages(ctx context.Context, workspaceID, conversationID string) ([]domain.Message, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Message, error) {
		return listMessages(ctx, tx, workspaceID, conversationID)
	})
}

func listMessages(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) ([]domain.Message, error) {
	rows, err := tx.Query(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.conversation_id=$2 ORDER BY m.created_at,m.id`, workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}
	rows.Close()
	return messages, attachConversationFiles(ctx, tx, workspaceID, conversationID, messages)
}

type messageQuerier interface {
	Querier
	rowsQuerier
}

func getMessage(ctx context.Context, query messageQuerier, workspaceID, messageID string) (domain.Message, error) {
	message, err := scanMessage(query.QueryRow(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, messageID))
	if err != nil {
		return domain.Message{}, fmt.Errorf("get message: %w", err)
	}
	if err := attachMessageFiles(ctx, query, workspaceID, &message); err != nil {
		return domain.Message{}, err
	}
	return message, nil
}

// Actor is who wrote something: an agent, and the agent token they used when an AI agent wrote it
// for them. TokenID is empty for what a person did.
type Actor struct {
	AgentID string
	TokenID string
	// RequestID is the agent request whose approval writes the message; empty otherwise.
	RequestID string
}

// CreateMessage stores a reply or note with files already written to attachment storage (only
// Filename, ContentType and Object are read). A reply is queued for email when the workspace has an
// email channel; a note never is. When it fails, the caller owns the stored bytes.
func (s *Store) CreateMessage(ctx context.Context, workspaceID, conversationID string, actor Actor, kind, body string, files ...NewAttachment) (domain.Message, error) {
	return s.CreateMessageWithCC(ctx, workspaceID, conversationID, actor, kind, body, nil, files...)
}

// ErrCCOnNote means a Cc list came with a private note: only a reply is emailed.
var ErrCCOnNote = errors.New("only a reply can be copied to other addresses")

// CreateMessageWithCC is CreateMessage for a reply copied to the given addresses. The caller has
// validated and de-duplicated them; the list is stored with the reply so every delivery attempt
// sends to the same people. On the trial it may name only addresses the customer already wrote from
// or copied.
func (s *Store) CreateMessageWithCC(ctx context.Context, workspaceID, conversationID string, actor Actor, kind, body string, cc []string, files ...NewAttachment) (domain.Message, error) {
	if len(cc) > 0 && kind != "outgoing" {
		return domain.Message{}, ErrCCOnNote
	}
	if len(cc) == 0 {
		cc = nil
	}
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("begin message creation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if cc, err = withoutContact(ctx, tx, workspaceID, conversationID, cc); err != nil {
		return domain.Message{}, err
	}
	var messageID string
	err = tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,agent_token_id,agent_request_id,kind,body,cc)
		SELECT $1,c.id,$3,NULLIF($4,'')::uuid,NULLIF($8,'')::uuid,$5,$6,$7 FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2
		RETURNING id::text`, workspaceID, conversationID, actor.AgentID, actor.TokenID, kind, body, cc, actor.RequestID).Scan(&messageID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("create message: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
		return domain.Message{}, fmt.Errorf("touch conversation: %w", err)
	}
	if err := s.insertMessageFiles(ctx, tx, workspaceID, messageID, files); err != nil {
		return domain.Message{}, err
	}
	if kind == "outgoing" {
		if err := s.enqueueReply(ctx, tx, workspaceID, conversationID, messageID, cc); err != nil {
			return domain.Message{}, err
		}
	}
	message, err := getMessage(ctx, tx, workspaceID, messageID)
	if err != nil {
		return domain.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, fmt.Errorf("commit message creation: %w", err)
	}
	return message, nil
}

func (s *Store) insertMessageFiles(ctx context.Context, tx pgx.Tx, workspaceID, messageID string, files []NewAttachment) error {
	if len(files) == 0 {
		return nil
	}
	var total int64
	for _, file := range files {
		total += file.Object.Size
	}
	if err := s.CheckStorage(ctx, tx, workspaceID, total, s.now()); err != nil {
		return err
	}
	keys := make([]string, 0, len(files))
	for _, file := range files {
		keys = append(keys, file.Object.Key)
	}
	if err := VerifyObjects(ctx, tx, s.objects, keys...); err != nil {
		return err
	}
	for _, file := range files {
		file.MessageID = messageID
		if _, err := InsertAttachment(ctx, tx, workspaceID, file); err != nil {
			return err
		}
	}
	return nil
}
