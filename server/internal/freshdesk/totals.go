package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

// Freshdesk launched in 2010, so this date matches every ticket.
const allTicketsQuery = `"created_at:>'2000-01-01'"`

// verifyTotals cross-checks what the importer found against totals Freshdesk
// reports through its filter (search) endpoints. A total that cannot be read,
// or that is larger than what was found, leaves the report UNVERIFIED.
func (r *run) verifyTotals(ctx context.Context) error {
	tickets := Verification{Verified: !r.archiveUnavailable.Load(), Method: r.ticketMethod()}
	if !tickets.Verified {
		tickets.Detail = "The archived-ticket endpoint was refused, so archived tickets could not be counted. "
	}
	accounted := len(r.unchanged) + len(r.trashed) + len(r.spam)
	for _, ref := range r.refs {
		if !ref.archived {
			accounted++
		}
	}
	total, err := r.searchTotal(ctx, "/api/v2/search/tickets", allTicketsQuery)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		tickets.Verified, tickets.Detail = false, tickets.Detail+"Freshdesk search total unavailable: "+err.Error()
	case total > accounted:
		tickets.Verified = false
		tickets.Detail += fmt.Sprintf("Freshdesk search reports %d tickets outside the archive, but only %d were found.", total, accounted)
	default:
		tickets.Detail += fmt.Sprintf("Freshdesk search reports %d tickets outside the archive; %d were found.", total, accounted)
		if !tickets.Verified {
			tickets.Cause = CauseArchiveRefused
		}
	}
	r.verification.Tickets = tickets

	contacts := Verification{Verified: true, Method: "Freshdesk contact filter totals (active:true plus active:false) compared with the contact list."}
	active, errActive := r.searchTotal(ctx, "/api/v2/search/contacts", `"active:true"`)
	inactive, errInactive := r.searchTotal(ctx, "/api/v2/search/contacts", `"active:false"`)
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errActive != nil || errInactive != nil:
		contacts.Verified, contacts.Detail = false, "Freshdesk contact totals unavailable: "+errors.Join(errActive, errInactive).Error()
	case active+inactive > r.listedContacts:
		contacts.Verified = false
		contacts.Detail = fmt.Sprintf("Freshdesk reports %d contacts, but the contact list returned %d.", active+inactive, r.listedContacts)
	default:
		contacts.Detail = fmt.Sprintf("Freshdesk reports %d contacts; the contact list returned %d.", active+inactive, r.listedContacts)
	}
	r.verification.Contacts = contacts
	return nil
}

func (r *run) ticketMethod() string {
	method := fmt.Sprintf("Every ticket id from 1 to %d was looked up one by one (list, archive and ticket endpoints); probing went on to id %d and stopped after %d ids in a row that do not exist.",
		r.highestExisting, r.highestProbed, r.probeAhead)
	if r.opts.LastTicketID > 0 {
		method += fmt.Sprintf(" The probe covered at least --last-ticket-id %d.", r.opts.LastTicketID)
	}
	return method
}

func (r *run) searchTotal(ctx context.Context, path, query string) (int, error) {
	var response struct {
		Total *int `json:"total"`
	}
	if _, err := r.client.getJSON(ctx, r.endpoint(path, url.Values{"query": {query}}), &response); err != nil {
		return 0, err
	}
	if response.Total == nil {
		return 0, errors.New("response has no total")
	}
	return *response.Total, nil
}
