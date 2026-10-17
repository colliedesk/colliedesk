package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// ErrExportRunning means another export of the same workspace is in progress.
var ErrExportRunning = errors.New("an export of this workspace is already running")

// ExportLimits bound how long one export may hold its snapshot. Zero values use the defaults.
type ExportLimits struct {
	// IdleInTransaction ends the transaction when no statement runs for this long, for example
	// because the client stopped reading between two pages.
	IdleInTransaction time.Duration
	// Statement ends any single statement that runs longer.
	Statement time.Duration
}

const (
	defaultExportIdle      = 2 * time.Minute
	defaultExportStatement = 10 * time.Minute

	// A page of conversations stops growing at this many messages (at least one conversation).
	exportPageConversations = 200
	exportPageMessages      = 2000
)

// ExportWorkspace sends every record of one workspace to sink inside a single read-only
// REPEATABLE READ transaction, so the archive rows are one consistent snapshot. The transaction
// holds one pooled connection and a transaction-level advisory lock, so one export per workspace
// runs at a time and the lock can never outlive the connection. Messages are streamed row by row;
// memory is bounded by one page of conversations (at most about 2,000 messages) of metadata.
func (s *Store) ExportWorkspace(ctx context.Context, workspaceID string, sink export.Sink, limits ExportLimits) error {
	if limits.IdleInTransaction <= 0 {
		limits.IdleInTransaction = defaultExportIdle
	}
	if limits.Statement <= 0 {
		limits.Statement = defaultExportStatement
	}
	tx, err := s.db.BeginWorkspaceTx(ctx, workspaceID, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck
	// The database ends a transaction that a stalled client leaves open, whatever the server does.
	if _, err := tx.Exec(ctx, `SELECT set_config('idle_in_transaction_session_timeout',$1,true),set_config('statement_timeout',$2,true)`,
		fmt.Sprintf("%dms", limits.IdleInTransaction.Milliseconds()), fmt.Sprintf("%dms", limits.Statement.Milliseconds())); err != nil {
		return fmt.Errorf("bound the export transaction: %w", err)
	}
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))`, "export:"+workspaceID).Scan(&locked); err != nil {
		return fmt.Errorf("lock the export: %w", err)
	}
	if !locked {
		return ErrExportRunning
	}
	if err := exportWorkspaceRow(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportAgents(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportContacts(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportTags(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportSavedReplies(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportAgentRequests(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	return exportConversations(ctx, tx, workspaceID, sink)
}

func exportWorkspaceRow(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	var workspace domain.Workspace
	err := tx.QueryRow(ctx, `SELECT id::text,slug,name,timezone FROM workspaces WHERE id=$1`, workspaceID).
		Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone)
	if err != nil {
		return fmt.Errorf("export workspace: %w", err)
	}
	return sink.Workspace(workspace)
}

func exportAgents(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,email,display_name,role,timezone_override,created_at,deactivated_at
		FROM agents WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export agents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Agent
		if err := rows.Scan(&record.ID, &record.Email, &record.DisplayName, &record.Role, &record.TimezoneOverride, &record.CreatedAt, &record.DeactivatedAt); err != nil {
			return fmt.Errorf("export agents: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		record.DeactivatedAt = domain.UTCOptional(record.DeactivatedAt)
		if err := sink.Agent(record); err != nil {
			return err
		}
	}
	return rowsError("export agents", rows)
}

func exportContacts(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,name,email,organization,created_at,updated_at
		FROM contacts WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export contacts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Contact
		if err := rows.Scan(&record.ID, &record.Name, &record.Email, &record.Organization, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return fmt.Errorf("export contacts: %w", err)
		}
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		if err := sink.Contact(record); err != nil {
			return err
		}
	}
	return rowsError("export contacts", rows)
}

func exportTags(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,name,color,created_at FROM tags WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Tag
		if err := rows.Scan(&record.ID, &record.Name, &record.Color, &record.CreatedAt); err != nil {
			return fmt.Errorf("export tags: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.Tag(record); err != nil {
			return err
		}
	}
	return rowsError("export tags", rows)
}

func exportSavedReplies(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,title,body,created_by_agent_id::text,created_at,updated_at,deleted_at,deleted_by_agent_id::text
		FROM saved_replies WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export saved replies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.SavedReply
		if err := rows.Scan(&record.ID, &record.Title, &record.Body, &record.CreatedByAgentID, &record.CreatedAt, &record.UpdatedAt, &record.DeletedAt, &record.DeletedByAgentID); err != nil {
			return fmt.Errorf("export saved replies: %w", err)
		}
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.SavedReply(record); err != nil {
			return err
		}
	}
	if err := rowsError("export saved replies", rows); err != nil {
		return err
	}
	return exportSavedReplyVersions(ctx, tx, workspaceID, sink)
}

func exportSavedReplyVersions(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT saved_reply_id::text,version,kind,restored_from_version,title,body,changed_by_agent_id::text,token_id::text,token_label,agent_request_id::text,created_at
		FROM saved_reply_versions WHERE workspace_id=$1 ORDER BY saved_reply_id,version`, workspaceID)
	if err != nil {
		return fmt.Errorf("export saved reply versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.SavedReplyVersion
		if err := rows.Scan(&record.SavedReplyID, &record.Version, &record.Kind, &record.RestoredFromVersion, &record.Title, &record.Body,
			&record.ChangedByAgentID, &record.TokenID, &record.TokenLabel, &record.AgentRequestID, &record.CreatedAt); err != nil {
			return fmt.Errorf("export saved reply versions: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.SavedReplyVersion(record); err != nil {
			return err
		}
	}
	return rowsError("export saved reply versions", rows)
}

func exportAgentRequests(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,kind,status,method,route,path,body,edited_body,summary,conversation_id::text,token_id::text,token_label,token_scope,
		requested_by_agent_id::text,decided_by_agent_id::text,decided_at,reason,result_status,result_note,created_at,expires_at
		FROM agent_requests WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export agent requests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.AgentRequest
		var body, edited []byte
		if err := rows.Scan(&record.ID, &record.Kind, &record.Status, &record.Method, &record.Route, &record.Path, &body, &edited, &record.Summary,
			&record.ConversationID, &record.TokenID, &record.TokenLabel, &record.TokenScope, &record.RequestedByAgentID, &record.DecidedByAgentID,
			&record.DecidedAt, &record.Reason, &record.ResultStatus, &record.ResultNote, &record.CreatedAt, &record.ExpiresAt); err != nil {
			return fmt.Errorf("export agent requests: %w", err)
		}
		record.Body, record.EditedBody = body, edited
		record.CreatedAt, record.ExpiresAt, record.DecidedAt = domain.UTC(record.CreatedAt), domain.UTC(record.ExpiresAt), domain.UTCOptional(record.DecidedAt)
		if err := sink.AgentRequest(record); err != nil {
			return err
		}
	}
	return rowsError("export agent requests", rows)
}

