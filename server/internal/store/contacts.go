package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

func (s *Store) CreateContact(ctx context.Context, workspaceID, name, email string) (domain.Contact, error) {
	var contact domain.Contact
	err := s.pool.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,lower($3))
		RETURNING id::text,name,email,created_at,updated_at`, workspaceID, name, email).
		Scan(&contact.ID, &contact.Name, &contact.Email, &contact.CreatedAt, &contact.UpdatedAt)
	if err != nil {
		return domain.Contact{}, fmt.Errorf("create contact: %w", err)
	}
	contact.CreatedAt, contact.UpdatedAt = domain.UTC(contact.CreatedAt), domain.UTC(contact.UpdatedAt)
	return contact, nil
}

func (s *Store) GetContact(ctx context.Context, workspaceID, id string) (domain.ContactDetail, error) {
	var contact domain.ContactDetail
	err := s.pool.QueryRow(ctx, `SELECT id::text,name,email,created_at,updated_at FROM contacts WHERE workspace_id=$1 AND id=$2`, workspaceID, id).
		Scan(&contact.Contact.ID, &contact.Contact.Name, &contact.Contact.Email, &contact.Contact.CreatedAt, &contact.Contact.UpdatedAt)
	if err != nil {
		return domain.ContactDetail{}, fmt.Errorf("get contact: %w", err)
	}
	contact.Contact.CreatedAt, contact.Contact.UpdatedAt = domain.UTC(contact.Contact.CreatedAt), domain.UTC(contact.Contact.UpdatedAt)
	conversations, err := s.contactConversations(ctx, workspaceID, id)
	if err != nil {
		return domain.ContactDetail{}, err
	}
	contact.Conversations = conversations
	return contact, nil
}

func (s *Store) ListContacts(ctx context.Context, workspaceID, search string) ([]domain.Contact, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text,name,email,created_at,updated_at FROM contacts
		WHERE workspace_id=$1 AND ($2='' OR name ILIKE '%'||$2||'%' ESCAPE '\' OR email ILIKE '%'||$2||'%' ESCAPE '\') ORDER BY name`, workspaceID, escapeLikePattern(search))
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()
	contacts := make([]domain.Contact, 0)
	for rows.Next() {
		var contact domain.Contact
		if err := rows.Scan(&contact.ID, &contact.Name, &contact.Email, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
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

func (s *Store) contactConversations(ctx context.Context, workspaceID, contactID string) ([]domain.Conversation, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM conversations WHERE workspace_id=$1 AND contact_id=$2 ORDER BY updated_at DESC`, workspaceID, contactID)
	if err != nil {
		return nil, fmt.Errorf("list contact conversations: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Conversation, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan contact conversation: %w", err)
		}
		item, err := s.GetConversation(ctx, workspaceID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate contact conversations: %w", err)
	}
	return items, nil
}
