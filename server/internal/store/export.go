package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// ErrExportRunning means another export of the same workspace is in progress.
var ErrExportRunning = errors.New("an export of this workspace is already running")

const exportPageSize = 200

// ExportWorkspace sends every record of one workspace to sink inside a single read-only
// REPEATABLE READ transaction, so the archive is one consistent snapshot. Memory stays bounded by
// one page of conversations. One export per workspace runs at a time.
func (s *Store) ExportWorkspace(ctx context.Context, workspaceID string, sink export.Sink) error {
	release, err := s.db.TryAdvisoryLock(ctx, "export:"+workspaceID)
	if errors.Is(err, database.ErrLockHeld) {
		return ErrExportRunning
	}
	if err != nil {
		return err
	}
	defer release()
	tx, err := s.db.BeginWorkspaceTx(ctx, workspaceID, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck
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
	rows, err := tx.Query(ctx, `SELECT id::text,email,display_name,role,timezone_override,created_at
		FROM agents WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export agents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Agent
		if err := rows.Scan(&record.ID, &record.Email, &record.DisplayName, &record.Role, &record.TimezoneOverride, &record.CreatedAt); err != nil {
			return fmt.Errorf("export agents: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
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

func rowsError(what string, rows pgx.Rows) error {
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// exportConversations pages by id (keyset), so a page is read completely before the queries that
// hang messages, files, tags and time entries off it.
func exportConversations(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	after := "00000000-0000-0000-0000-000000000000"
	for {
		page, err := exportConversationPage(ctx, tx, workspaceID, after)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		for index := range page {
			if err := sink.Conversation(page[index]); err != nil {
				return err
			}
		}
		after = page[len(page)-1].ID
		if len(page) < exportPageSize {
			return nil
		}
	}
}

func exportConversationPage(ctx context.Context, tx pgx.Tx, workspaceID, after string) ([]export.Conversation, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,contact_id::text,subject,status,priority,assignee_id::text,origin,created_at,updated_at
		FROM conversations WHERE workspace_id=$1 AND id>$2::uuid ORDER BY id LIMIT $3`, workspaceID, after, exportPageSize)
	if err != nil {
		return nil, fmt.Errorf("export conversations: %w", err)
	}
	page := make([]export.Conversation, 0, exportPageSize)
	index := map[string]int{}
	ids := make([]string, 0, exportPageSize)
	for rows.Next() {
		var record export.Conversation
		if err := rows.Scan(&record.ID, &record.ContactID, &record.Subject, &record.Status, &record.Priority, &record.AssigneeID,
			&record.Origin, &record.CreatedAt, &record.UpdatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("export conversations: %w", err)
		}
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		index[record.ID] = len(page)
		ids = append(ids, record.ID)
		page = append(page, record)
	}
	rows.Close()
	if err := rowsError("export conversations", rows); err != nil {
		return nil, err
	}
	if len(page) == 0 {
		return page, nil
	}
	if err := exportPageTags(ctx, tx, workspaceID, ids, page, index); err != nil {
		return nil, err
	}
	if err := exportPageMessages(ctx, tx, workspaceID, ids, page, index); err != nil {
		return nil, err
	}
	if err := exportPageTimeEntries(ctx, tx, workspaceID, ids, page, index); err != nil {
		return nil, err
	}
	return page, nil
}

func exportPageTags(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, page []export.Conversation, index map[string]int) error {
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
		position := index[conversationID]
		page[position].TagIDs = append(page[position].TagIDs, tagID)
	}
	return rowsError("export conversation tags", rows)
}

func exportPageMessages(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, page []export.Conversation, index map[string]int) error {
	rows, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,author_agent_id::text,author_name,kind,body,body_html,origin,email_message_id,email_meta,created_at
		FROM messages WHERE workspace_id=$1 AND conversation_id=ANY($2::text[]::uuid[]) ORDER BY conversation_id,created_at,id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("export messages: %w", err)
	}
	messageAt := map[string][2]int{}
	for rows.Next() {
		var record export.Message
		var meta []byte
		if err := rows.Scan(&record.ID, &record.ConversationID, &record.AuthorAgentID, &record.AuthorName, &record.Kind, &record.Body,
			&record.BodyHTML, &record.Origin, &record.EmailMessageID, &meta, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export messages: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		record.EmailMeta = meta
		record.Attachments = []export.Attachment{}
		position := index[record.ConversationID]
		messageAt[record.ID] = [2]int{position, len(page[position].Messages)}
		page[position].Messages = append(page[position].Messages, record)
	}
	rows.Close()
	if err := rowsError("export messages", rows); err != nil {
		return err
	}
	return exportPageAttachments(ctx, tx, workspaceID, ids, page, messageAt)
}

func exportPageAttachments(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, page []export.Conversation, messageAt map[string][2]int) error {
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
		at := messageAt[messageID]
		message := &page[at[0]].Messages[at[1]]
		message.Attachments = append(message.Attachments, record)
	}
	return rowsError("export attachments", rows)
}

func exportPageTimeEntries(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, page []export.Conversation, index map[string]int) error {
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
		position := index[item.ConversationID]
		page[position].TimeEntries = append(page[position].TimeEntries, item)
	}
	return rowsError("export time entries", rows)
}
