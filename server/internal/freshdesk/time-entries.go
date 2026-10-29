package freshdesk

import (
	"context"
	"strconv"
)

// ticketTimeEntries returns the time entries of one ticket. The account's entries are listed once
// (GET /api/v2/time_entries, each entry names its ticket_id) and kept for the rest of the run, so
// a ticket costs no request of its own. When that list is refused, each ticket is asked for its own.
func (r *run) ticketTimeEntries(ctx context.Context, ticketID int64) ([]fdTimeEntry, error) {
	if !r.timeEntriesTried {
		r.timeEntriesTried = true
		listed := map[int64][]fdTimeEntry{}
		err := listAll(ctx, r.client, r.endpoint("/api/v2/time_entries", pageQuery()), func(e fdTimeEntry) error {
			listed[e.TicketID] = append(listed[e.TicketID], e)
			return nil
		})
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err == nil {
			r.timeEntriesByTicket = listed
		}
	}
	if r.timeEntriesByTicket != nil {
		return r.timeEntriesByTicket[ticketID], nil
	}
	var entries []fdTimeEntry
	err := listAll(ctx, r.client, r.endpoint("/api/v2/tickets/"+strconv.FormatInt(ticketID, 10)+"/time_entries", pageQuery()), func(e fdTimeEntry) error {
		entries = append(entries, e)
		return nil
	})
	return entries, err
}
