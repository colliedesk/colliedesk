package freshdesk

import (
	"context"
	"fmt"
	"time"
)

// listEpoch is the default updated_since. Without updated_since the Freshdesk
// list endpoint returns only tickets created in the last 30 days.
var listEpoch = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)

// listTickets lists tickets oldest-updated first. The list endpoint stops after
// MaxTicketPages pages, so when the cap is hit the next window restarts at the
// last updated_at seen; ids already seen are skipped.
func (r *run) listTickets(ctx context.Context) error {
	since := listEpoch
	if r.opts.Since != nil {
		since = r.opts.Since.UTC()
	}
	seen := map[int64]bool{}
	for {
		query := pageQuery()
		query.Set("updated_since", since.Format(time.RFC3339))
		query.Set("order_by", "updated_at")
		query.Set("order_type", "asc")
		// Ticket stats (resolved_at, first_responded_at, closed_at) for the results baseline; two more
		// credits per page of 100.
		query.Set("include", "stats")
		target, pages, last := r.endpoint("/api/v2/tickets", query), 0, since
		for target != "" && pages < r.opts.MaxTicketPages {
			var page []fdTicket
			next, err := r.client.getJSON(ctx, target, &page)
			if err != nil {
				return fmt.Errorf("list tickets: %w", err)
			}
			pages++
			for _, ticket := range page {
				if !seen[ticket.ID] {
					seen[ticket.ID] = true
					r.refs = append(r.refs, ticketRef{id: ticket.ID, updatedAt: ticket.UpdatedAt.UTC(), stats: ticket.Stats, status: ticket.Status})
				}
				if ticket.UpdatedAt.After(last) {
					last = ticket.UpdatedAt.UTC()
				}
			}
			target = next
		}
		if target == "" {
			return nil
		}
		if !last.After(since) {
			return fmt.Errorf("list tickets: more than %d pages of tickets share updated_at %s; the list API cannot page past them", r.opts.MaxTicketPages, since.Format(time.RFC3339))
		}
		since = last
	}
}

func (r *run) recordSkip(reason, key string) {
	switch reason {
	case skipDeleted:
		r.deleted = append(r.deleted, key)
	case skipTrashed:
		r.trashed = append(r.trashed, key)
	case skipSpam:
		r.spam = append(r.spam, key)
	default:
		r.absent++
	}
}
