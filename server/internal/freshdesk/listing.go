package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
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
					r.refs = append(r.refs, ticketRef{id: ticket.ID, updatedAt: ticket.UpdatedAt.UTC()})
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

// probeGaps checks every ticket id between 1 and the highest listed id that the
// list did not return. Freshdesk leaves archived, trashed and spam tickets out
// of the list; archived ones are only reachable by id.
func (r *run) probeGaps(ctx context.Context) error {
	listed := map[int64]bool{}
	var maxID int64
	for _, ref := range r.refs {
		listed[ref.id] = true
		maxID = max(maxID, ref.id)
	}
	for id := int64(1); id < maxID; id++ {
		if listed[id] {
			continue
		}
		key := strconv.FormatInt(id, 10)
		entry, found, err := r.ledger.get(ctx, entityTicket, key)
		if err != nil {
			return err
		}
		if found && entry.Status == ledgerSkipped && entry.Error != skipDeleted {
			r.recordSkip(entry.Error, key)
			continue
		}
		if found && entry.Meta.Archived && entry.SourceUpdatedAt != nil {
			r.refs = append(r.refs, ticketRef{id: id, updatedAt: *entry.SourceUpdatedAt, archived: true})
			r.archived++
			continue
		}
		if err := r.probeTicket(ctx, id, key); err != nil {
			return err
		}
	}
	return nil
}

func (r *run) probeTicket(ctx context.Context, id int64, key string) error {
	var ticket fdTicket
	_, err := r.client.getJSON(ctx, r.endpoint("/api/v2/tickets/archived/"+key, nil), &ticket)
	if err == nil {
		r.refs = append(r.refs, ticketRef{id: id, updatedAt: ticket.UpdatedAt.UTC(), archived: true})
		r.archived++
		return nil
	}
	if !errors.Is(err, errNotFound) {
		return fmt.Errorf("probe archived ticket %s: %w", key, err)
	}
	reason := skipAbsent
	_, err = r.client.getJSON(ctx, r.endpoint("/api/v2/tickets/"+key, url.Values{}), &ticket)
	switch {
	case err == nil && ticket.Spam:
		reason = skipSpam
	case err == nil && ticket.Deleted:
		reason = skipTrashed
	case err == nil:
		// Created after the list was read: import it like a listed ticket.
		r.refs = append(r.refs, ticketRef{id: id, updatedAt: ticket.UpdatedAt.UTC()})
		return nil
	case !errors.Is(err, errNotFound):
		return fmt.Errorf("probe ticket %s: %w", key, err)
	}
	r.recordSkip(reason, key)
	if r.opts.DryRun {
		return nil
	}
	return r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: key, status: ledgerSkipped, errText: reason})
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
