package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The clocks of a conversation (docs/user/response-targets.md) are stored on it: the database applies what
// needs no business time (conversations_sla_change, messages_sla_reply) and marks it stale; refreshSLA
// counts a stale one again with the workspace's policy (domain.SLAPolicy, the one place business time is
// counted). A conversation read stale is counted before it is shown; the worker counts the rest.

func init() {
	targetsChanged = func(ctx context.Context, tx pgx.Tx, workspaceID string) error {
		// The targets changed: every clock that may still run is counted again (by the worker, in batches).
		_, err := tx.Exec(ctx, `UPDATE conversations SET sla_stale=true WHERE workspace_id=$1 AND sla_applies AND NOT sla_stale
			AND status IN ('open','pending')`, workspaceID)
		if err != nil {
			return fmt.Errorf("count the clocks again: %w", err)
		}
		return nil
	}
}

// refreshSLA counts again the clocks of the stale conversations among ids.
func refreshSLA(ctx context.Context, tx pgx.Tx, workspaceID string, policy domain.SLAPolicy, ids []string) error {
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.created_at,c.priority,c.status,c.sla_first_met_at,
			(SELECT h.from_status FROM conversation_status_changes h WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id ORDER BY h.changed_at,h.id LIMIT 1),
			COALESCE((SELECT json_agg(json_build_object('at',h.changed_at,'status',h.to_status) ORDER BY h.changed_at,h.id) FROM conversation_status_changes h
				WHERE h.workspace_id=c.workspace_id AND h.conversation_id=c.id),'[]')
		FROM conversations c WHERE c.workspace_id=$1 AND c.id=ANY($2::uuid[]) AND c.sla_applies AND c.sla_stale FOR UPDATE OF c`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read the facts of the clocks: %w", err)
	}
	type stale struct {
		id    string
		facts domain.SLAFacts
	}
	var list []stale
	for rows.Next() {
		var item stale
		var status string
		var opened *string
		var history []byte
		if err := rows.Scan(&item.id, &item.facts.CreatedAt, &item.facts.Priority, &status, &item.facts.FirstReplyAt, &opened, &history); err != nil {
			rows.Close()
			return fmt.Errorf("read the facts of the clocks: %w", err)
		}
		var changes []struct {
			At     time.Time `json:"at"`
			Status string    `json:"status"`
		}
		if err := json.Unmarshal(history, &changes); err != nil {
			rows.Close()
			return fmt.Errorf("read the status history: %w", err)
		}
		first := status
		if opened != nil {
			first = *opened
		}
		item.facts.Changes = []domain.StatusAt{{At: item.facts.CreatedAt, Status: first}}
		for _, change := range changes {
			item.facts.Changes = append(item.facts.Changes, domain.StatusAt{At: change.At, Status: change.Status})
		}
		list = append(list, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range list {
		first, resolution := policy.Evaluate(item.facts)
		state := func(clock *domain.SLAClock) *string {
			if clock == nil {
				return nil
			}
			return &clock.State
		}
		at := func(clock *domain.SLAClock, pick func(*domain.SLAClock) *time.Time) *time.Time {
			if clock == nil {
				return nil
			}
			return pick(clock)
		}
		due := func(c *domain.SLAClock) *time.Time { return c.DueAt }
		warn := func(c *domain.SLAClock) *time.Time { return c.WarnAt }
		met := func(c *domain.SLAClock) *time.Time { return c.MetAt }
		var earliestDue, earliestWarn *time.Time
		for _, clock := range []*domain.SLAClock{first, resolution} {
			if clock == nil || clock.State != domain.ClockRunning || clock.DueAt == nil {
				continue
			}
			if earliestDue == nil || clock.DueAt.Before(*earliestDue) {
				earliestDue = clock.DueAt
			}
			if earliestWarn == nil || clock.WarnAt.Before(*earliestWarn) {
				earliestWarn = clock.WarnAt
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET sla_stale=false,sla_first_state=$3,sla_first_due_at=$4,sla_first_warn_at=$5,sla_first_met_at=COALESCE($6,sla_first_met_at),
				sla_resolution_state=$7,sla_resolution_due_at=$8,sla_resolution_warn_at=$9,sla_due_at=$10,sla_warn_at=$11 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, item.id, state(first), at(first, due), at(first, warn), at(first, met), state(resolution), at(resolution, due), at(resolution, warn),
			earliestDue, earliestWarn); err != nil {
			return fmt.Errorf("save the clocks: %w", err)
		}
	}
	return nil
}

// attachSLA gives the conversations read in tx their clocks, counting a stale one first; none while the
// workspace's targets are off.
func (s *Store) attachSLA(ctx context.Context, tx pgx.Tx, workspaceID string, items []domain.Conversation) error {
	if len(items) == 0 {
		return nil
	}
	_, policy, on, err := workspacePolicy(ctx, tx, workspaceID)
	if err != nil || !on {
		return err
	}
	ids := make([]string, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	if err := refreshSLA(ctx, tx, workspaceID, policy, ids); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,sla_first_state,sla_first_due_at,sla_first_warn_at,sla_first_met_at,sla_resolution_state,sla_resolution_due_at,sla_resolution_warn_at
		FROM conversations WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND sla_applies`, workspaceID, ids)
	if err != nil {
		return fmt.Errorf("read the clocks: %w", err)
	}
	defer rows.Close()
	now := s.now()
	clocks := map[string]*domain.ConversationSLA{}
	for rows.Next() {
		var id string
		var firstState, resolutionState *string
		var firstDue, firstWarn, firstMet, resolutionDue, resolutionWarn *time.Time
		if err := rows.Scan(&id, &firstState, &firstDue, &firstWarn, &firstMet, &resolutionState, &resolutionDue, &resolutionWarn); err != nil {
			return fmt.Errorf("read the clocks: %w", err)
		}
		sla := &domain.ConversationSLA{
			FirstResponse: clockView(policy, firstState, firstDue, firstWarn, firstMet, now),
			Resolution:    clockView(policy, resolutionState, resolutionDue, resolutionWarn, nil, now),
		}
		if sla.FirstResponse == nil && sla.Resolution == nil {
			continue
		}
		sla.State = overallState(sla)
		clocks[id] = sla
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range items {
		items[index].SLA = clocks[items[index].ID]
	}
	return nil
}

