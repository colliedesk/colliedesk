package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

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
