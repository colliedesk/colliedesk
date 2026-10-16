package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

type ConversationFilter struct {
	Status   string
	Assignee string
	TagID    string
	Search   string
	AgentID  string
	Page     int
	PageSize int
}

type ConversationPage struct {
	Items []domain.Conversation `json:"items"`
	Page  int                   `json:"page"`
	Size  int                   `json:"page_size"`
}

func (s *Store) CreateConversation(ctx context.Context, workspaceID, contactID, subject string) (domain.Conversation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Conversation, error) {
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject)
			SELECT $1,c.id,$3 FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`, workspaceID, contactID, subject).Scan(&id)
		if err != nil {
			return domain.Conversation{}, fmt.Errorf("create conversation: %w", err)
		}
		return getConversation(ctx, tx, workspaceID, id)
	})
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
	idRows, err := tx.Query(ctx, `SELECT c.id::text FROM conversations c
		LEFT JOIN messages m ON m.workspace_id=c.workspace_id AND m.conversation_id=c.id
		WHERE c.workspace_id=$1 AND ($2='' OR c.status=$2)
		AND ($3='' OR ($3='unassigned' AND c.assignee_id IS NULL) OR ($3='me' AND c.assignee_id=$6) OR ($3='anyone' AND c.assignee_id IS NOT NULL))
		AND ($4='' OR EXISTS(SELECT 1 FROM conversation_tags ct WHERE ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id AND ct.tag_id=$4::uuid))
		AND ($5='' OR c.subject ILIKE '%'||$5||'%' ESCAPE '\' OR m.body ILIKE '%'||$5||'%' ESCAPE '\')
		GROUP BY c.id ORDER BY c.updated_at DESC,c.id DESC LIMIT $7 OFFSET $8`, workspaceID, filter.Status, filter.Assignee, filter.TagID, escapeLikePattern(filter.Search), filter.AgentID, filter.PageSize, (filter.Page-1)*filter.PageSize)
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
