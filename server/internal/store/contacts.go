package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
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
		return getContact(ctx, tx, workspaceID, id, s.now())
	})
}

func getContact(ctx context.Context, tx pgx.Tx, workspaceID, id string, now time.Time) (domain.ContactDetail, error) {
	var contact domain.ContactDetail
	err := tx.QueryRow(ctx, `SELECT id::text,name,email,organization,created_at,updated_at FROM contacts WHERE workspace_id=$1 AND id=$2`, workspaceID, id).
		Scan(&contact.Contact.ID, &contact.Contact.Name, &contact.Contact.Email, &contact.Contact.Organization, &contact.Contact.CreatedAt, &contact.Contact.UpdatedAt)
	if err != nil {
		return domain.ContactDetail{}, fmt.Errorf("get contact: %w", err)
	}
	contact.Contact.CreatedAt, contact.Contact.UpdatedAt = domain.UTC(contact.Contact.CreatedAt), domain.UTC(contact.Contact.UpdatedAt)
	conversations, err := contactConversations(ctx, tx, workspaceID, id, now)
	if err != nil {
		return domain.ContactDetail{}, err
	}
	contact.Conversations = conversations
	return contact, nil
}

func (s *Store) ListContacts(ctx context.Context, workspaceID, search string) ([]domain.Contact, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Contact, error) {
		return listContacts(ctx, tx, workspaceID, search)
	})
}

func listContacts(ctx context.Context, tx pgx.Tx, workspaceID, search string) ([]domain.Contact, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,name,email,organization,created_at,updated_at FROM contacts
		WHERE workspace_id=$1 AND ($2='' OR name ILIKE '%'||$2||'%' ESCAPE '\' OR email ILIKE '%'||$2||'%' ESCAPE '\') ORDER BY name`, workspaceID, escapeLikePattern(search))
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()
	contacts := make([]domain.Contact, 0)
	for rows.Next() {
		var contact domain.Contact
		if err := rows.Scan(&contact.ID, &contact.Name, &contact.Email, &contact.Organization, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contact.CreatedAt, contact.UpdatedAt = domain.UTC(contact.CreatedAt), domain.UTC(contact.UpdatedAt)
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contacts: %w", err)
	}
	return contacts, nil
}

func contactConversations(ctx context.Context, tx pgx.Tx, workspaceID, contactID string, now time.Time) ([]domain.Conversation, error) {
	rows, err := tx.Query(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND contact_id=$2 AND (held_at IS NULL OR held_at < $3) ORDER BY updated_at DESC`, workspaceID, contactID, domain.MonthStart(now))
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
