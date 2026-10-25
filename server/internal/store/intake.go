package store

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/intake/rules"
	"github.com/jackc/pgx/v5"
)

// Intake of chat channels (docs/internals/channels.md "Intake"): what a customer said in one go on a
// scope the type declares for intake is a gathering, written in the transaction that decides it. Email
// and web forms declare none, so they never have one.

// Gathering states and why one waits in the queue.
const (
	GatheringDeciding  = "deciding"
	GatheringReview    = "review"
	GatheringCreated   = "created"
	GatheringAttached  = "attached"
	GatheringIgnored   = "ignored"
	GatheringApplied   = "applied"
	GatheringCancelled = "cancelled"

	ReviewNoCommand     = "no_command"
	ReviewAIUnavailable = "ai_unavailable"
	ReviewLaneBlocked   = "lane_blocked"
	ReviewRequeued      = "requeued"
	ReviewTargetChanged = "target_changed"
)

// DecideWithin is how long a gathering waits for AI before the rules' decision applies.
const DecideWithin = 2 * time.Minute

// Intake levels a scope's intake is at (ai_level).
const (
	intakeOff     = "off"
	intakeSuggest = "suggest"
	intakeAuto    = "auto"
)

// intakeLevel is the level of intake for a message, "" when it is no batch of intake: a scope the
// type does not declare for intake, a person's replay, a team member's message.
func intakeLevel(ctx context.Context, tx pgx.Tx, channel channels.Channel, definition channels.Definition, in channels.Inbound, request IngestRequest) (string, error) {
	if !slices.Contains(definition.IntakeScopes, in.Scope) || request.Replay || request.IntoConversation != "" || in.Role == channels.RoleStaff {
		return "", nil
	}
	var level string
	if err := tx.QueryRow(ctx, `SELECT ai_level($1,$2,$3,'intake')`, channel.WorkspaceID, channel.ID, in.Scope).Scan(&level); err != nil {
		return "", fmt.Errorf("read intake level: %w", err)
	}
	return level, nil
}

// gathering is what is written of a batch of intake when it is decided or held.
type gathering struct {
	state, reviewReason string
	decision            rules.Decision
	conversationID      string
	contactID           string
	decidedByRules      bool
	decideBy            *time.Time
}

// rulesAction is what the rules decided, as a gathering keeps it for its fallback.
func rulesAction(decision rules.Decision) string {
	if decision.Action == rules.Ignore {
		return rules.Archive
	}
	return decision.Action
}

// laneKey is where a sender's gatherings are decided one after the other: the place and the sender.
func laneKey(ctx context.Context, tx pgx.Tx, channel channels.Channel, in channels.Inbound) (string, string, error) {
	sender := in.Sender.Identity.Key()
	if in.Place == nil {
		return clip("sender:"+channel.ID+":"+sender, 500), "", nil
	}
	var threadID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM channel_threads WHERE workspace_id=$1 AND channel_id=$2 AND external_id=$3`,
		channel.WorkspaceID, channel.ID, in.Place.ExternalID).Scan(&threadID)
	if err != nil {
		return "", "", fmt.Errorf("find chat thread of gathering: %w", err)
	}
	return clip("thread:"+threadID+":"+sender, 500), threadID, nil
}

// recordGathering writes the gathering of the journal's events and points them and their items at it.
func recordGathering(ctx context.Context, tx pgx.Tx, journal *journalWriter, in channels.Inbound, g gathering) (string, error) {
	channel := journal.channel
	eventIDs := journal.request.EventIDs
	if len(eventIDs) == 0 {
		eventIDs = []string{journal.eventID}
	}
	lane, threadID, err := laneKey(ctx, tx, channel, in)
	if err != nil {
		return "", err
	}
	decidedBy := ""
	var decidedAt *time.Time
	if g.decidedByRules {
		decidedBy, decidedAt = "rules", &journal.now
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,contact_id,state,review_reason,rules_action,rules_rule,
			rules_target,conversation_id,decided_by,decided_at,opened_at,closed_at,decide_by)
		SELECT $1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,NULLIF($8,''),$9,$10,NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,NULLIF($13,''),$14,
			LEAST(min(e.received_at),$15),$15,$16
		FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.id::text=ANY($17)
		RETURNING id::text`, channel.WorkspaceID, channel.ID, in.Scope, lane, threadID, g.contactID, g.state, g.reviewReason, rulesAction(g.decision),
		g.decision.Rule, g.decision.ConversationID, g.conversationID, decidedBy, decidedAt, journal.now, g.decideBy, eventIDs).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("record gathering: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE channel_inbound_events SET gathering_id=$3 WHERE workspace_id=$1 AND id::text=ANY($2)`, channel.WorkspaceID, eventIDs, id); err != nil {
		return "", fmt.Errorf("point events at gathering: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET gathering_id=$3 WHERE workspace_id=$1 AND inbound_event_id::text=ANY($2)`, channel.WorkspaceID, eventIDs, id); err != nil {
		return "", fmt.Errorf("point items at gathering: %w", err)
	}
	return id, nil
}

// holdForIntake keeps a batch the rules decided only by default for intake to decide: held like any
// held message (its original stored and counted), with what the rules would do, until DecideWithin.
// Behind a card of the same lane that waits for a person it waits in the queue too.
func (s *Store) holdForIntake(ctx context.Context, tx pgx.Tx, journal *journalWriter, in channels.Inbound, decision rules.Decision,
	finish func(channels.IngestResult) (channels.IngestResult, error)) (channels.IngestResult, error) {
	var result channels.IngestResult
	held, err := s.holdInbound(ctx, tx, journal, in, channels.HoldIntake, decision, func(r channels.IngestResult) (channels.IngestResult, error) {
		result = r
		return r, nil
	})
	if err != nil {
		return held, err
	}
	lane, _, err := laneKey(ctx, tx, journal.channel, in)
	if err != nil {
		return result, err
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM intake_gatherings WHERE workspace_id=$1 AND lane_key=$2 AND state='review')`,
		journal.channel.WorkspaceID, lane).Scan(&blocked); err != nil {
		return result, fmt.Errorf("read gathering lane: %w", err)
	}
	g := gathering{state: GatheringDeciding, decision: decision}
	if blocked {
		g.state, g.reviewReason = GatheringReview, ReviewLaneBlocked
	} else {
		due := journal.now.Add(DecideWithin)
		g.decideBy = &due
	}
	if _, err := recordGathering(ctx, tx, journal, in, g); err != nil {
		return result, err
	}
	return finish(result)
}
