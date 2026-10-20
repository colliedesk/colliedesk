package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) CreateContact(ctx context.Context, workspaceID, name, email string) (domain.Contact, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Contact, error) {
		return insertContact(ctx, tx, workspaceID, name, email)
	})
}

// insertContact adds a contact inside tx. An address that is already a contact is refused with a
// *domain.ContactExistsError naming the contact to use: the one with the address, or the one it was
// merged into (a merged contact is left out of searches, so it would be a dead end).
func insertContact(ctx context.Context, tx pgx.Tx, workspaceID, name, email string) (domain.Contact, error) {
	contact, err := scanContact(tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,lower($3))
		ON CONFLICT(workspace_id,email) DO NOTHING RETURNING `+contactColumns, workspaceID, name, email))
	if !errors.Is(err, pgx.ErrNoRows) {
		if err != nil {
			return domain.Contact{}, fmt.Errorf("create contact: %w", err)
		}
		return contact, nil
	}
	use, err := scanContact(tx.QueryRow(ctx, `SELECT `+contactColumns+` FROM contacts WHERE workspace_id=$1 AND email=lower($2)`, workspaceID, email))
	if err == nil && use.MergedIntoID != nil {
		use, err = contactByID(ctx, tx, workspaceID, *use.MergedIntoID)
	}
	if err != nil {
		return domain.Contact{}, fmt.Errorf("find existing contact: %w", err)
	}
	return domain.Contact{}, &domain.ContactExistsError{Use: use}
}

// contactForEmail finds the contact of an address, or adds it with name, for mail and forms that arrive
// from it. A contact merged into another answers with the one it was merged into, so new mail from a
// merged address joins the kept contact instead of the duplicate.
func contactForEmail(ctx context.Context, tx pgx.Tx, workspaceID, name, email string) (string, error) {
	var contactID string
	err := tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email) VALUES($1,$2,$3)
		ON CONFLICT(workspace_id,email) DO UPDATE SET email=contacts.email RETURNING COALESCE(merged_into_id,id)::text`, workspaceID, name, email).Scan(&contactID)
	if err != nil {
		return "", fmt.Errorf("find or create contact: %w", err)
	}
	return contactID, nil
}

func (s *Store) GetContact(ctx context.Context, workspaceID, id string) (domain.ContactDetail, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.ContactDetail, error) {
		return getContact(ctx, tx, workspaceID, id)
	})
}

