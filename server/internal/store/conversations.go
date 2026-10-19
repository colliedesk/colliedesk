package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

type ConversationFilter struct {
	Status   string
	Assignee string
	TagID    string
	Search   string
	// ContactID, when set, keeps the conversations of one contact (its history).
	ContactID string
	AgentID   string
	Page      int
	PageSize  int
}

type ConversationPage struct {
	Items []domain.Conversation `json:"items"`
	Page  int                   `json:"page"`
	Size  int                   `json:"page_size"`
}

func (s *Store) CreateConversation(ctx context.Context, workspaceID, contactID, subject string) (domain.Conversation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Conversation, error) {
		if err := checkContactNotMerged(ctx, tx, workspaceID, contactID); err != nil {
			return domain.Conversation{}, err
		}
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject)
			SELECT $1,c.id,$3 FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`, workspaceID, contactID, subject).Scan(&id)
		if err != nil {
			return domain.Conversation{}, fmt.Errorf("create conversation: %w", err)
		}
		return getConversation(ctx, tx, workspaceID, id)
	})
}

// NewConversation is a conversation the team starts, with a contact of the workspace (ContactID) or a
// customer added with it (NewContact), in the same transaction.
type NewConversation struct {
	ContactID  string
	NewContact *NewContact
	Subject    string
}

// NewContact is a customer added while starting a conversation.
type NewContact struct {
	Name, Email string
}

// StartConversation opens a conversation with its first reply in one transaction, so a reply the email
// rules refuse (trial, Cc, storage) leaves no empty conversation behind. The reply is emailed through the
// outbox like any other; a workspace without an email channel is refused, because the reply would be
// stored and never sent.
func (s *Store) StartConversation(ctx context.Context, workspaceID string, in NewConversation, actor Actor, body string, cc []string, files ...NewAttachment) (domain.Conversation, error) {
	if len(cc) == 0 {
		cc = nil
	}
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("begin conversation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	contactID := in.ContactID
	if in.NewContact != nil {
		added, err := insertContact(ctx, tx, workspaceID, in.NewContact.Name, in.NewContact.Email)
		if err != nil {
			return domain.Conversation{}, err
		}
		contactID = added.ID
	} else if err := checkContactNotMerged(ctx, tx, workspaceID, contactID); err != nil {
		return domain.Conversation{}, err
	}
	var channel bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_channels WHERE workspace_id=$1)`, workspaceID).Scan(&channel); err != nil {
		return domain.Conversation{}, fmt.Errorf("load email channel: %w", err)
	}
	if !channel {
		return domain.Conversation{}, domain.ErrEmailChannelMissing
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject)
		SELECT $1,c.id,$3 FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
		workspaceID, contactID, in.Subject).Scan(&id)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	if _, err := s.createMessage(ctx, tx, workspaceID, id, actor, "outgoing", body, cc, files); err != nil {
		return domain.Conversation{}, err
	}
	conversation, err := getConversation(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Conversation{}, fmt.Errorf("commit conversation: %w", err)
	}
	return conversation, nil
}

func (s *Store) GetConversation(ctx context.Context, workspaceID, id string) (domain.Conversation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Conversation, error) {
		return getConversation(ctx, tx, workspaceID, id)
	})
}