func rowsError(what string, rows pgx.Rows) error {
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// exportConversations pages by id (keyset). Each page's small metadata (tags, files, time
// entries) is loaded in bulk; the messages, which can be large, stream row by row.
func exportConversations(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	after := "00000000-0000-0000-0000-000000000000"
	for {
		page, err := exportConversationPage(ctx, tx, workspaceID, after)
		if err != nil {
			return err
		}
		if len(page.heads) == 0 {
			return nil
		}
		for _, head := range page.heads {
			head.TagIDs = page.tags[head.ID]
			if err := sink.BeginConversation(head); err != nil {
				return err
			}
			if err := exportMessages(ctx, tx, workspaceID, head.ID, page.files, sink); err != nil {
				return err
			}
			if err := sink.EndConversation(page.timeEntries[head.ID]); err != nil {
				return err
			}
		}
		after = page.heads[len(page.heads)-1].ID
	}
}

type conversationPage struct {
	heads       []export.ConversationHead
	tags        map[string][]string
	files       map[string][]export.Attachment // by message id
	timeEntries map[string][]domain.TimeEntry  // by conversation id
}

func exportConversationPage(ctx context.Context, tx pgx.Tx, workspaceID, after string) (conversationPage, error) {
	page := conversationPage{tags: map[string][]string{}, files: map[string][]export.Attachment{}, timeEntries: map[string][]domain.TimeEntry{}}
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.contact_id::text,c.subject,c.status,c.priority,c.assignee_id::text,c.origin,c.created_at,c.updated_at,
		(SELECT count(*) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id)
		FROM conversations c WHERE c.workspace_id=$1 AND c.id>$2::uuid ORDER BY c.id LIMIT $3`, workspaceID, after, exportPageConversations)
	if err != nil {
		return page, fmt.Errorf("export conversations: %w", err)
	}
	total := 0
	for rows.Next() {
		var head export.ConversationHead
		var messages int
		if err := rows.Scan(&head.ID, &head.ContactID, &head.Subject, &head.Status, &head.Priority, &head.AssigneeID,
			&head.Origin, &head.CreatedAt, &head.UpdatedAt, &messages); err != nil {
			rows.Close()
			return page, fmt.Errorf("export conversations: %w", err)
		}
		if len(page.heads) > 0 && total+messages > exportPageMessages {
			break
		}
		total += messages
		head.CreatedAt, head.UpdatedAt = domain.UTC(head.CreatedAt), domain.UTC(head.UpdatedAt)
		head.TagIDs = []string{}
		page.heads = append(page.heads, head)
	}
	rows.Close()
	if err := rowsError("export conversations", rows); err != nil {
		return page, err
	}
	if len(page.heads) == 0 {
		return page, nil
	}
	ids := make([]string, len(page.heads))
	for index, head := range page.heads {
		ids[index] = head.ID
	}
	if err := exportPageTags(ctx, tx, workspaceID, ids, page.tags); err != nil {
		return page, err
	}
	if err := exportPageAttachments(ctx, tx, workspaceID, ids, page.files); err != nil {
		return page, err
	}
	return page, exportPageTimeEntries(ctx, tx, workspaceID, ids, page.timeEntries)
}

func exportPageTags(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, into map[string][]string) error {
	rows, err := tx.Query(ctx, `SELECT conversation_id::text,tag_id::text FROM conversation_tags
		WHERE workspace_id=$1 AND conversation_id=ANY($2::text[]::uuid[]) ORDER BY conversation_id,tag_id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("export conversation tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var conversationID, tagID string
		if err := rows.Scan(&conversationID, &tagID); err != nil {
			return fmt.Errorf("export conversation tags: %w", err)
		}
		into[conversationID] = append(into[conversationID], tagID)
	}
	return rowsError("export conversation tags", rows)
}

func exportPageAttachments(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, into map[string][]export.Attachment) error {
	rows, err := tx.Query(ctx, `SELECT a.id::text,a.message_id::text,a.filename,a.content_type,a.size,a.sha256,a.storage_key,a.created_at
		FROM attachments a JOIN messages m ON m.workspace_id=a.workspace_id AND m.id=a.message_id
		WHERE a.workspace_id=$1 AND m.conversation_id=ANY($2::text[]::uuid[]) ORDER BY a.created_at,a.id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("export attachments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Attachment
		var messageID string
		if err := rows.Scan(&record.ID, &messageID, &record.Filename, &record.ContentType, &record.Size, &record.SHA256, &record.StorageKey, &record.CreatedAt); err != nil {
			return fmt.Errorf("export attachments: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		into[messageID] = append(into[messageID], record)
	}
	return rowsError("export attachments", rows)
}

func exportPageTimeEntries(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, into map[string][]domain.TimeEntry) error {
	rows, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,agent_id::text,agent_name,minutes,note,billable,executed_at
		FROM time_entries WHERE workspace_id=$1 AND conversation_id=ANY($2::text[]::uuid[]) ORDER BY conversation_id,executed_at,id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("export time entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.TimeEntry
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.AgentID, &item.AgentName, &item.Minutes, &item.Note, &item.Billable, &item.ExecutedAt); err != nil {
			return fmt.Errorf("export time entries: %w", err)
		}
		item.ExecutedAt = domain.UTC(item.ExecutedAt)
		into[item.ConversationID] = append(into[item.ConversationID], item)
	}
	return rowsError("export time entries", rows)
}

// exportMessages streams one conversation's messages, one row in memory at a time.
func exportMessages(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, files map[string][]export.Attachment, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,author_agent_id::text,author_name,kind,body,body_html,origin,email_message_id,email_meta,cc,created_at,agent_request_id::text
		FROM messages WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY created_at,id`, workspaceID, conversationID)
	if err != nil {
		return fmt.Errorf("export messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Message
		var meta []byte
		if err := rows.Scan(&record.ID, &record.ConversationID, &record.AuthorAgentID, &record.AuthorName, &record.Kind, &record.Body,
			&record.BodyHTML, &record.Origin, &record.EmailMessageID, &meta, &record.CC, &record.CreatedAt, &record.AgentRequestID); err != nil {
			return fmt.Errorf("export messages: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		record.EmailMeta = meta
		record.Attachments = files[record.ID]
		if err := sink.Message(record); err != nil {
			return err
		}
	}
	return rowsError("export messages", rows)
}