func getContact(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.ContactDetail, error) {
	var detail domain.ContactDetail
	contact, err := contactByID(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.ContactDetail{}, fmt.Errorf("get contact: %w", err)
	}
	detail.Contact = contact
	if contact.MergedIntoID != nil {
		into, err := contactByID(ctx, tx, workspaceID, *contact.MergedIntoID)
		if err != nil {
			return domain.ContactDetail{}, fmt.Errorf("get merged into contact: %w", err)
		}
		detail.MergedInto = &into
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE workspace_id=$1 AND contact_id=$2`, workspaceID, id).Scan(&detail.ConversationCount); err != nil {
		return domain.ContactDetail{}, fmt.Errorf("count contact conversations: %w", err)
	}
	if detail.Merges, err = contactMerges(ctx, tx, workspaceID, id); err != nil {
		return domain.ContactDetail{}, err
	}
	return detail, nil
}

const contactColumns = `id::text,name,email,organization,merged_into_id::text,created_at,updated_at`

func scanContact(row pgx.Row) (domain.Contact, error) {
	var contact domain.Contact
	if err := row.Scan(&contact.ID, &contact.Name, &contact.Email, &contact.Organization, &contact.MergedIntoID, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
		return domain.Contact{}, err
	}
	contact.CreatedAt, contact.UpdatedAt = domain.UTC(contact.CreatedAt), domain.UTC(contact.UpdatedAt)
	return contact, nil
}

func contactByID(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.Contact, error) {
	return scanContact(tx.QueryRow(ctx, `SELECT `+contactColumns+` FROM contacts WHERE workspace_id=$1 AND id=$2`, workspaceID, id))
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

const contactMatches = `($2='' OR name ILIKE '%'||$2||'%' ESCAPE '\' OR email ILIKE '%'||$2||'%' ESCAPE '\')`

// mergedMatches keeps a contact some merged contact of which matches the search, so the duplicate's
// own name or address still finds the contact it was merged into.
const mergedMatches = `($2<>'' AND id IN (SELECT d.merged_into_id FROM contacts d WHERE d.workspace_id=$1 AND d.merged_into_id IS NOT NULL
	AND (d.name ILIKE '%'||$2||'%' ESCAPE '\' OR d.email ILIKE '%'||$2||'%' ESCAPE '\')))`

func listContacts(ctx context.Context, tx pgx.Tx, workspaceID string, filter ContactFilter) ([]domain.Contact, bool, error) {
	// A merged contact is no longer a customer of its own: it is left out, and a search that matches it
	// finds the contact it was merged into instead.
	query := `SELECT ` + contactColumns + ` FROM contacts WHERE workspace_id=$1 AND merged_into_id IS NULL AND (` + contactMatches + ` OR ` + mergedMatches + `) ORDER BY name,id`
	args := []any{workspaceID, escapeLikePattern(filter.Search)}
	if filter.IDs != nil {
		// A statement of its own: matching uuid values lets the planner use the (workspace_id, id) index.
		// Comparing id::text defeated it and scanned every contact of every workspace. Named ids are
		// returned merged or not.
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
		contact, err := scanContact(rows)
		if err != nil {
			return nil, false, fmt.Errorf("scan contact: %w", err)
		}
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

// RenameContact changes the name the team sees and later emails address; the email stays.
func (s *Store) RenameContact(ctx context.Context, workspaceID, id, name string) (domain.Contact, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Contact, error) {
		contact, err := scanContact(tx.QueryRow(ctx, `UPDATE contacts SET name=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2
			RETURNING `+contactColumns, workspaceID, id, name))
		if err != nil {
			return domain.Contact{}, fmt.Errorf("rename contact: %w", err)
		}
		return contact, nil
	})
}

const contactMergeColumns = `m.id::text,m.contact_id::text,m.conversation_ids::text[],m.merged_by_agent_id::text,m.created_at,m.undone_at,
	k.id::text,k.name,k.email,k.organization,k.merged_into_id::text,k.created_at,k.updated_at`

func scanContactMerge(row pgx.Row) (domain.ContactMerge, error) {
	var merge domain.ContactMerge
	contact := &merge.MergedContact
	if err := row.Scan(&merge.ID, &merge.ContactID, &merge.ConversationIDs, &merge.MergedByAgentID, &merge.MergedAt, &merge.UndoneAt,
		&contact.ID, &contact.Name, &contact.Email, &contact.Organization, &contact.MergedIntoID, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
		return domain.ContactMerge{}, err
	}
	merge.MergedAt = domain.UTC(merge.MergedAt)
	if merge.UndoneAt != nil {
		undone := domain.UTC(*merge.UndoneAt)
		merge.UndoneAt = &undone
	}
	contact.CreatedAt, contact.UpdatedAt = domain.UTC(contact.CreatedAt), domain.UTC(contact.UpdatedAt)
	return merge, nil
}

func contactMerges(ctx context.Context, tx pgx.Tx, workspaceID, contactID string) ([]domain.ContactMerge, error) {
	rows, err := tx.Query(ctx, `SELECT `+contactMergeColumns+` FROM contact_merges m
		JOIN contacts k ON k.workspace_id=m.workspace_id AND k.id=m.merged_contact_id
		WHERE m.workspace_id=$1 AND m.contact_id=$2 AND m.undone_at IS NULL ORDER BY m.created_at DESC,m.id`, workspaceID, contactID)
	if err != nil {
		return nil, fmt.Errorf("list contact merges: %w", err)
	}
	merges, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.ContactMerge, error) { return scanContactMerge(row) })
	if err != nil {
		return nil, fmt.Errorf("scan contact merges: %w", err)
	}
	return merges, nil
}

func getContactMerge(ctx context.Context, tx pgx.Tx, workspaceID, mergeID string) (domain.ContactMerge, error) {
	merge, err := scanContactMerge(tx.QueryRow(ctx, `SELECT `+contactMergeColumns+` FROM contact_merges m
		JOIN contacts k ON k.workspace_id=m.workspace_id AND k.id=m.merged_contact_id WHERE m.workspace_id=$1 AND m.id=$2`, workspaceID, mergeID))
	if err != nil {
		return domain.ContactMerge{}, fmt.Errorf("get contact merge: %w", err)
	}
	return merge, nil
}

// lockContacts locks the given contacts of the workspace in id order, so two merges over the same pair
// wait for each other instead of deadlocking, and returns them by id. A contact missing from the
// workspace is a not-found error.
func lockContacts(ctx context.Context, tx pgx.Tx, workspaceID string, ids ...string) (map[string]domain.Contact, error) {
	rows, err := tx.Query(ctx, `SELECT `+contactColumns+` FROM contacts WHERE workspace_id=$1 AND id=ANY($2::uuid[]) ORDER BY id FOR UPDATE`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("lock contacts: %w", err)
	}
	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Contact, error) { return scanContact(row) })
	if err != nil {
		return nil, fmt.Errorf("lock contacts: %w", err)
	}
	byID := make(map[string]domain.Contact, len(found))
	for _, contact := range found {
		byID[contact.ID] = contact
	}
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			return nil, fmt.Errorf("lock contacts: %w", pgx.ErrNoRows)
		}
	}
	return byID, nil
}

