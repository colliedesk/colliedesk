package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
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
	if err := exportContactMerges(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportTags(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportSavedReplies(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportKB(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportAgentRequests(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportImportJobs(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportImportPeople(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportAI(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportResultsBaseline(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := exportAutofix(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	if err := s.exportChannels(ctx, tx, workspaceID, sink); err != nil {
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
	rows, err := tx.Query(ctx, `SELECT id::text,name,email,organization,merged_into_id::text,created_at,updated_at,
		(SELECT COALESCE(json_agg(json_build_object('channel_id',i.channel_id,'kind',i.kind,'value',i.value,'display_name',i.display_name)
			ORDER BY i.created_at,i.id),'[]') FROM contact_identities i WHERE i.workspace_id=contacts.workspace_id AND i.contact_id=contacts.id)
		FROM contacts WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export contacts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Contact
		var identities []byte
		if err := rows.Scan(&record.ID, &record.Name, &record.Email, &record.Organization, &record.MergedIntoID, &record.CreatedAt, &record.UpdatedAt, &identities); err != nil {
			return fmt.Errorf("export contacts: %w", err)
		}
		if err := json.Unmarshal(identities, &record.Identities); err != nil {
			return fmt.Errorf("export contact identities: %w", err)
		}
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		if err := sink.Contact(record); err != nil {
			return err
		}
	}
	return rowsError("export contacts", rows)
}

func exportContactMerges(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,contact_id::text,merged_contact_id::text,conversation_ids::text[],merged_by_agent_id::text,
		token_id::text,created_at,undone_at,undone_by_agent_id::text FROM contact_merges WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export contact merges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.ContactMerge
		if err := rows.Scan(&record.ID, &record.ContactID, &record.MergedContactID, &record.ConversationIDs, &record.MergedByAgentID,
			&record.TokenID, &record.CreatedAt, &record.UndoneAt, &record.UndoneByAgentID); err != nil {
			return fmt.Errorf("export contact merges: %w", err)
		}
		record.CreatedAt, record.UndoneAt = domain.UTC(record.CreatedAt), domain.UTCOptional(record.UndoneAt)
		if err := sink.ContactMerge(record); err != nil {
			return err
		}
	}
	return rowsError("export contact merges", rows)
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
	if err := exportSavedReplyVersions(ctx, tx, workspaceID, sink); err != nil {
		return err
	}
	return exportSavedViews(ctx, tx, workspaceID, sink)
}

// exportSavedViews writes every view of the workspace, every person's personal views and the trash
// included: the export is the workspace's whole record, and only an admin downloads it.
func exportSavedViews(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,name,scope,owner_agent_id::text,filters,created_by_agent_id::text,created_at,updated_at,deleted_at
		FROM saved_views WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export saved views: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.SavedView
		var filters []byte
		if err := rows.Scan(&record.ID, &record.Name, &record.Scope, &record.OwnerAgentID, &filters, &record.CreatedByAgentID,
			&record.CreatedAt, &record.UpdatedAt, &record.DeletedAt); err != nil {
			return fmt.Errorf("export saved views: %w", err)
		}
		record.Filters = filters
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.SavedView(record); err != nil {
			return err
		}
	}
	return rowsError("export saved views", rows)
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
	rows, err := tx.Query(ctx, `SELECT id::text,kind,status,method,route,path,body,edited_body,summary,conversation_id::text,token_id::text,token_label,token_scope,COALESCE(token_permissions,'{}'),
		requested_by_agent_id::text,decided_by_agent_id::text,decided_at,reason,result_status,result_note,created_at,expires_at,saved_reply_id::text,saved_reply_before
		FROM agent_requests WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export agent requests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.AgentRequest
		var body, edited, replyBefore []byte
		if err := rows.Scan(&record.ID, &record.Kind, &record.Status, &record.Method, &record.Route, &record.Path, &body, &edited, &record.Summary,
			&record.ConversationID, &record.TokenID, &record.TokenLabel, &record.TokenScope, &record.TokenPermissions, &record.RequestedByAgentID, &record.DecidedByAgentID,
			&record.DecidedAt, &record.Reason, &record.ResultStatus, &record.ResultNote, &record.CreatedAt, &record.ExpiresAt, &record.SavedReplyID, &replyBefore); err != nil {
			return fmt.Errorf("export agent requests: %w", err)
		}
		record.Body, record.EditedBody, record.SavedReplyBefore = body, edited, replyBefore
		record.CreatedAt, record.ExpiresAt, record.DecidedAt = domain.UTC(record.CreatedAt), domain.UTC(record.ExpiresAt), domain.UTCOptional(record.DecidedAt)
		if err := sink.AgentRequest(record); err != nil {
			return err
		}
	}
	return rowsError("export agent requests", rows)
}

// exportImportJobs writes the in-app imports; the sealed key is never selected.
func exportImportJobs(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,kind,source,domain,account_source,undo_of::text,state,phase,options,preview,interim_report,report,status,
		error_code,error_message,created_by::text,created_at,started_at,finished_at
		FROM import_jobs WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export import jobs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.ImportJob
		var options, preview, interim, report []byte
		if err := rows.Scan(&record.ID, &record.Kind, &record.Source, &record.Domain, &record.AccountSource, &record.UndoOf, &record.State, &record.Phase,
			&options, &preview, &interim, &report, &record.Status, &record.ErrorCode, &record.ErrorMessage, &record.CreatedBy,
			&record.CreatedAt, &record.StartedAt, &record.FinishedAt); err != nil {
			return fmt.Errorf("export import jobs: %w", err)
		}
		record.Options, record.Preview, record.InterimReport, record.Report = options, preview, interim, report
		record.CreatedAt, record.StartedAt, record.FinishedAt = domain.UTC(record.CreatedAt), domain.UTCOptional(record.StartedAt), domain.UTCOptional(record.FinishedAt)
		if err := sink.ImportJob(record); err != nil {
			return err
		}
	}
	return rowsError("export import jobs", rows)
}

func exportImportPeople(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT source,external_id,name,email,active,linked_agent_id::text,linked_by_email,chosen_by_person,created_at,updated_at
		FROM import_people WHERE workspace_id=$1 ORDER BY source,external_id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export import people: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.ImportPerson
		if err := rows.Scan(&record.Source, &record.ExternalID, &record.Name, &record.Email, &record.Active, &record.LinkedAgentID, &record.LinkedByEmail,
			&record.ChosenByPerson, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return fmt.Errorf("export import people: %w", err)
		}
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		if err := sink.ImportPerson(record); err != nil {
			return err
		}
	}
	return rowsError("export import people", rows)
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
			if err := sink.EndConversation(page.timeEntries[head.ID], page.statusChanges[head.ID]); err != nil {
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
	// statusChanges by conversation id, oldest first.
	statusChanges map[string][]export.StatusChange
}

func exportConversationPage(ctx context.Context, tx pgx.Tx, workspaceID, after string) (conversationPage, error) {
	page := conversationPage{tags: map[string][]string{}, files: map[string][]export.Attachment{}, timeEntries: map[string][]domain.TimeEntry{}, statusChanges: map[string][]export.StatusChange{}}
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.contact_id::text,c.subject,c.status,c.priority,c.assignee_id::text,c.channel,c.channel_id::text,c.channel_scope,c.reply_channel_id::text,c.merged_into_id::text,c.origin,c.created_at,c.updated_at,
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
			&head.Channel, &head.ChannelID, &head.ChannelScope, &head.ReplyChannelID, &head.MergedIntoID, &head.Origin, &head.CreatedAt, &head.UpdatedAt, &messages); err != nil {
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
	if err := exportPageTimeEntries(ctx, tx, workspaceID, ids, page.timeEntries); err != nil {
		return page, err
	}
	return page, exportPageStatusChanges(ctx, tx, workspaceID, ids, page.statusChanges)
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
	rows, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,author_agent_id::text,author_name,kind,body,body_html,origin,CASE WHEN starts_with(external_id,'email:') THEN substr(external_id,7) END,email_meta,cc,created_at,agent_request_id::text,ai_draft_id::text,
		channel_id::text,external_id,channel_meta,
		(SELECT e.decision_basis FROM channel_inbound_events e WHERE e.workspace_id=m.workspace_id AND e.message_id=m.id AND e.decision_basis IS NOT NULL LIMIT 1),
		(SELECT e.decision_rule FROM channel_inbound_events e WHERE e.workspace_id=m.workspace_id AND e.message_id=m.id AND e.decision_rule IS NOT NULL LIMIT 1),
		(SELECT e.command FROM channel_inbound_events e WHERE e.workspace_id=m.workspace_id AND e.message_id=m.id AND e.command IS NOT NULL ORDER BY e.received_at LIMIT 1),
		(SELECT e.gathering_id::text FROM channel_inbound_events e WHERE e.workspace_id=m.workspace_id AND e.message_id=m.id AND e.gathering_id IS NOT NULL LIMIT 1),
		other_contact_id::text,detached_at,detached_by_agent_id::text
		FROM messages m WHERE workspace_id=$1 AND conversation_id=$2 ORDER BY created_at,id`, workspaceID, conversationID)
	if err != nil {
		return fmt.Errorf("export messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.Message
		var meta, channelMeta []byte
		if err := rows.Scan(&record.ID, &record.ConversationID, &record.AuthorAgentID, &record.AuthorName, &record.Kind, &record.Body,
			&record.BodyHTML, &record.Origin, &record.EmailMessageID, &meta, &record.CC, &record.CreatedAt, &record.AgentRequestID, &record.AIDraftID,
			&record.ChannelID, &record.ExternalID, &channelMeta, &record.DecisionBasis, &record.DecisionRule, &record.Command, &record.GatheringID,
			&record.OtherContactID, &record.DetachedAt, &record.DetachedByAgentID); err != nil {
			return fmt.Errorf("export messages: %w", err)
		}
		record.CreatedAt, record.DetachedAt = domain.UTC(record.CreatedAt), domain.UTCOptional(record.DetachedAt)
		record.EmailMeta = meta
		if channelMeta != nil {
			record.ChannelMeta = channelMeta
		}
		record.Attachments = files[record.ID]
		if err := sink.Message(record); err != nil {
			return err
		}
	}
	return rowsError("export messages", rows)
}

// exportKB writes the whole knowledge base: collections and articles in the trash too, every version,
// translation and image, and the help center settings. Articles carry the original HTML an import kept.
func exportKB(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,parent_id::text,name,slug,description,position,source_meta,created_at,updated_at,deleted_at,deleted_by_agent_id::text
		FROM kb_collections WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base collections: %w", err)
	}
	for rows.Next() {
		var record export.KBCollection
		var meta []byte
		if err := rows.Scan(&record.ID, &record.ParentID, &record.Name, &record.Slug, &record.Description, &record.Position, &meta,
			&record.CreatedAt, &record.UpdatedAt, &record.DeletedAt, &record.DeletedByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base collections: %w", err)
		}
		record.SourceMeta = meta
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.KBCollection(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base collections", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT a.id::text,a.collection_id::text,a.position,a.number,a.title,a.slug,a.body_md,a.seo_title,a.seo_description,a.status,a.published_at,a.author_agent_id::text,a.source_meta,a.source_html,
			COALESCE((SELECT array_agg(l.media_id::text ORDER BY l.media_id) FROM kb_article_media l JOIN kb_media m ON m.workspace_id=l.workspace_id AND m.id=l.media_id
				WHERE l.workspace_id=a.workspace_id AND l.article_id=a.id AND m.kind='image'), '{}'),
			a.created_at,a.updated_at,a.deleted_at,a.deleted_by_agent_id::text,a.ai_excluded_at,a.ai_excluded_by_agent_id::text
		FROM kb_articles a WHERE a.workspace_id=$1 ORDER BY a.number`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base articles: %w", err)
	}
	for rows.Next() {
		var record export.KBArticle
		var meta []byte
		if err := rows.Scan(&record.ID, &record.CollectionID, &record.Position, &record.Number, &record.Title, &record.Slug, &record.BodyMD, &record.SEOTitle, &record.SEODescription, &record.Status, &record.PublishedAt,
			&record.AuthorAgentID, &meta, &record.SourceHTML, &record.ImageIDs, &record.CreatedAt, &record.UpdatedAt, &record.DeletedAt, &record.DeletedByAgentID,
			&record.AIExcludedAt, &record.AIExcludedByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base articles: %w", err)
		}
		record.SourceMeta = meta
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		record.PublishedAt, record.DeletedAt = domain.UTCOptional(record.PublishedAt), domain.UTCOptional(record.DeletedAt)
		record.AIExcludedAt = domain.UTCOptional(record.AIExcludedAt)
		if err := sink.KBArticle(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base articles", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT article_id::text,version,kind,restored_from_version,title,slug,body_md,seo_title,seo_description,status,collection_id::text,changed_by_agent_id::text,
			token_id::text,token_label,agent_request_id::text,created_at
		FROM kb_article_versions WHERE workspace_id=$1 ORDER BY article_id,version`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base versions: %w", err)
	}
	for rows.Next() {
		var record export.KBArticleVersion
		if err := rows.Scan(&record.ArticleID, &record.Version, &record.Kind, &record.RestoredFromVersion, &record.Title, &record.Slug, &record.BodyMD, &record.SEOTitle, &record.SEODescription, &record.Status,
			&record.CollectionID, &record.ChangedByAgentID, &record.TokenID, &record.TokenLabel, &record.AgentRequestID, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base versions: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.KBArticleVersion(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base versions", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT article_id::text,locale,title,body_md,source_meta,source_html,created_at,updated_at
		FROM kb_article_translations WHERE workspace_id=$1 ORDER BY article_id,locale`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base translations: %w", err)
	}
	for rows.Next() {
		var record export.KBArticleTranslation
		var meta []byte
		if err := rows.Scan(&record.ArticleID, &record.Locale, &record.Title, &record.BodyMD, &meta, &record.SourceHTML, &record.CreatedAt, &record.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base translations: %w", err)
		}
		record.SourceMeta = meta
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		if err := sink.KBArticleTranslation(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base translations", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT id::text,content_type,size,sha256,storage_key,created_by_agent_id::text,created_at,
			name,alt,width,height,kept,deleted_at,deleted_by_agent_id::text,updated_at,updated_by_agent_id::text
		FROM kb_media WHERE workspace_id=$1 AND kind='image' ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base images: %w", err)
	}
	for rows.Next() {
		var record export.KBImage
		if err := rows.Scan(&record.ID, &record.ContentType, &record.Size, &record.SHA256, &record.StorageKey, &record.CreatedByAgentID, &record.CreatedAt,
			&record.Name, &record.Alt, &record.Width, &record.Height, &record.Kept, &record.DeletedAt, &record.DeletedByAgentID, &record.UpdatedAt, &record.UpdatedByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base images: %w", err)
		}
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.KBImage(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base images", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT id::text,content_type,size,sha256,storage_key,COALESCE(poster_storage_key,''),COALESCE(poster_size,0),COALESCE(poster_content_type,''),
			name,alt,width,height,duration_ms,codecs,kept,created_by_agent_id::text,created_at,updated_at,updated_by_agent_id::text,deleted_at,deleted_by_agent_id::text
		FROM kb_media WHERE workspace_id=$1 AND kind='video' ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base videos: %w", err)
	}
	for rows.Next() {
		var record export.KBVideo
		if err := rows.Scan(&record.ID, &record.ContentType, &record.Size, &record.SHA256, &record.StorageKey, &record.PosterKey, &record.PosterSize, &record.PosterContentType,
			&record.Name, &record.Alt, &record.Width, &record.Height, &record.DurationMS, &record.Codecs, &record.Kept, &record.CreatedByAgentID, &record.CreatedAt,
			&record.UpdatedAt, &record.UpdatedByAgentID, &record.DeletedAt, &record.DeletedByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base videos: %w", err)
		}
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.KBVideo(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base videos", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT id::text,from_media_id::text,to_media_id::text,article_ids::text[],article_versions,translations,skipped_article_ids::text[],replaced_by_agent_id::text,created_at,undone_at,undone_by_agent_id::text
		FROM kb_media_replacements WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base media replacements: %w", err)
	}
	for rows.Next() {
		var record export.KBMediaReplacement
		if err := rows.Scan(&record.ID, &record.FromMediaID, &record.ToMediaID, &record.ArticleIDs, &record.ArticleVersions, &record.Translations, &record.SkippedArticleIDs, &record.ReplacedByAgentID,
			&record.CreatedAt, &record.UndoneAt, &record.UndoneByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base media replacements: %w", err)
		}
		record.CreatedAt, record.UndoneAt = domain.UTC(record.CreatedAt), domain.UTCOptional(record.UndoneAt)
		if err := sink.KBMediaReplacement(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base media replacements", rows); err != nil {
		return err
	}

	var center export.HelpCenter
	// The custom domain's token is never exported: it is a credential of this server's check.
	err = tx.QueryRow(ctx, `SELECT h.enabled,h.title,h.locale,h.content_version,h.last_article_number,h.updated_at,
		h.logo_media_id::text,h.favicon_media_id::text,h.logo_alt,h.linkback_url,h.contact_url,h.contact_phone,h.color_primary,h.color_header,h.color_header_text,
		h.color_background,h.color_text,h.color_link,h.color_footer,h.body_font,h.heading_font,h.custom_css,h.google_site_verification,h.bing_site_verification,d.hostname,d.status,d.verified_at
		FROM help_centers h LEFT JOIN help_center_domains d ON d.workspace_id=h.workspace_id WHERE h.workspace_id=$1`, workspaceID).
		Scan(&center.Enabled, &center.Title, &center.Locale, &center.ContentVersion, &center.LastArticleNumber, &center.UpdatedAt,
			&center.LogoMediaID, &center.FaviconMediaID, &center.LogoAlt, &center.LinkbackURL, &center.ContactURL, &center.ContactPhone,
			&center.ColorPrimary, &center.ColorHeader, &center.ColorHeaderText, &center.ColorBackground, &center.ColorText, &center.ColorLink,
			&center.ColorFooter, &center.BodyFont, &center.HeadingFont, &center.CustomCSS, &center.GoogleSiteVerification, &center.BingSiteVerification, &center.CustomDomain, &center.CustomDomainStatus, &center.CustomDomainVerifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("export help center: %w", err)
	}
	center.UpdatedAt = domain.UTC(center.UpdatedAt)
	center.CustomDomainVerifiedAt = domain.UTCOptional(center.CustomDomainVerifiedAt)
	return sink.HelpCenter(center)
}

// exportAI writes the AI providers (never a sealed key), feature settings, their history and usage.
func exportAI(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,kind,label,base_url,auth_kind,key_hint,models,check_status,check_error_code,checked_at,created_by::text,created_at,updated_at,routing
		FROM ai_providers WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI providers: %w", err)
	}
	providers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (export.AIProvider, error) {
		var r export.AIProvider
		err := row.Scan(&r.ID, &r.Kind, &r.Label, &r.BaseURL, &r.AuthKind, &r.KeyHint, &r.Models, &r.CheckStatus, &r.CheckErrorCode, &r.CheckedAt, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt, &r.Routing)
		r.CheckedAt, r.CreatedAt, r.UpdatedAt = domain.UTC(r.CheckedAt), domain.UTC(r.CreatedAt), domain.UTC(r.UpdatedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export AI providers: %w", err)
	}
	for _, record := range providers {
		if err := sink.AIProvider(record); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT slot,uses_server,provider_id::text,model,fallback_model,effort,enabled,default_level,
			(slot='triage' AND default_level='auto') OR (slot='draft' AND default_level='suggest'),auto_parts,consented_by::text,consented_at,updated_by::text,updated_at,
			monthly_cap_micro_usd
		FROM ai_slots WHERE workspace_id=$1 ORDER BY slot`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI slots: %w", err)
	}
	slots, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (export.AISlot, error) {
		var r export.AISlot
		err := row.Scan(&r.Slot, &r.UsesServer, &r.ProviderID, &r.Model, &r.FallbackModel, &r.Effort, &r.Enabled, &r.DefaultLevel, &r.Automatic, &r.AutoParts, &r.ConsentedBy, &r.ConsentedAt, &r.UpdatedBy, &r.UpdatedAt,
			&r.MonthlyCapMicroUSD)
		r.ConsentedAt, r.UpdatedAt = domain.UTCOptional(r.ConsentedAt), domain.UTC(r.UpdatedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export AI slots: %w", err)
	}
	for _, record := range slots {
		if err := sink.AISlot(record); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT id::text,kind,actor_agent_id::text,actor_token_id::text,provider_id::text,provider_label,slot,detail,created_at
		FROM ai_settings_events WHERE workspace_id=$1 ORDER BY created_at,seq`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI settings events: %w", err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (export.AISettingsEvent, error) {
		var r export.AISettingsEvent
		err := row.Scan(&r.ID, &r.Kind, &r.ActorAgentID, &r.ActorTokenID, &r.ProviderID, &r.ProviderLabel, &r.Slot, &r.Detail, &r.CreatedAt)
		r.CreatedAt = domain.UTC(r.CreatedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export AI settings events: %w", err)
	}
	for _, record := range events {
		if err := sink.AISettingsEvent(record); err != nil {
			return err
		}
	}
	rows, err = tx.Query(ctx, `SELECT source_class,consented_by::text,consented_at FROM ai_source_consents WHERE workspace_id=$1 ORDER BY source_class`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI source consents: %w", err)
	}
	consents, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (export.AISourceConsent, error) {
		var r export.AISourceConsent
		err := row.Scan(&r.SourceClass, &r.ConsentedBy, &r.ConsentedAt)
		r.ConsentedAt = domain.UTC(r.ConsentedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export AI source consents: %w", err)
	}
	for _, record := range consents {
		if err := sink.AISourceConsent(record); err != nil {
			return err
		}
	}
	// Usage can be long: stream it.
	usage, err := tx.Query(ctx, `SELECT id::text,feature,billed_to,provider_id::text,provider_kind,provider_label,model,status,error_code,stop_reason,served_model,
			input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,hops,provider_request_id,actor_agent_id::text,actor_token_id::text,started_at,finished_at,latency_ms,
			task_id::text,automatic,cost_micro_usd,reserved_micro_usd,provider_cost::float8
		FROM ai_usage WHERE workspace_id=$1 ORDER BY started_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI usage: %w", err)
	}
	defer usage.Close()
	for usage.Next() {
		var r export.AIUsage
		var hops []byte
		if err := usage.Scan(&r.ID, &r.Feature, &r.BilledTo, &r.ProviderID, &r.ProviderKind, &r.ProviderLabel, &r.Model, &r.Status, &r.ErrorCode, &r.StopReason, &r.ServedModel,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &hops, &r.ProviderRequestID, &r.ActorAgentID, &r.ActorTokenID, &r.StartedAt, &r.FinishedAt, &r.LatencyMS,
			&r.TaskID, &r.Automatic, &r.CostMicroUSD, &r.ReservedMicroUSD, &r.ProviderCost); err != nil {
			return fmt.Errorf("export AI usage: %w", err)
		}
		r.Hops = hops
		r.StartedAt, r.FinishedAt = domain.UTC(r.StartedAt), domain.UTCOptional(r.FinishedAt)
		if err := sink.AIUsage(r); err != nil {
			return err
		}
	}
	if err := rowsError("export AI usage", usage); err != nil {
		return err
	}
	usage.Close()
	drafts, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,task_id::text,requested_by_agent_id::text,requested_by_token_id::text,prompt,status,text,missing_info,
			sources,warnings,model,outcome,similarity::float8,sent_message_id::text,automatic,answers_message_id::text,reply_needed,created_at,ready_at,decided_at,decided_by_agent_id::text,text_cleared_at
		FROM ai_drafts WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI drafts: %w", err)
	}
	defer drafts.Close()
	for drafts.Next() {
		var r export.AIDraft
		var sources, warnings []byte
		if err := drafts.Scan(&r.ID, &r.ConversationID, &r.TaskID, &r.RequestedByAgentID, &r.RequestedByTokenID, &r.Prompt, &r.Status, &r.Text, &r.MissingInfo,
			&sources, &warnings, &r.Model, &r.Outcome, &r.Similarity, &r.SentMessageID, &r.Automatic, &r.AnswersMessageID, &r.ReplyNeeded, &r.CreatedAt, &r.ReadyAt, &r.DecidedAt, &r.DecidedByAgentID, &r.TextClearedAt); err != nil {
			return fmt.Errorf("export AI drafts: %w", err)
		}
		r.Sources, r.Warnings = sources, warnings
		r.CreatedAt, r.ReadyAt, r.DecidedAt, r.TextClearedAt = domain.UTC(r.CreatedAt), domain.UTCOptional(r.ReadyAt), domain.UTCOptional(r.DecidedAt), domain.UTCOptional(r.TextClearedAt)
		if err := sink.AIDraft(r); err != nil {
			return err
		}
	}
	if err := rowsError("export AI drafts", drafts); err != nil {
		return err
	}
	drafts.Close()
	triage, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,task_id::text,automatic,requested_by_agent_id::text,requested_by_token_id::text,status,tag_ids::text[],
			priority,assignee_agent_id::text,reason,sources,base,stale,model,outcome,applied,created_at,ready_at,decided_at,decided_by_agent_id::text,
			auto_applied_at,auto_applied,skipped,undone_parts,kept,undone_at,undone_by_agent_id::text
		FROM ai_triage_suggestions WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export AI triage: %w", err)
	}
	defer triage.Close()
	for triage.Next() {
		var r export.AITriage
		var applied, sources, base, stale, autoApplied, skipped, undone, kept []byte
		if err := triage.Scan(&r.ID, &r.ConversationID, &r.TaskID, &r.Automatic, &r.RequestedByAgentID, &r.RequestedByTokenID, &r.Status, &r.TagIDs,
			&r.Priority, &r.AssigneeAgentID, &r.Reason, &sources, &base, &stale, &r.Model, &r.Outcome, &applied, &r.CreatedAt, &r.ReadyAt, &r.DecidedAt, &r.DecidedByAgentID,
			&r.AutoAppliedAt, &autoApplied, &skipped, &undone, &kept, &r.UndoneAt, &r.UndoneByAgentID); err != nil {
			return fmt.Errorf("export AI triage: %w", err)
		}
		r.Applied, r.Sources, r.Base, r.Stale = applied, sources, base, stale
		r.AutoApplied, r.Skipped, r.UndoneParts, r.Kept = autoApplied, skipped, undone, kept
		r.AutoAppliedAt, r.UndoneAt = domain.UTCOptional(r.AutoAppliedAt), domain.UTCOptional(r.UndoneAt)
		r.CreatedAt, r.ReadyAt, r.DecidedAt = domain.UTC(r.CreatedAt), domain.UTCOptional(r.ReadyAt), domain.UTCOptional(r.DecidedAt)
		if err := sink.AITriage(r); err != nil {
			return err
		}
	}
	if err := rowsError("export AI triage", triage); err != nil {
		return err
	}
	triage.Close()
	return exportAIChats(ctx, tx, workspaceID, sink)
}

// exportAIChats streams the help center's AI chats, each with its turns, their sources and feedback.
func exportAIChats(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	// A page of chats at a time, oldest first, with all their turns and sources in two queries.
	var after *time.Time
	afterID := ""
	for {
		rows, err := tx.Query(ctx, `SELECT id::text,channel_id::text,surface,status,conversation_id::text,handed_off_at,history_cut_at,created_at,last_turn_at,ended_at
			FROM ai_chat_sessions WHERE workspace_id=$1 AND ($2::timestamptz IS NULL OR (created_at,id::text)>($2,$3))
			ORDER BY created_at,id::text LIMIT $4`, workspaceID, after, afterID, exportPageConversations)
		if err != nil {
			return fmt.Errorf("export AI chats: %w", err)
		}
		chats, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (export.AIChatSession, error) {
			var r export.AIChatSession
			err := row.Scan(&r.ID, &r.ChannelID, &r.Surface, &r.Status, &r.ConversationID, &r.HandedOffAt, &r.HistoryCutAt, &r.CreatedAt, &r.LastTurnAt, &r.EndedAt)
			return r, err
		})
		if err != nil {
			return fmt.Errorf("export AI chats: %w", err)
		}
		if len(chats) == 0 {
			return nil
		}
		last := chats[len(chats)-1]
		after, afterID = &last.CreatedAt, last.ID
		ids := make([]string, len(chats))
		turnsOf := map[string][]export.AIChatTurn{}
		for i, chat := range chats {
			ids[i] = chat.ID
		}
		turns, err := tx.Query(ctx, `SELECT t.session_id::text,t.id::text,t.seq,t.question,t.answer,t.viewing_kb_article_id::text,t.status,t.error_code,t.model,t.ttft_ms,
				t.input_tokens,t.cache_read_tokens,t.cache_creation_tokens,t.output_tokens,t.created_at,t.finished_at,f.rating,f.created_at,f.updated_at
			FROM ai_chat_turns t LEFT JOIN ai_chat_feedback f ON f.workspace_id=t.workspace_id AND f.turn_id=t.id
			WHERE t.workspace_id=$1 AND t.session_id=ANY($2::text[]::uuid[]) ORDER BY t.session_id,t.seq`, workspaceID, ids)
		if err != nil {
			return fmt.Errorf("export AI chat turns: %w", err)
		}
		for turns.Next() {
			var sessionID string
			var r export.AIChatTurn
			var rating *string
			var ratedAt, changedAt *time.Time
			if err := turns.Scan(&sessionID, &r.ID, &r.Seq, &r.Question, &r.Answer, &r.ViewingKBArticleID, &r.Status, &r.ErrorCode, &r.Model, &r.TTFTMS,
				&r.InputTokens, &r.CacheReadTokens, &r.CacheCreationTokens, &r.OutputTokens, &r.CreatedAt, &r.FinishedAt, &rating, &ratedAt, &changedAt); err != nil {
				turns.Close()
				return fmt.Errorf("export AI chat turns: %w", err)
			}
			r.CreatedAt, r.FinishedAt, r.Sources = domain.UTC(r.CreatedAt), domain.UTCOptional(r.FinishedAt), []export.AIChatSource{}
			if rating != nil && ratedAt != nil && changedAt != nil {
				r.Feedback = &export.AIChatFeedback{Rating: *rating, CreatedAt: domain.UTC(*ratedAt), UpdatedAt: domain.UTC(*changedAt)}
			}
			turnsOf[sessionID] = append(turnsOf[sessionID], r)
		}
		if err := rowsError("export AI chat turns", turns); err != nil {
			return err
		}
		sources, err := tx.Query(ctx, `SELECT t.session_id::text,o.turn_id::text,o.ordinal,o.kb_article_id::text,o.article_number,o.article_version,o.title,o.folder
			FROM ai_chat_sources o JOIN ai_chat_turns t ON t.workspace_id=o.workspace_id AND t.id=o.turn_id
			WHERE o.workspace_id=$1 AND t.session_id=ANY($2::text[]::uuid[]) ORDER BY t.session_id,t.seq,o.ordinal`, workspaceID, ids)
		if err != nil {
			return fmt.Errorf("export AI chat sources: %w", err)
		}
		for sources.Next() {
			var sessionID, turnID string
			var r export.AIChatSource
			if err := sources.Scan(&sessionID, &turnID, &r.Ordinal, &r.KBArticleID, &r.ArticleNumber, &r.ArticleVersion, &r.Title, &r.Folder); err != nil {
				sources.Close()
				return fmt.Errorf("export AI chat sources: %w", err)
			}
			list := turnsOf[sessionID]
			for i := range list {
				if list[i].ID == turnID {
					list[i].Sources = append(list[i].Sources, r)
					break
				}
			}
		}
		if err := rowsError("export AI chat sources", sources); err != nil {
			return err
		}
		for _, chat := range chats {
			chat.HistoryCutAt, chat.EndedAt, chat.HandedOffAt = domain.UTCOptional(chat.HistoryCutAt), domain.UTCOptional(chat.EndedAt), domain.UTCOptional(chat.HandedOffAt)
			chat.CreatedAt, chat.LastTurnAt, chat.Turns = domain.UTC(chat.CreatedAt), domain.UTC(chat.LastTurnAt), turnsOf[chat.ID]
			if chat.Turns == nil {
				chat.Turns = []export.AIChatTurn{}
			}
			if err := sink.AIChatSession(chat); err != nil {
				return err
			}
		}
		if len(chats) < exportPageConversations {
			return nil
		}
	}
}

func exportPageStatusChanges(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, into map[string][]export.StatusChange) error {
	rows, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,from_status,to_status,changed_at,actor_agent_id::text,source
		FROM conversation_status_changes WHERE workspace_id=$1 AND conversation_id=ANY($2::text[]::uuid[]) ORDER BY conversation_id,changed_at,id`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("export status changes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item export.StatusChange
		var conversationID string
		if err := rows.Scan(&item.ID, &conversationID, &item.FromStatus, &item.ToStatus, &item.ChangedAt, &item.ActorAgentID, &item.Source); err != nil {
			return fmt.Errorf("export status changes: %w", err)
		}
		item.ChangedAt = domain.UTC(item.ChangedAt)
		into[conversationID] = append(into[conversationID], item)
	}
	return rowsError("export status changes", rows)
}

// exportResultsBaseline writes the results baseline, when there is one: the numbers are derived, but the
// anchor and the import's status are not, and the numbers measured at the anchor cannot be measured again.
func exportResultsBaseline(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	var record export.ResultsBaseline
	var numbers []byte
	err := tx.QueryRow(ctx, `SELECT s.baseline_source,s.baseline_anchor_at,s.baseline_import_status,b.computed_at,b.data
		FROM results_state s LEFT JOIN LATERAL (SELECT computed_at,data FROM results_snapshots
			WHERE workspace_id=s.workspace_id AND kind='baseline' ORDER BY computed_at DESC LIMIT 1) b ON true
		WHERE s.workspace_id=$1 AND s.baseline_source IS NOT NULL`, workspaceID).Scan(&record.Source, &record.AnchorAt, &record.ImportStatus, &record.ComputedAt, &numbers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("export results baseline: %w", err)
	}
	record.AnchorAt, record.ComputedAt, record.Numbers = domain.UTC(record.AnchorAt), domain.UTCOptional(record.ComputedAt), numbers
	if record.Numbers == nil {
		record.Numbers = json.RawMessage("null")
	}
	return sink.ResultsBaseline(record)
}

// exportChannels writes every channel (its configuration as its type shows it, never a secret), the
// channels' history, and the events they received that are not messages, with their raw payloads.
func (s *Store) exportChannels(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT `+channelRowColumns+` FROM channels WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export channels: %w", err)
	}
	type stored struct {
		channel  channels.Channel
		settings []byte
	}
	var list []stored
	for rows.Next() {
		var item stored
		var kind, external string
		if err := rows.Scan(&item.channel.ID, &item.channel.WorkspaceID, &kind, &item.channel.Name, &item.channel.Enabled, &item.settings, &external,
			&item.channel.CreatedAt, &item.channel.UpdatedAt, &item.channel.DisabledAt, &item.channel.DeletedAt, &item.channel.IsDefault); err != nil {
			rows.Close()
			return fmt.Errorf("export channels: %w", err)
		}
		item.channel.Type, item.channel.ExternalAccount = channels.Type(kind), external
		list = append(list, item)
	}
	rows.Close()
	if err := rowsError("export channels", rows); err != nil {
		return err
	}
	for _, item := range list {
		channel := item.channel
		record := export.Channel{ID: channel.ID, Type: string(channel.Type), Name: channel.Name, Enabled: channel.Enabled, Settings: item.settings,
			CreatedAt: domain.UTC(channel.CreatedAt), UpdatedAt: domain.UTC(channel.UpdatedAt), DisabledAt: domain.UTCOptional(channel.DisabledAt),
			DeletedAt: domain.UTCOptional(channel.DeletedAt), IsDefault: channel.IsDefault}
		if channel.ExternalAccount != "" {
			record.ExternalAccount = &channel.ExternalAccount
		}
		if adapter, ok := s.channels.Adapter(channel.Type); ok && channel.Live() {
			if record.Config, err = adapter.Config().Public(ctx, tx, channel); err != nil {
				return fmt.Errorf("export channel %s: %w", channel.ID, err)
			}
		}
		if err := sink.Channel(record); err != nil {
			return err
		}
	}
	events, err := tx.Query(ctx, `SELECT id::text,channel_id::text,channel_type,channel_name,kind,actor_agent_id::text,actor_token_id::text,detail,created_at
		FROM channel_events WHERE workspace_id=$1 ORDER BY created_at,seq`, workspaceID)
	if err != nil {
		return fmt.Errorf("export channel history: %w", err)
	}
	history, err := pgx.CollectRows(events, func(row pgx.CollectableRow) (export.ChannelEvent, error) {
		var r export.ChannelEvent
		err := row.Scan(&r.ID, &r.ChannelID, &r.ChannelType, &r.ChannelName, &r.Kind, &r.ActorAgentID, &r.ActorTokenID, &r.Detail, &r.CreatedAt)
		r.CreatedAt = domain.UTC(r.CreatedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export channel history: %w", err)
	}
	for _, record := range history {
		if err := sink.ChannelEvent(record); err != nil {
			return err
		}
	}
	levels, err := tx.Query(ctx, `SELECT channel_id::text,scope,slot,level,options,updated_by::text,updated_at FROM channel_ai_levels
		WHERE workspace_id=$1 ORDER BY channel_id,scope,slot`, workspaceID)
	if err != nil {
		return fmt.Errorf("export channel AI levels: %w", err)
	}
	levelRecords, err := pgx.CollectRows(levels, func(row pgx.CollectableRow) (export.ChannelAILevel, error) {
		var r export.ChannelAILevel
		err := row.Scan(&r.ChannelID, &r.Scope, &r.Slot, &r.Level, &r.Options, &r.UpdatedBy, &r.UpdatedAt)
		r.UpdatedAt = domain.UTC(r.UpdatedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export channel AI levels: %w", err)
	}
	for _, record := range levelRecords {
		if err := sink.ChannelAILevel(record); err != nil {
			return err
		}
	}
	// What a channel received and did not make a message: everything but a processed message and a
	// discarded event (an admin threw it away).
	inbound, err := tx.Query(ctx, `SELECT e.id::text,e.channel_id::text,e.external_event_id,e.state,e.raw,COALESCE(e.raw_object_key,''),COALESCE(e.raw_bytes,0),
			e.hold_reason,e.drop_reason,e.error,e.decision_basis,e.decision_rule,e.command,e.gathering_id::text,e.received_at,e.processed_at,
			(SELECT COALESCE(json_agg(json_build_object('name',f.name,'content_type',f.content_type,'status',f.status,'size',f.size,'sha256',f.sha256,
				'error',f.error,'key',f.storage_key) ORDER BY f.idx),'[]') FROM channel_inbound_files f WHERE f.workspace_id=e.workspace_id AND f.event_id=e.id)
		FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.state<>'discarded' AND NOT (e.state='processed' AND e.message_id IS NOT NULL)
		ORDER BY e.received_at,e.id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export channel events: %w", err)
	}
	defer inbound.Close()
	for inbound.Next() {
		var r export.ChannelInboundEvent
		var files []byte
		if err := inbound.Scan(&r.ID, &r.ChannelID, &r.ExternalEventID, &r.State, &r.Raw, &r.RawKey, &r.RawSize, &r.HoldReason, &r.DropReason, &r.Error,
			&r.DecisionBasis, &r.DecisionRule, &r.Command, &r.GatheringID, &r.ReceivedAt, &r.ProcessedAt, &files); err != nil {
			return fmt.Errorf("export channel events: %w", err)
		}
		var stored []struct {
			export.ChannelInboundFile
			Key *string `json:"key"`
		}
		if err := json.Unmarshal(files, &stored); err != nil {
			return fmt.Errorf("export channel event files: %w", err)
		}
		for _, file := range stored {
			if file.Key != nil {
				file.StorageKey = *file.Key
			}
			r.Files = append(r.Files, file.ChannelInboundFile)
		}
		r.ReceivedAt, r.ProcessedAt = domain.UTC(r.ReceivedAt), domain.UTCOptional(r.ProcessedAt)
		if err := sink.ChannelInboundEvent(r); err != nil {
			return err
		}
	}
	if err := rowsError("export channel events", inbound); err != nil {
		return err
	}
	inbound.Close()
	return exportChatThreads(ctx, tx, workspaceID, sink)
}

// exportChatThreads writes the places of the chat channels, everything said in them, and which of it
// is part of which conversation; items stream, a busy group says a lot.
func exportChatThreads(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	threads, err := tx.Query(ctx, `SELECT id::text,channel_id::text,kind,external_id,name,contact_id::text,opens_conversations,last_item_at,created_at
		FROM channel_threads WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export chat threads: %w", err)
	}
	records, err := pgx.CollectRows(threads, func(row pgx.CollectableRow) (export.ChannelThread, error) {
		var r export.ChannelThread
		err := row.Scan(&r.ID, &r.ChannelID, &r.Kind, &r.ExternalID, &r.Name, &r.ContactID, &r.OpensConversations, &r.LastItemAt, &r.CreatedAt)
		r.LastItemAt, r.CreatedAt = domain.UTCOptional(r.LastItemAt), domain.UTC(r.CreatedAt)
		return r, err
	})
	if err != nil {
		return fmt.Errorf("export chat threads: %w", err)
	}
	for _, record := range records {
		if err := sink.ChannelThread(record); err != nil {
			return err
		}
	}
	items, err := tx.Query(ctx, `SELECT id::text,channel_id::text,thread_id::text,direction,role,sender_kind,sender_value,sender_name,contact_id::text,agent_id::text,
			external_id,quote_external_id,body,inbound_event_id::text,message_id::text,gathering_id::text,disposition,disposition_reason,occurred_at,alerted_at
		FROM channel_thread_items WHERE workspace_id=$1 ORDER BY occurred_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export chat items: %w", err)
	}
	for items.Next() {
		var r export.ChannelThreadItem
		if err := items.Scan(&r.ID, &r.ChannelID, &r.ThreadID, &r.Direction, &r.Role, &r.SenderKind, &r.SenderValue, &r.SenderName, &r.ContactID, &r.AgentID,
			&r.ExternalID, &r.QuoteExternalID, &r.Body, &r.InboundEventID, &r.MessageID, &r.GatheringID, &r.Disposition, &r.DispositionReason,
			&r.OccurredAt, &r.AlertedAt); err != nil {
			items.Close()
			return fmt.Errorf("export chat items: %w", err)
		}
		r.OccurredAt, r.AlertedAt = domain.UTC(r.OccurredAt), domain.UTCOptional(r.AlertedAt)
		if err := sink.ChannelThreadItem(r); err != nil {
			items.Close()
			return err
		}
	}
	if err := rowsError("export chat items", items); err != nil {
		return err
	}
	links, err := tx.Query(ctx, `SELECT id::text,conversation_id::text,item_id::text,reason,decision_id::text,linked_by_agent_id::text,linked_at,unlinked_at,
			unlinked_by_agent_id::text FROM conversation_thread_items WHERE workspace_id=$1 ORDER BY linked_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export chat links: %w", err)
	}
	for links.Next() {
		var r export.ConversationThreadItem
		if err := links.Scan(&r.ID, &r.ConversationID, &r.ItemID, &r.Reason, &r.DecisionID, &r.LinkedByAgentID, &r.LinkedAt, &r.UnlinkedAt, &r.UnlinkedByAgentID); err != nil {
			links.Close()
			return fmt.Errorf("export chat links: %w", err)
		}
		r.LinkedAt, r.UnlinkedAt = domain.UTC(r.LinkedAt), domain.UTCOptional(r.UnlinkedAt)
		if err := sink.ConversationThreadItem(r); err != nil {
			links.Close()
			return err
		}
	}
	if err := rowsError("export chat links", links); err != nil {
		return err
	}
	links.Close()
	return exportIntake(ctx, tx, workspaceID, sink)
}

// exportIntake writes the gatherings of the chat channels' intake and every decision made on them.
func exportIntake(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	gatherings, err := tx.Query(ctx, `SELECT id::text,channel_id::text,scope,lane_key,thread_id::text,contact_id::text,state,review_reason,rules_action,rules_basis,rules_rule,
			rules_target::text,conversation_id::text,decided_by,decided_at,decided_by_agent_id::text,decided_by_token_id::text,fallback,ignore_reason,opened_at,closed_at,decide_by,
			requeued_at,requeued_by_agent_id::text,created_at,updated_at
		FROM intake_gatherings WHERE workspace_id=$1 ORDER BY closed_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export intake gatherings: %w", err)
	}
	for gatherings.Next() {
		var r export.IntakeGathering
		if err := gatherings.Scan(&r.ID, &r.ChannelID, &r.Scope, &r.LaneKey, &r.ThreadID, &r.ContactID, &r.State, &r.ReviewReason, &r.RulesAction, &r.RulesBasis, &r.RulesRule,
			&r.RulesTarget, &r.ConversationID, &r.DecidedBy, &r.DecidedAt, &r.DecidedByAgentID, &r.DecidedByTokenID, &r.Fallback, &r.IgnoreReason, &r.OpenedAt, &r.ClosedAt,
			&r.DecideBy, &r.RequeuedAt, &r.RequeuedByAgentID, &r.CreatedAt, &r.UpdatedAt); err != nil {
			gatherings.Close()
			return fmt.Errorf("export intake gatherings: %w", err)
		}
		r.DecidedAt, r.DecideBy, r.RequeuedAt = domain.UTCOptional(r.DecidedAt), domain.UTCOptional(r.DecideBy), domain.UTCOptional(r.RequeuedAt)
		r.OpenedAt, r.ClosedAt, r.CreatedAt, r.UpdatedAt = domain.UTC(r.OpenedAt), domain.UTC(r.ClosedAt), domain.UTC(r.CreatedAt), domain.UTC(r.UpdatedAt)
		if err := sink.IntakeGathering(r); err != nil {
			gatherings.Close()
			return err
		}
	}
	if err := rowsError("export intake gatherings", gatherings); err != nil {
		return err
	}
	decisions, err := tx.Query(ctx, `SELECT id::text,gathering_id::text,decider,basis,action,target_conversation_id::text,runner_up_conversation_id::text,
			runner_up_confidence::float8,command,subject,reply_kind,related_conversation_id::text,confidence::float8,reason,applied,ignore_reason,not_applied_reason,
			items_seen::text[],fallback,corrects_decision_id::text,manifest,task_id::text,model,prompt_version,latency_ms,error_code,agent_id::text,token_id::text,created_at
		FROM intake_decisions WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export intake decisions: %w", err)
	}
	for decisions.Next() {
		var r export.IntakeDecision
		if err := decisions.Scan(&r.ID, &r.GatheringID, &r.Decider, &r.Basis, &r.Action, &r.TargetConversationID, &r.RunnerUpConversationID,
			&r.RunnerUpConfidence, &r.Command, &r.Subject, &r.ReplyKind, &r.RelatedConversationID, &r.Confidence, &r.Reason, &r.Applied, &r.IgnoreReason, &r.NotAppliedReason,
			&r.ItemsSeen, &r.Fallback, &r.CorrectsDecisionID, &r.Manifest, &r.TaskID, &r.Model, &r.PromptVersion, &r.LatencyMS, &r.ErrorCode, &r.AgentID,
			&r.TokenID, &r.CreatedAt); err != nil {
			decisions.Close()
			return fmt.Errorf("export intake decisions: %w", err)
		}
		r.CreatedAt = domain.UTC(r.CreatedAt)
		if err := sink.IntakeDecision(r); err != nil {
			decisions.Close()
			return err
		}
	}
	if err := rowsError("export intake decisions", decisions); err != nil {
		return err
	}
	return exportMessageMoves(ctx, tx, workspaceID, sink)
}

// exportMessageMoves writes every move of customer messages, undone ones included.
func exportMessageMoves(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,message_ids::text[],from_conversation_id::text,to_conversation_id::text,created_conversation,closed_source,
			source_status_before,target_status_before,thread_keys_moved,gathering_id::text,decision_id::text,moved_by_agent_id::text,moved_by_token_id::text,
			moved_at,undone_at,undone_by_agent_id::text,undone_by_token_id::text
		FROM message_moves WHERE workspace_id=$1 ORDER BY seq`, workspaceID)
	if err != nil {
		return fmt.Errorf("export message moves: %w", err)
	}
	for rows.Next() {
		var r export.MessageMove
		if err := rows.Scan(&r.ID, &r.MessageIDs, &r.FromConversationID, &r.ToConversationID, &r.CreatedConversation, &r.ClosedSource, &r.SourceStatusBefore,
			&r.TargetStatusBefore, &r.ThreadKeysMoved, &r.GatheringID, &r.DecisionID, &r.MovedByAgentID, &r.MovedByTokenID, &r.MovedAt, &r.UndoneAt,
			&r.UndoneByAgentID, &r.UndoneByTokenID); err != nil {
			rows.Close()
			return fmt.Errorf("export message moves: %w", err)
		}
		r.MovedAt, r.UndoneAt = domain.UTC(r.MovedAt), domain.UTCOptional(r.UndoneAt)
		if err := sink.MessageMove(r); err != nil {
			rows.Close()
			return err
		}
	}
	return rowsError("export message moves", rows)
}