func clockView(policy domain.SLAPolicy, state *string, due, warn, met *time.Time, now time.Time) *domain.SLAClockView {
	if state == nil {
		return nil
	}
	view := &domain.SLAClockView{State: *state, DueAt: domain.UTCOptional(due), WarnAt: domain.UTCOptional(warn), MetAt: domain.UTCOptional(met)}
	if view.State == domain.ClockRunning && due != nil {
		view.Overdue = !now.Before(*due)
		view.DueSoon = !view.Overdue && warn != nil && !now.Before(*warn)
		if view.Overdue {
			view.BreachedAt = view.DueAt
		}
		view.BusinessMinutesLeft = policy.MinutesLeft(&domain.SLAClock{State: view.State, DueAt: due}, now)
	}
	return view
}

// overallState is the most pressing of the clocks: overdue, due soon, running (ok), paused, stopped, met.
func overallState(sla *domain.ConversationSLA) string {
	clocks := []*domain.SLAClockView{sla.FirstResponse, sla.Resolution}
	for _, test := range []struct {
		state string
		is    func(*domain.SLAClockView) bool
	}{
		{"overdue", func(c *domain.SLAClockView) bool { return c.Overdue }},
		{"due_soon", func(c *domain.SLAClockView) bool { return c.DueSoon }},
		{"ok", func(c *domain.SLAClockView) bool { return c.State == domain.ClockRunning }},
		{"paused", func(c *domain.SLAClockView) bool { return c.State == domain.ClockPaused }},
		{"stopped", func(c *domain.SLAClockView) bool { return c.State == domain.ClockStopped }},
	} {
		for _, clock := range clocks {
			if clock != nil && test.is(clock) {
				return test.state
			}
		}
	}
	return "met"
}

// RefreshStaleSLA counts again, in every workspace, up to limit conversations whose clocks are stale, and
// returns how many it counted.
func (s *Store) RefreshStaleSLA(ctx context.Context, limit int) (int, error) {
	type batch struct {
		workspaceID string
		ids         []string
	}
	var batches []batch
	err := s.db.Privileged(ctx, database.PrivilegeSLAClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,array_agg(id::text) FROM (SELECT workspace_id,id FROM conversations WHERE sla_stale LIMIT $1) s
			GROUP BY workspace_id`, limit)
		if err != nil {
			return err
		}
		batches, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (batch, error) {
			var b batch
			err := row.Scan(&b.workspaceID, &b.ids)
			return b, err
		})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("list stale clocks: %w", err)
	}
	counted := 0
	for _, b := range batches {
		if _, err := scoped(ctx, s, b.workspaceID, func(tx pgx.Tx) (struct{}, error) {
			_, policy, on, err := workspacePolicy(ctx, tx, b.workspaceID)
			if err != nil {
				return struct{}{}, err
			}
			if !on {
				// Off: nothing shows the clocks; they are counted again once the targets are on.
				_, err := tx.Exec(ctx, `UPDATE conversations SET sla_stale=false WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, b.workspaceID, b.ids)
				return struct{}{}, err
			}
			return struct{}{}, refreshSLA(ctx, tx, b.workspaceID, policy, b.ids)
		}); err != nil {
			return counted, err
		}
		counted += len(b.ids)
	}
	return counted, nil
}