func getConversation(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.Conversation, error) {
	var item domain.Conversation
	err := tx.QueryRow(ctx, `SELECT c.id::text,c.contact_id::text,k.name,c.subject,c.status,c.priority,c.assignee_id::text,c.origin,c.created_at,c.updated_at
		FROM conversations c JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, id).
		Scan(&item.ID, &item.ContactID, &item.ContactName, &item.Subject, &item.Status, &item.Priority, &item.AssigneeID, &item.Origin, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
	tags, err := conversationTags(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.Conversation{}, err
	}
	item.Tags = tags
	return item, nil
}

func (s *Store) ListConversations(ctx context.Context, workspaceID string, filter ConversationFilter) (ConversationPage, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ConversationPage, error) {
		return listConversations(ctx, tx, workspaceID, filter)
	})
}

func listConversations(ctx context.Context, tx pgx.Tx, workspaceID string, filter ConversationFilter) (ConversationPage, error) {
	query, args := conversationIDQuery(workspaceID, filter)
	idRows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("list conversation ids: %w", err)
	}
	ids := make([]string, 0, filter.PageSize)
	for idRows.Next() {
		var id string
		if err := idRows.Scan(&id); err != nil {
			idRows.Close()
			return ConversationPage{}, fmt.Errorf("scan conversation id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := idRows.Err(); err != nil {
		idRows.Close()
		return ConversationPage{}, fmt.Errorf("iterate conversation ids: %w", err)
	}
	idRows.Close()
	if len(ids) == 0 {
		return ConversationPage{Items: []domain.Conversation{}, Page: filter.Page, Size: filter.PageSize}, nil
	}

	rows, err := tx.Query(ctx, `SELECT c.id::text,c.contact_id::text,k.name,c.subject,c.status,c.priority,
		c.assignee_id::text,c.origin,c.created_at,c.updated_at,COALESCE(tag_data.tags,'[]'::jsonb)
		FROM conversations c
		JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(jsonb_build_object('id',t.id::text,'name',t.name,'color',t.color) ORDER BY t.name) AS tags
			FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id AND t.workspace_id=ct.workspace_id
			WHERE ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id
		) tag_data ON true
		WHERE c.workspace_id=$1 AND c.id=ANY($2::uuid[])
		ORDER BY c.updated_at DESC,c.id DESC`, workspaceID, ids)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Conversation, 0)
	for rows.Next() {
		var item domain.Conversation
		var tagsJSON []byte
		if err := rows.Scan(&item.ID, &item.ContactID, &item.ContactName, &item.Subject, &item.Status, &item.Priority,
			&item.AssigneeID, &item.Origin, &item.CreatedAt, &item.UpdatedAt, &tagsJSON); err != nil {
			return ConversationPage{}, fmt.Errorf("scan conversation: %w", err)
		}
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return ConversationPage{}, fmt.Errorf("decode conversation tags: %w", err)
		}
		item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ConversationPage{}, fmt.Errorf("iterate conversations: %w", err)
	}
	return ConversationPage{Items: items, Page: filter.Page, Size: filter.PageSize}, nil
}

// conversationIDQuery selects one page of conversation ids, newest activity first, with only the
// conditions the filter sets, so each list uses its index (the contact's history reads
// conversations(workspace_id, contact_id, updated_at)). Messages are read only for a search, and then
// through EXISTS: joining them made every list cost as much as all the workspace's messages.
func conversationIDQuery(workspaceID string, filter ConversationFilter) (string, []any) {
	args := []any{workspaceID}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	where := []string{"c.workspace_id=$1"}
	if filter.Status != "" {
		where = append(where, "c.status="+arg(filter.Status))
	}
	switch filter.Assignee {
	case "unassigned":
		where = append(where, "c.assignee_id IS NULL")
	case "me":
		where = append(where, "c.assignee_id=NULLIF("+arg(filter.AgentID)+",'')::uuid")
	case "anyone":
		where = append(where, "c.assignee_id IS NOT NULL")
	}
	if filter.TagID != "" {
		where = append(where, "EXISTS(SELECT 1 FROM conversation_tags ct WHERE ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id AND ct.tag_id="+arg(filter.TagID)+"::uuid)")
	}
	if filter.ContactID != "" {
		where = append(where, "c.contact_id="+arg(filter.ContactID)+"::uuid")
	}
	if filter.Search != "" {
		pattern := arg(escapeLikePattern(filter.Search))
		where = append(where, "(c.subject ILIKE '%'||"+pattern+"||'%' ESCAPE '\\' OR EXISTS(SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id "+
			"AND m.conversation_id=c.id AND m.body ILIKE '%'||"+pattern+"||'%' ESCAPE '\\'))")
	}
	limit, offset := arg(filter.PageSize), arg((filter.Page-1)*filter.PageSize)
	return "SELECT c.id::text FROM conversations c WHERE " + strings.Join(where, " AND ") +
		" ORDER BY c.updated_at DESC,c.id DESC LIMIT " + limit + " OFFSET " + offset, args
}

func conversationTags(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) ([]domain.Tag, error) {
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.name,t.color FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id AND t.workspace_id=ct.workspace_id
		WHERE ct.workspace_id=$1 AND ct.conversation_id=$2 ORDER BY t.name`, workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list conversation tags: %w", err)
	}
	defer rows.Close()
	tags := make([]domain.Tag, 0)
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, fmt.Errorf("scan conversation tag: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation tags: %w", err)
	}
	return tags, nil
}
