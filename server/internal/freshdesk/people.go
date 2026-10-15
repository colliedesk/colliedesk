package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (r *run) loadAgents(ctx context.Context) error {
	local := map[string]string{}
	tx, err := r.begin(ctx)
	if err != nil {
		return fmt.Errorf("list Colliedesk agents: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // also covers a panic; a second rollback is a no-op
	rows, err := tx.Query(ctx, `SELECT id::text,lower(email) FROM agents WHERE workspace_id=$1`, r.workspaceID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("list Colliedesk agents: %w", err)
	}
	for rows.Next() {
		var id, email string
		if err := rows.Scan(&id, &email); err != nil {
			rows.Close()
			_ = tx.Rollback(ctx)
			return fmt.Errorf("scan Colliedesk agent: %w", err)
		}
		local[email] = id
	}
	rows.Close()
	_ = tx.Rollback(ctx) // read only; end it before the Freshdesk calls below
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list Colliedesk agents: %w", err)
	}
	return listAll(ctx, r.client, r.endpoint("/api/v2/agents", pageQuery()), func(agent fdAgent) error {
		r.agents[agent.ID] = agent
		if id, ok := local[strings.ToLower(strings.TrimSpace(agent.Contact.Email))]; ok {
			r.agentTargets[agent.ID] = id
		} else {
			r.warn("agent_not_matched", "Freshdesk agent has no Colliedesk agent with the same email; their replies keep the author name only. Invite them, then re-run to keep future items linked.", strconv.FormatInt(agent.ID, 10))
		}
		return nil
	})
}

func (r *run) loadCompanies(ctx context.Context) error {
	return listAll(ctx, r.client, r.endpoint("/api/v2/companies", pageQuery()), func(company fdCompany) error {
		r.companies[company.ID] = company.Name
		return nil
	})
}

func (r *run) importContacts(ctx context.Context) error {
	err := listAll(ctx, r.client, r.endpoint("/api/v2/contacts", pageQuery()), func(contact fdContact) error {
		r.listedContacts++
		r.progress.tick("contacts", r.listedContacts, 0)
		if contact.CompanyID != nil {
			if _, known := r.companies[*contact.CompanyID]; known {
				r.usedCompanies[*contact.CompanyID] = true
			}
		}
		if r.opts.DryRun {
			r.contactIDs = append(r.contactIDs, strconv.FormatInt(contact.ID, 10))
			r.contactNames[contact.ID] = contact.Name
			return nil
		}
		// Item failures are already in the ledger; only cancellation stops the list.
		if _, err := r.importContact(ctx, contact); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	})
	if err != nil {
		return err
	}
	for id := range r.companies {
		if !r.usedCompanies[id] {
			r.warn("company_without_contacts", "Freshdesk company has no contacts, so its name is not kept anywhere in Colliedesk.", strconv.FormatInt(id, 10))
		}
	}
	return nil
}

// requesterContact returns the Colliedesk contact for a Freshdesk requester,
// fetching contacts that the list endpoint did not return (for example deleted ones).
func (r *run) requesterContact(ctx context.Context, freshdeskID int64) (string, error) {
	key := strconv.FormatInt(freshdeskID, 10)
	if id, ok, err := r.ledger.done(ctx, entityContact, key); err != nil || ok {
		if ok && r.contactNames[freshdeskID] == "" {
			// Keep the author name for new messages from a contact the list does not return.
			var name string
			err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT name FROM contacts WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id).Scan(&name)
			})
			if err == nil {
				r.contactNames[freshdeskID] = name
			}
		}
		return id, err
	}
	var contact fdContact
	_, err := r.client.getJSON(ctx, r.endpoint("/api/v2/contacts/"+key, nil), &contact)
	switch {
	case errors.Is(err, errNotFound):
		contact = fdContact{ID: freshdeskID, Name: "Freshdesk requester " + key}
		if agent, ok := r.agents[freshdeskID]; ok {
			email := agent.Contact.Email
			contact.Name, contact.Email = agent.Contact.Name, &email
		}
		r.warn("requester_not_found", "Ticket requester no longer exists in Freshdesk; a contact was created from what is known.", key)
	case err != nil:
		return "", err
	}
	return r.importContact(ctx, contact)
}

// importContact links a Freshdesk contact to the workspace contact with the
// same email, creating it with its original timestamps when it is new.
func (r *run) importContact(ctx context.Context, contact fdContact) (string, error) {
	key := strconv.FormatInt(contact.ID, 10)
	r.contactIDs = append(r.contactIDs, key)
	r.contactNames[contact.ID] = contact.Name
	if id, ok, err := r.ledger.done(ctx, entityContact, key); err != nil {
		r.itemFailed(ctx, entityContact, key, "", err)
		return "", err
	} else if ok {
		return id, nil
	}
	email := ""
	if contact.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*contact.Email))
	}
	if email == "" {
		email = "freshdesk-contact-" + key + "@import.invalid"
		r.warn("contact_without_email", "Freshdesk contact has no email; Colliedesk gave it a reserved .invalid address.", key)
	}
	name := strings.TrimSpace(contact.Name)
	if name == "" {
		name = email
	}
	var organization *string
	if contact.CompanyID != nil {
		if company, ok := r.companies[*contact.CompanyID]; ok {
			organization = &company
		}
	}
	created, updated := orNow(contact.CreatedAt), orNow(contact.UpdatedAt)
	id, err := r.writeContact(ctx, key, name, email, organization, created, updated)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		r.itemFailed(ctx, entityContact, key, "", err)
		return "", err
	}
	return id, nil
}

func (r *run) writeContact(ctx context.Context, key, name, email string, organization *string, created, updated time.Time) (string, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin contact import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	var meta *ticketMeta
	err = tx.QueryRow(ctx, `SELECT id::text FROM contacts WHERE workspace_id=$1 AND email=$2`, r.workspaceID, email).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		meta = &ticketMeta{Created: true}
		err = tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email,organization,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, r.workspaceID, name, email, organization, created, updated).Scan(&id)
	case err == nil:
		_, err = tx.Exec(ctx, `UPDATE contacts SET organization=COALESCE(organization,$3) WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id, organization)
	}
	if err != nil {
		return "", fmt.Errorf("write contact: %w", err)
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityContact, sourceID: key, targetID: &id, status: ledgerDone, meta: meta}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit contact import: %w", err)
	}
	return id, nil
}

// itemFailed records a per-item failure. Cancellation is not a failure of the item.
func (r *run) itemFailed(ctx context.Context, entity, sourceID, parentID string, cause error) {
	if ctx.Err() != nil {
		return
	}
	err := r.ledger.mark(ctx, nil, ledgerMark{entity: entity, sourceID: sourceID, parentID: parentID, status: ledgerFailed, errText: cause.Error()})
	if err != nil && r.internalErr == nil {
		r.internalErr = err
	}
}

func orNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}
