package freshdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const (
	defaultProbeAhead = 200
	probeWorkers      = 4
)

type probeOutcome int

const (
	outcomeAbsent probeOutcome = iota
	outcomeArchived
	outcomeLive
	outcomeTrashed
	outcomeSpam
	outcomeError
)

type probeResult struct {
	id        int64
	outcome   probeOutcome
	updatedAt time.Time
	err       error
}

type knownTicket struct {
	status, reason  string
	archived        bool
	archiveChecked  bool
	sourceUpdatedAt *time.Time
}

// classifyTickets gives every ticket id from 1 upward a verdict: listed,
// archived, trashed, spam, absent, or already known from an earlier run. The
// list endpoint leaves out archived, trashed and spam tickets, so this per-id
// walk, not the list, establishes how many tickets the account holds. Probing
// continues past the highest known id until probeAhead ids in a row do not exist.
func (r *run) classifyTickets(ctx context.Context) error {
	listed := map[int64]bool{}
	bound := r.opts.LastTicketID
	for _, ref := range r.refs {
		listed[ref.id] = true
		bound = max(bound, ref.id)
		r.highestExisting = max(r.highestExisting, ref.id)
	}
	known, err := r.loadTicketLedger(ctx)
	if err != nil {
		return err
	}
	for id, ticket := range known {
		if ticket.reason != skipAbsent {
			bound = max(bound, id)
		}
	}
	var todo []int64
	for id := int64(1); id <= bound; id++ {
		if !listed[id] && !r.fromLedger(id, known[id]) {
			todo = append(todo, id)
		}
	}
	results := r.probeAll(ctx, todo)
	if err := ctx.Err(); err != nil {
		return err
	}
	absent := r.applyProbes(ctx, results)
	next, run := bound+1, 0
	for run < r.probeAhead {
		batch := make([]int64, 0, probeWorkers)
		for len(batch) < min(probeWorkers, r.probeAhead-run) {
			batch = append(batch, next)
			next++
		}
		results := r.probeAll(ctx, batch)
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, result := range results {
			if result.outcome == outcomeAbsent {
				run++
			} else {
				run = 0
			}
		}
		absent = append(absent, r.applyProbes(ctx, results)...)
	}
	r.highestProbed = next - 1
	if r.archiveUnavailable.Load() {
		r.warn("archive_unavailable", "Freshdesk refused the archived-ticket endpoint (HTTP 403), so archived tickets could not be checked or imported.", "")
	}
	return r.recordAbsent(ctx, absent)
}

// fromLedger reuses an earlier run's verdict for an id the list did not return.
func (r *run) fromLedger(id int64, ticket knownTicket) bool {
	key := strconv.FormatInt(id, 10)
	switch {
	case ticket.status == ledgerSkipped && ticket.reason == skipAbsent && !ticket.archiveChecked:
		// The archive was not confirmed when this id was found missing: ask again.
		return false
	case ticket.status == ledgerSkipped && ticket.reason != skipDeleted:
		r.recordSkip(ticket.reason, key)
		if ticket.reason != skipAbsent {
			r.highestExisting = max(r.highestExisting, id)
		}
		return true
	case ticket.archived && ticket.sourceUpdatedAt != nil && ticket.status != ledgerSkipped:
		r.refs = append(r.refs, ticketRef{id: id, updatedAt: *ticket.sourceUpdatedAt, archived: true})
		r.archived++
		r.highestExisting = max(r.highestExisting, id)
		return true
	case ticket.status == ledgerDone && r.opts.Since != nil:
		// Imported before and not updated since --since: still part of the account.
		r.unchanged = append(r.unchanged, key)
		r.highestExisting = max(r.highestExisting, id)
		return true
	}
	return false
}

func (r *run) loadTicketLedger(ctx context.Context) (map[int64]knownTicket, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("read import ledger: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read only
	rows, err := tx.Query(ctx, `SELECT source_id,status,error,meta,source_updated_at FROM import_items
		WHERE workspace_id=$1 AND source=$2 AND entity='ticket'`, r.workspaceID, r.ledger.source)
	if err != nil {
		return nil, fmt.Errorf("read import ledger: %w", err)
	}
	defer rows.Close()
	known := map[int64]knownTicket{}
	for rows.Next() {
		var key string
		var raw []byte
		var ticket knownTicket
		var meta ticketMeta
		if err := rows.Scan(&key, &ticket.status, &ticket.reason, &raw, &ticket.sourceUpdatedAt); err != nil {
			return nil, fmt.Errorf("read import ledger: %w", err)
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("read import ledger meta: %w", err)
		}
		ticket.archived, ticket.archiveChecked = meta.Archived, meta.ArchiveChecked
		if id, err := strconv.ParseInt(key, 10, 64); err == nil {
			known[id] = ticket
		}
	}
	return known, rows.Err()
}
