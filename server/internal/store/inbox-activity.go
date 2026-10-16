package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// inboxActivityLookback widens every poll backwards. A conversation's created_at is taken before its
// transaction commits (inbound mail waits on a lock and object storage first), so a poll can move
// its cursor past a row it could not see yet. The browser removes the repeats.
const inboxActivityLookback = "2 minutes"

// InboxActivityLimit caps each activity list; an agent who was away longer reloads the inbox.
const InboxActivityLimit = 50

type NewConversationActivity struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	ContactName string    `json:"contact_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type AssignedConversationActivity struct {
	ID          string    `json:"id"`
	Subject     string    `json:"subject"`
	ContactName string    `json:"contact_name"`
	AssignedAt  time.Time `json:"assigned_at"`
}

// UpdatedConversationActivity is a conversation that changed inside the polled window. IncomingAt is
// the newest customer message in that window and is nil for any other change.
type UpdatedConversationActivity struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	ContactName string     `json:"contact_name"`
	AssigneeID  *string    `json:"assignee_id"`
	UpdatedAt   time.Time  `json:"updated_at"`
	IncomingAt  *time.Time `json:"incoming_at"`
}

// InboxActivity is what happened in the inbox after a cursor. Now is the server clock and the
// cursor for the next call, so browser clock skew never matters.
type InboxActivity struct {
	Now              time.Time                      `json:"now"`
	NewConversations []NewConversationActivity      `json:"new_conversations"`
	AssignedToMe     []AssignedConversationActivity `json:"assigned_to_me"`
	Updated          []UpdatedConversationActivity  `json:"updated"`
}

// InboxActivity lists conversations created after since, conversations somebody else assigned to
// agentID after since, and conversations changed after since. A nil since returns empty lists and the initial cursor.
func (s *Store) InboxActivity(ctx context.Context, workspaceID, agentID string, since *time.Time) (InboxActivity, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (InboxActivity, error) {
		activity := InboxActivity{NewConversations: []NewConversationActivity{}, AssignedToMe: []AssignedConversationActivity{}, Updated: []UpdatedConversationActivity{}}
		// now() is the transaction start and is the same in every statement, so the lists and the
		// cursor agree: a row created after it is returned by the next call, never lost between two.
		if since == nil {
			if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&activity.Now); err != nil {
				return InboxActivity{}, fmt.Errorf("read inbox activity cursor: %w", err)
			}
			activity.Now = domain.UTC(activity.Now)
			return activity, nil
		}
		// One round trip: this endpoint is polled by every open tab.
		batch := &pgx.Batch{}
		batch.Queue(`SELECT now()`)
		batch.Queue(`SELECT c.id::text,c.subject,ct.name,c.created_at
			FROM conversations c JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			WHERE c.workspace_id=$1 AND c.created_at>$2::timestamptz-$4::interval AND c.created_at<=now()
			ORDER BY c.created_at DESC LIMIT $3`, workspaceID, *since, InboxActivityLimit, inboxActivityLookback)
		batch.Queue(`SELECT c.id::text,c.subject,ct.name,c.assigned_at
			FROM conversations c JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			WHERE c.workspace_id=$1 AND c.assignee_id=$5 AND c.assigned_at>$2::timestamptz-$4::interval AND c.assigned_at<=now()
			AND c.assigned_by_agent_id IS DISTINCT FROM $5::uuid
			ORDER BY c.assigned_at DESC LIMIT $3`, workspaceID, *since, InboxActivityLimit, inboxActivityLookback, agentID)
		batch.Queue(`SELECT c.id::text,c.subject,ct.name,c.assignee_id::text,c.updated_at,
			(SELECT max(m.created_at) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id
				AND m.kind='incoming' AND m.created_at>$2::timestamptz-$4::interval AND m.created_at<=now())
			FROM conversations c JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			WHERE c.workspace_id=$1 AND c.updated_at>$2::timestamptz-$4::interval AND c.updated_at<=now()
			ORDER BY c.updated_at DESC LIMIT $3`, workspaceID, *since, InboxActivityLimit, inboxActivityLookback)
		results := tx.SendBatch(ctx, batch)
		defer results.Close()
		if err := results.QueryRow().Scan(&activity.Now); err != nil {
			return InboxActivity{}, fmt.Errorf("read inbox activity cursor: %w", err)
		}
		activity.Now = domain.UTC(activity.Now)
		created, err := results.Query()
		if err != nil {
			return InboxActivity{}, fmt.Errorf("list new conversations: %w", err)
		}
		for created.Next() {
			var item NewConversationActivity
			if err := created.Scan(&item.ID, &item.Subject, &item.ContactName, &item.CreatedAt); err != nil {
				created.Close()
				return InboxActivity{}, fmt.Errorf("scan new conversation: %w", err)
			}
			item.CreatedAt = domain.UTC(item.CreatedAt)
			activity.NewConversations = append(activity.NewConversations, item)
		}
		created.Close()
		if err := created.Err(); err != nil {
			return InboxActivity{}, fmt.Errorf("list new conversations: %w", err)
		}
		assigned, err := results.Query()
		if err != nil {
			return InboxActivity{}, fmt.Errorf("list assigned conversations: %w", err)
		}
		for assigned.Next() {
			var item AssignedConversationActivity
			if err := assigned.Scan(&item.ID, &item.Subject, &item.ContactName, &item.AssignedAt); err != nil {
				assigned.Close()
				return InboxActivity{}, fmt.Errorf("scan assigned conversation: %w", err)
			}
			item.AssignedAt = domain.UTC(item.AssignedAt)
			activity.AssignedToMe = append(activity.AssignedToMe, item)
		}
		assigned.Close()
		if err := assigned.Err(); err != nil {
			return InboxActivity{}, fmt.Errorf("list assigned conversations: %w", err)
		}
		updated, err := results.Query()
		if err != nil {
			return InboxActivity{}, fmt.Errorf("list updated conversations: %w", err)
		}
		for updated.Next() {
			var item UpdatedConversationActivity
			if err := updated.Scan(&item.ID, &item.Subject, &item.ContactName, &item.AssigneeID, &item.UpdatedAt, &item.IncomingAt); err != nil {
				updated.Close()
				return InboxActivity{}, fmt.Errorf("scan updated conversation: %w", err)
			}
			item.UpdatedAt = domain.UTC(item.UpdatedAt)
			if item.IncomingAt != nil {
				incoming := domain.UTC(*item.IncomingAt)
				item.IncomingAt = &incoming
			}
			activity.Updated = append(activity.Updated, item)
		}
		updated.Close()
		if err := updated.Err(); err != nil {
			return InboxActivity{}, fmt.Errorf("list updated conversations: %w", err)
		}
		return activity, nil
	})
}
