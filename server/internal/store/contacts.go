package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) CreateContact(ctx context.Context, workspaceID, name, email string) (domain.Contact, error) {
	var contact domain.Contact
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,lower($3))
			RETURNING id::text,name,email,organization,created_at,updated_at`, workspaceID, name, email).
			Scan(&contact.ID, &contact.Name, &contact.Email, &contact.Organization, &contact.CreatedAt, &contact.UpdatedAt)
	})
	if err != nil {
		return domain.Contact{}, fmt.Errorf("create contact: %w", err)
	}
	contact.CreatedAt, contact.UpdatedAt = domain.UTC(contact.CreatedAt), domain.UTC(contact.UpdatedAt)
	return contact, nil
}

func (s *Store) GetContact(ctx context.Context, workspaceID, id string) (domain.ContactDetail, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.ContactDetail, error) {
		return getContact(ctx, tx, workspaceID, id)
	})
}

func getContact(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.ContactDetail, error) {
	var contact domain.ContactDetail
	err := tx.QueryRow(ctx, `SELECT id::text,name,email,organization,created_at,updated_at FROM contacts WHERE workspace_id=$1 AND id=$2`, workspaceID, id).
		Scan(&contact.Contact.ID, &contact.Contact.Name, &contact.Contact.Email, &contact.Contact.Organization, &contact.Contact.CreatedAt, &contact.Contact.UpdatedAt)
	if err != nil {
		return domain.ContactDetail{}, fmt.Errorf("get contact: %w", err)
	}
	contact.Contact.CreatedAt, contact.Contact.UpdatedAt = domain.UTC(contact.Contact.CreatedAt), domain.UTC(contact.Contact.UpdatedAt)
	conversations, err := contactConversations(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.ContactDetail{}, err
	}
	contact.Conversations = conversations
	return contact, nil
}

// ContactFilter narrows a contact list. IDs, when not nil, keeps only those contacts; an id that
// names no contact of the workspace is skipped. Limit, when above zero, caps the list.
type ContactFilter struct {
	Search string
	IDs    []pgtype.UUID
	Limit  int
}

// ListContacts returns the contacts in name order. hasMore is true when a limit cut the list: the
// query asks for one row beyond the limit, so no count of all matches is ever made.
func (s *Store) ListContacts(ctx context.Context, workspaceID string, filter ContactFilter) (contacts []domain.Contact, hasMore bool, err error) {
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		contacts, hasMore, err = listContacts(ctx, tx, workspaceID, filter)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return contacts, hasMore, nil
}

const (
	contactColumns = `id::text,name,email,organization,created_at,updated_at`
	contactMatches = `($2='' OR name ILIKE '%'||$2||'%' ESCAPE '\' OR email ILIKE '%'||$2||'%' ESCAPE '\')`
)

func listContacts(ctx context.Context, tx pgx.Tx, workspaceID string, filter ContactFilter) ([]domain.Contact, bool, error) {
	query := `SELECT ` + contactColumns + ` FROM contacts WHERE workspace_id=$1 AND ` + contactMatches + ` ORDER BY name,id`
	args := []any{workspaceID, escapeLikePattern(filter.Search)}
	if filter.IDs != nil {
		// A statement of its own: matching uuid values lets the planner use the (workspace_id, id) index.
		// Comparing id::text defeated it and scanned every contact of every workspace.
		query = `SELECT ` + contactColumns + ` FROM contacts WHERE workspace_id=$1 AND id=ANY($3::uuid[]) AND ` + contactMatches + ` ORDER BY name,id`
		args = append(args, filter.IDs)
	}
	if filter.Limit > 0 {
		args = append(args, filter.Limit+1)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()
	contacts := make([]domain.Contact, 0)
	for rows.Next() {
		var contact domain.Contact
		if err := rows.Scan(&contact.ID, &contact.Name, &contact.Email, &contact.Organization, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
			return nil, false, fmt.Errorf("scan contact: %w", err)
		}
		contact.CreatedAt, contact.UpdatedAt = domain.UTC(contact.CreatedAt), domain.UTC(contact.UpdatedAt)
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate contacts: %w", err)
	}
	hasMore := filter.Limit > 0 && len(contacts) > filter.Limit
	if hasMore {
		contacts = contacts[:filter.Limit]
	}
	return contacts, hasMore, nil
}

func contactConversations(ctx context.Context, tx pgx.Tx, workspaceID, contactID string) ([]domain.Conversation, error) {
	rows, err := tx.Query(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND contact_id=$2 ORDER BY updated_at DESC`, workspaceID, contactID)
	if err != nil {
		return nil, fmt.Errorf("list contact conversations: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan contact conversations: %w", err)
	}
	items := make([]domain.Conversation, 0, len(ids))
	for _, id := range ids {
		item, err := getConversation(ctx, tx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
