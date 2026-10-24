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
	o.status,o.attempts,o.last_error,o.sent_at,t.id::text,t.label,t.scope,COALESCE(NULLIF(ta.display_name,''),ta.email),m.agent_request_id::text,m.reply_number,
	m.ai_draft_id::text,ad.outcome
	FROM messages m JOIN conversations c ON c.id=m.conversation_id AND c.workspace_id=m.workspace_id
	LEFT JOIN agents aa ON aa.workspace_id=m.workspace_id AND aa.id=m.author_agent_id
	LEFT JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
	LEFT JOIN channel_outbox o ON o.workspace_id=m.workspace_id AND o.message_id=m.id
	LEFT JOIN sessions t ON t.workspace_id=m.workspace_id AND t.id=m.agent_token_id
	LEFT JOIN agents ta ON ta.workspace_id=t.workspace_id AND ta.id=t.agent_id
	LEFT JOIN ai_drafts ad ON ad.workspace_id=m.workspace_id AND ad.id=m.ai_draft_id`

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
		&tokenID, &tokenLabel, &tokenScope, &tokenAgent, &message.AgentRequestID, &message.ReplyNumber, &message.AIDraftID, &message.AIDraftOutcome); err != nil {
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
	// AIDraftID is the AI draft a reply was written from; empty otherwise.
	AIDraftID string
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
	return s.CreateReply(ctx, workspaceID, conversationID, actor, kind, body, cc, nil, files...)
}

// CreateReply is CreateMessageWithCC for a sender who says which replies they had seen: with seen set,
// a reply (never a note) is refused with *domain.ReplyConflictError, and nothing stored, when the
// conversation has a reply numbered above seen. The numbering lock is held from the check to the insert
// (taken first, as every other writer of a reply takes it before any conversation row lock), so two
// replies sent at once with the same seen never both go.
func (s *Store) CreateReply(ctx context.Context, workspaceID, conversationID string, actor Actor, kind, body string, cc []string, seen *int, files ...NewAttachment) (domain.Message, error) {
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
	if kind == "outgoing" {
		if err := checkReplySeen(ctx, tx, workspaceID, conversationID, seen, true); err != nil {
			return domain.Message{}, err
		}
	}
	message, err := s.createMessage(ctx, tx, workspaceID, conversationID, actor, kind, body, cc, files)
	if err != nil {
		return domain.Message{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Message{}, fmt.Errorf("commit message creation: %w", err)
	}
	return message, nil
}

// createMessage writes a message, its files and its outbox row inside tx; see CreateMessageWithCC.
func (s *Store) createMessage(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, actor Actor, kind, body string, cc []string, files []NewAttachment) (domain.Message, error) {
	cc, err := withoutContact(ctx, tx, workspaceID, conversationID, cc)
	if err != nil {
		return domain.Message{}, err
	}
	var messageID string
	if actor.AIDraftID != "" && kind != "outgoing" {
		return domain.Message{}, domain.ErrAIDraftNotReady
	}
	// The draft is read and locked before the reply names it: another conversation's or another
	// workspace's id never reaches the insert.
	draft, err := lockAIDraft(ctx, tx, workspaceID, conversationID, actor.AIDraftID)
	if err != nil {
		return domain.Message{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO messages(workspace_id,conversation_id,author_agent_id,agent_token_id,agent_request_id,kind,body,cc,ai_draft_id)
		SELECT $1,c.id,NULLIF($3::text,'')::uuid,NULLIF($4,'')::uuid,NULLIF($8,'')::uuid,$5,$6,$7,NULLIF($9,'')::uuid FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2
		RETURNING id::text`, workspaceID, conversationID, actor.AgentID, actor.TokenID, kind, body, cc, actor.RequestID, actor.AIDraftID).Scan(&messageID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("create message: %w", err)
	}
	if kind == "outgoing" {
		if err := recordAIDraftSend(ctx, tx, workspaceID, conversationID, actor, messageID, draft, body); err != nil {
			return domain.Message{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, conversationID); err != nil {
		return domain.Message{}, fmt.Errorf("touch conversation: %w", err)
	}
	if err := s.insertMessageFiles(ctx, tx, workspaceID, messageID, files); err != nil {
		return domain.Message{}, err
	}
	if kind == "outgoing" {
		enqueue := s.enqueueReply
		if s.channels != nil {
			enqueue = s.enqueueChannelReply
		}
		if err := enqueue(ctx, tx, workspaceID, conversationID, messageID, cc); err != nil {
			return domain.Message{}, err
		}
	}
	return getMessage(ctx, tx, workspaceID, messageID)
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

// CheckReplySeen refuses early, without a lock, a reply whose sender had seen replies up to seen when
// the conversation has a newer one, so a reply with files stores none of them. CreateReply checks
// again under the lock.
func (s *Store) CheckReplySeen(ctx context.Context, workspaceID, conversationID string, seen int) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, checkReplySeen(ctx, tx, workspaceID, conversationID, &seen, false)
	})
	return err
}

// lockReplyNumbers takes the transaction-scoped advisory lock the numbering trigger takes for the
// conversation (migration 20261022140100-reply-numbers: the same key), so no reply is numbered in it
// until this transaction ends.
func lockReplyNumbers(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('reply_number:'||$1::uuid::text||':'||$2::uuid::text,0))`, workspaceID, conversationID); err != nil {
		return fmt.Errorf("lock reply numbers: %w", err)
	}
	return nil
}

// latestReplyNumberSQL is the number of the newest reply of conversation $2 of workspace $1, 0 before the first.
const latestReplyNumberSQL = `SELECT coalesce(max(reply_number),0) FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND reply_number IS NOT NULL`

// checkReplySeen reads the conversation's latest reply number, after taking the numbering lock when lock
// is set (it is then held until the reply is stored), and returns *domain.ReplyConflictError when seen is
// set and older. A conversation that is not in the workspace is pgx.ErrNoRows.
func checkReplySeen(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, seen *int, lock bool) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
		return fmt.Errorf("find conversation: %w", err)
	}
	if !exists {
		return fmt.Errorf("find conversation: %w", pgx.ErrNoRows)
	}
	if lock {
		if err := lockReplyNumbers(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
	}
	var latest int
	if err := tx.QueryRow(ctx, latestReplyNumberSQL, workspaceID, conversationID).Scan(&latest); err != nil {
		return fmt.Errorf("read latest reply number: %w", err)
	}
	if seen == nil || latest <= *seen {
		return nil
	}
	rows, err := tx.Query(ctx, messageSelect+` WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND m.kind='outgoing' AND m.reply_number>$3
		ORDER BY m.created_at,m.id`, workspaceID, conversationID, *seen)
	if err != nil {
		return fmt.Errorf("list newer replies: %w", err)
	}
	defer rows.Close()
	newer := []domain.Message{}
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return fmt.Errorf("scan newer reply: %w", err)
		}
		newer = append(newer, message)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list newer replies: %w", err)
	}
	return &domain.ReplyConflictError{Latest: latest, Newer: newer}
}
