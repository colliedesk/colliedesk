package freshdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
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
	rows, err := r.pool.Query(ctx, `SELECT source_id,status,error,meta,source_updated_at FROM import_items
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
		ticket.archived = meta.Archived
		if id, err := strconv.ParseInt(key, 10, 64); err == nil {
			known[id] = ticket
		}
	}
	return known, rows.Err()
}

// probeAll looks up ids with a few requests in flight; results keep id order.
func (r *run) probeAll(ctx context.Context, ids []int64) []probeResult {
	results := make([]probeResult, len(ids))
	slots := make(chan struct{}, probeWorkers)
	var wg sync.WaitGroup
	for index, id := range ids {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			results[index] = r.probeOne(ctx, id)
		}()
	}
	wg.Wait()
	r.probed += len(ids)
	return results
}

func (r *run) probeOne(ctx context.Context, id int64) probeResult {
	key := strconv.FormatInt(id, 10)
	var ticket fdTicket
	if !r.archiveUnavailable.Load() {
		_, err := r.client.getJSON(ctx, r.endpoint("/api/v2/tickets/archived/"+key, nil), &ticket)
		switch {
		case err == nil:
			return probeResult{id: id, outcome: outcomeArchived, updatedAt: ticket.UpdatedAt.UTC()}
		case errors.Is(err, errForbidden):
			r.archiveUnavailable.Store(true)
		case !errors.Is(err, errNotFound):
			return probeResult{id: id, outcome: outcomeError, err: err}
		}
	}
	_, err := r.client.getJSON(ctx, r.endpoint("/api/v2/tickets/"+key, nil), &ticket)
	switch {
	case errors.Is(err, errNotFound):
		return probeResult{id: id, outcome: outcomeAbsent}
	case err != nil:
		return probeResult{id: id, outcome: outcomeError, err: err}
	case ticket.Spam:
		return probeResult{id: id, outcome: outcomeSpam}
	case ticket.Deleted:
		return probeResult{id: id, outcome: outcomeTrashed}
	default:
		return probeResult{id: id, outcome: outcomeLive, updatedAt: ticket.UpdatedAt.UTC()}
	}
}

// applyProbes records each verdict and returns the ids that do not exist.
func (r *run) applyProbes(ctx context.Context, results []probeResult) []int64 {
	var absent []int64
	for _, result := range results {
		key := strconv.FormatInt(result.id, 10)
		if result.outcome != outcomeAbsent {
			r.highestExisting = max(r.highestExisting, result.id)
		}
		switch result.outcome {
		case outcomeAbsent:
			absent = append(absent, result.id)
		case outcomeArchived:
			r.refs = append(r.refs, ticketRef{id: result.id, updatedAt: result.updatedAt, archived: true})
			r.archived++
		case outcomeLive:
			r.refs = append(r.refs, ticketRef{id: result.id, updatedAt: result.updatedAt})
			r.warn("ticket_not_listed", "Ticket exists in Freshdesk but the list endpoint did not return it; it was found by id and imported.", key)
		case outcomeTrashed, outcomeSpam:
			reason := map[probeOutcome]string{outcomeTrashed: skipTrashed, outcomeSpam: skipSpam}[result.outcome]
			r.recordSkip(reason, key)
			r.markSkip(ctx, key, reason)
		case outcomeError:
			r.unclassified = append(r.unclassified, key)
			if !r.opts.DryRun {
				r.itemFailed(ctx, entityTicket, key, "", fmt.Errorf("could not check whether ticket %s exists: %w", key, result.err))
			}
		}
	}
	return absent
}

// recordAbsent stores ids that do not exist below the highest existing id.
// Ids above it are not stored: Freshdesk may give them to new tickets later.
func (r *run) recordAbsent(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if id < r.highestExisting {
			key := strconv.FormatInt(id, 10)
			r.recordSkip(skipAbsent, key)
			r.markSkip(ctx, key, skipAbsent)
		}
	}
	return r.internalErr
}

func (r *run) markSkip(ctx context.Context, key, reason string) {
	if r.opts.DryRun {
		return
	}
	if err := r.ledger.mark(ctx, nil, ledgerMark{entity: entityTicket, sourceID: key, status: ledgerSkipped, errText: reason}); err != nil && r.internalErr == nil {
		r.internalErr = err
	}
}