// MergeContacts merges the duplicate into the kept contact: every conversation of the duplicate moves to
// the kept contact, the duplicate points at it and keeps its name and email, and the merge records the
// moved conversations so UndoContactMerge can move exactly those back. Moving a conversation does not
// change its last activity.
func (s *Store) MergeContacts(ctx context.Context, workspaceID, keptID, duplicateID string, actor Actor) (domain.ContactMerge, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.ContactMerge, error) {
		if keptID == duplicateID {
			return domain.ContactMerge{}, &domain.ContactMergeError{Reason: "A contact cannot be merged into itself."}
		}
		contacts, err := lockContacts(ctx, tx, workspaceID, keptID, duplicateID)
		if err != nil {
			return domain.ContactMerge{}, err
		}
		kept, duplicate := contacts[keptID], contacts[duplicateID]
		switch {
		case kept.MergedIntoID != nil:
			return domain.ContactMerge{}, &domain.ContactMergeError{Reason: kept.Email + " is merged into another contact. Merge into that contact instead."}
		case duplicate.MergedIntoID != nil:
			return domain.ContactMerge{}, &domain.ContactMergeError{Reason: duplicate.Email + " is merged into another contact already. Undo that merge first."}
		}
		var mergedIn bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM contacts WHERE workspace_id=$1 AND merged_into_id=$2)`, workspaceID, duplicateID).Scan(&mergedIn); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("check merged contacts: %w", err)
		}
		if mergedIn {
			return domain.ContactMerge{}, &domain.ContactMergeError{Reason: "Other contacts are merged into " + duplicate.Email + ". Undo those merges first, or merge them into this contact one by one."}
		}
		// Rows are locked in (workspace_id, id) order, the order the last_public trigger on messages locks them
		// in, so a merge and a statement writing messages of several of these conversations never deadlock.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM conversations WHERE workspace_id=$1 AND contact_id=$2 ORDER BY workspace_id,id FOR NO KEY UPDATE`,
			workspaceID, duplicateID); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("lock conversations: %w", err)
		}
		rows, err := tx.Query(ctx, `UPDATE conversations SET contact_id=$3 WHERE workspace_id=$1 AND contact_id=$2 RETURNING id::text`, workspaceID, duplicateID, keptID)
		if err != nil {
			return domain.ContactMerge{}, fmt.Errorf("move conversations: %w", err)
		}
		moved, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return domain.ContactMerge{}, fmt.Errorf("move conversations: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE contacts SET merged_into_id=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, duplicateID, keptID); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("mark contact merged: %w", err)
		}
		var mergeID string
		if err := tx.QueryRow(ctx, `INSERT INTO contact_merges(workspace_id,contact_id,merged_contact_id,conversation_ids,merged_by_agent_id,token_id)
			VALUES($1,$2,$3,$4::uuid[],NULLIF($5,'')::uuid,NULLIF($6,'')::uuid) RETURNING id::text`,
			workspaceID, keptID, duplicateID, moved, actor.AgentID, actor.TokenID).Scan(&mergeID); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("record contact merge: %w", err)
		}
		return getContactMerge(ctx, tx, workspaceID, mergeID)
	})
}

