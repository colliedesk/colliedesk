package helpscout

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// contactFor returns the Colliedesk contact of a conversation's primary customer, linking it to the
// workspace contact with the same email or creating it. The conversation carries the customer's
// email; when it does not (phone and chat conversations), the customer record is read.
func (r *run) contactFor(ctx context.Context, ref hsConversation) (string, error) {
	person := ref.PrimaryCustomer
	key := strconv.FormatInt(person.ID, 10)
	if id, ok, err := r.ledger.done(ctx, entityContact, key); err != nil || ok {
		return id, err
	}
	name := fullName(person.First, person.Last)
	address := strings.ToLower(strings.TrimSpace(person.Email))
	created, updated := ref.CreatedAt.UTC(), ref.CreatedAt.UTC()
	if address == "" || name == "" {
		var customer hsCustomer
		err := r.client.getJSON(ctx, "/v2/customers/"+key, nil, &customer)
		switch {
		case errors.Is(err, errNotFound):
			r.warn("customer_not_found", "Conversation's customer no longer exists in Help Scout; a contact was created from what the conversation holds.", key)
		case err != nil:
			r.itemFailed(ctx, entityContact, key, "", err)
			return "", err
		default:
			if name == "" {
				name = fullName(customer.FirstName, customer.LastName)
			}
			if address == "" && len(customer.Embedded.Emails) > 0 {
				address = strings.ToLower(strings.TrimSpace(customer.Embedded.Emails[0].Value))
			}
			if !customer.CreatedAt.IsZero() {
				created, updated = customer.CreatedAt.UTC(), customer.UpdatedAt.UTC()
			}
		}
	}
	if address == "" {
		address = "helpscout-customer-" + key + "@import.invalid"
		r.warn("contact_without_email", "Help Scout customer has no email; Colliedesk gave it a reserved .invalid address.", key)
	}
	if name == "" {
		name = address
	}
	if updated.Before(created) {
		updated = created
	}
	id, err := r.writeContact(ctx, key, name, address, created, updated)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		r.itemFailed(ctx, entityContact, key, "", err)
		return "", err
	}
	return id, nil
}

func (r *run) writeContact(ctx context.Context, key, name, address string, created, updated time.Time) (string, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin contact import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var id string
	var meta *itemMeta
	err = tx.QueryRow(ctx, `SELECT id::text FROM contacts WHERE workspace_id=$1 AND email=$2`, r.workspaceID, address).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		meta = &itemMeta{Created: true}
		err = tx.QueryRow(ctx, `INSERT INTO contacts(workspace_id,name,email,created_at,updated_at) VALUES($1,$2,$3,$4,$5) RETURNING id::text`,
			r.workspaceID, name, address, created, updated).Scan(&id)
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