// UndoContactMerge undoes a merge into keptID: the conversations it moved that are still on the kept
// contact go back to the duplicate, which is a contact of its own again. Conversations that arrived
// after the merge stay where they are.
func (s *Store) UndoContactMerge(ctx context.Context, workspaceID, keptID, mergeID, agentID string) (domain.ContactMerge, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.ContactMerge, error) {
		var duplicateID string
		var undone bool
		var moved []string
		err := tx.QueryRow(ctx, `SELECT merged_contact_id::text,undone_at IS NOT NULL,conversation_ids::text[] FROM contact_merges
			WHERE workspace_id=$1 AND id=$2 AND contact_id=$3 FOR UPDATE`, workspaceID, mergeID, keptID).Scan(&duplicateID, &undone, &moved)
		if err != nil {
			return domain.ContactMerge{}, fmt.Errorf("find contact merge: %w", err)
		}
		if undone {
			return domain.ContactMerge{}, domain.ErrMergeUndone
		}
		if _, err := lockContacts(ctx, tx, workspaceID, keptID, duplicateID); err != nil {
			return domain.ContactMerge{}, err
		}
		// Locked in the trigger's order first, as in MergeContacts.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM conversations WHERE workspace_id=$1 AND contact_id=$2 AND id=ANY($3::uuid[]) ORDER BY workspace_id,id FOR NO KEY UPDATE`,
			workspaceID, keptID, moved); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("lock conversations: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET contact_id=$3 WHERE workspace_id=$1 AND contact_id=$2 AND id=ANY($4::uuid[])`,
			workspaceID, keptID, duplicateID, moved); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("move conversations back: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE contacts SET merged_into_id=NULL,updated_at=now() WHERE workspace_id=$1 AND id=$2`, workspaceID, duplicateID); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("unmark contact merged: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE contact_merges SET undone_at=now(),undone_by_agent_id=NULLIF($3,'')::uuid WHERE workspace_id=$1 AND id=$2`,
			workspaceID, mergeID, agentID); err != nil {
			return domain.ContactMerge{}, fmt.Errorf("record undone merge: %w", err)
		}
		return getContactMerge(ctx, tx, workspaceID, mergeID)
	})
}

// checkContactNotMerged refuses to open a conversation with a contact merged into another.
func checkContactNotMerged(ctx context.Context, tx pgx.Tx, workspaceID, contactID string) error {
	var mergedInto *string
	err := tx.QueryRow(ctx, `SELECT merged_into_id::text FROM contacts WHERE workspace_id=$1 AND id=$2`, workspaceID, contactID).Scan(&mergedInto)
	if err != nil {
		return fmt.Errorf("find contact: %w", err)
	}
	if mergedInto != nil {
		return &domain.ContactMergeError{Reason: "This contact is merged into another. Start the conversation with the contact it was merged into."}
	}
	return nil
}
