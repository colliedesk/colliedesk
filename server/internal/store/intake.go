package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/database"
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

// autoMaxAge is how old a batch may be for AI to decide it by itself once the card before it in its
// lane is decided.
const autoMaxAge = 2 * time.Hour

// unblockLane gives the next card of a lane that waited only for the one before it back to its decider,
// once no other card of the lane waits for a person.
func unblockLane(ctx context.Context, tx pgx.Tx, workspaceID, gatheringID string, now time.Time) error {
	tag, err := tx.Exec(ctx, `WITH lane AS (SELECT lane_key FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid),
		next AS (SELECT g.id FROM intake_gatherings g, lane WHERE g.workspace_id=$1 AND g.lane_key=lane.lane_key AND g.state='review'
			AND g.review_reason='lane_blocked' ORDER BY g.closed_at,g.id LIMIT 1)
		UPDATE intake_gatherings SET state='deciding',review_reason=NULL,decide_by=$3,updated_at=$4
		WHERE workspace_id=$1 AND id IN (SELECT id FROM next) AND closed_at>$5
			AND NOT EXISTS(SELECT 1 FROM intake_gatherings o, lane WHERE o.workspace_id=$1 AND o.lane_key=lane.lane_key AND o.state='review'
				AND o.review_reason<>'lane_blocked')`, workspaceID, gatheringID, now.Add(DecideWithin), now, now.Add(-autoMaxAge))
	if err != nil {
		return fmt.Errorf("unblock gathering lane: %w", err)
	}
	if tag.RowsAffected() > 0 {
		_, err = tx.Exec(ctx, `UPDATE channel_thread_items i SET disposition='gathering',disposition_reason='deciding' FROM intake_gatherings g
			WHERE i.workspace_id=$1 AND g.workspace_id=i.workspace_id AND g.id=i.gathering_id AND g.state='deciding'
			AND i.disposition='pending_review'`, workspaceID)
	}
	return err
}

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

// batchEvents are the journal events of the batch: those collected, or the one message.
func (j *journalWriter) batchEvents() []string {
	if len(j.request.EventIDs) == 0 {
		return []string{j.eventID}
	}
	return j.request.EventIDs
}

// recordGathering writes the gathering of the journal's events and points them and their items at it.
func recordGathering(ctx context.Context, tx pgx.Tx, journal *journalWriter, in channels.Inbound, g gathering) (string, error) {
	channel := journal.channel
	eventIDs := journal.batchEvents()
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
	basis := rules.Default
	if g.decision.Basis == rules.Match {
		basis = rules.Match
	}
	err = tx.QueryRow(ctx, `INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,contact_id,state,review_reason,rules_action,rules_rule,
			rules_target,conversation_id,decided_by,decided_at,opened_at,closed_at,decide_by,rules_basis)
		SELECT $1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,NULLIF($8,''),$9,$10,NULLIF($11,'')::uuid,NULLIF($12,'')::uuid,NULLIF($13,''),$14,
			LEAST(min(e.received_at),$15),$15,$16,$18
		FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.id::text=ANY($17)
		RETURNING id::text`, channel.WorkspaceID, channel.ID, in.Scope, lane, threadID, g.contactID, g.state, g.reviewReason, rulesAction(g.decision),
		g.decision.Rule, g.decision.ConversationID, g.conversationID, decidedBy, decidedAt, journal.now, g.decideBy, eventIDs, basis).Scan(&id)
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
		// Waiting for a person like the card before it.
		if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET disposition='pending_review',disposition_reason=$3
			WHERE workspace_id=$1 AND inbound_event_id::text=ANY($2)`, journal.channel.WorkspaceID, journal.batchEvents(), ReviewLaneBlocked); err != nil {
			return result, fmt.Errorf("queue gathering items: %w", err)
		}
	} else {
		due := journal.now.Add(DecideWithin)
		g.decideBy = &due
	}
	if _, err := recordGathering(ctx, tx, journal, in, g); err != nil {
		return result, err
	}
	return finish(result)
}

// Errors of deciding a gathering.
var (
	// ErrGatheringDecided means the gathering was decided meanwhile (by a person, AI or the rules), or
	// its items changed: the decision is not applied.
	ErrGatheringDecided = errors.New("this gathering was decided meanwhile")
	// ErrGatheringNotFound means no gathering of the workspace has that id.
	ErrGatheringNotFound = errors.New("no gathering with that id")
)

// isID says id is a uuid, as the queries of a gathering compare it (so its index serves them).
func isID(id string) bool { return itemID.MatchString(strings.ToLower(id)) }

// Deciders of a gathering, as intake_gatherings.decided_by names them.
const (
	DecidedByRules = "rules"
	DecidedByAI    = "ai"
	DecidedByAgent = "agent"
)

// IntakeApply is how a decision of a gathering is applied by IngestInbound: its held events, replayed
// as one message, open a conversation (ForceNew: never one a thread key found meanwhile) or join
// IntoConversation, while the gathering is still undecided and holds exactly those events.
type IntakeApply struct {
	GatheringID string
	// Decider is rules (the fallback of what they decided when it closed), ai or agent.
	Decider  string
	Fallback bool
	Actor    ChannelActor
	ForceNew bool
	// Command and Subject are a person's for the conversation a new request opens: the command's tags
	// and priority, and its subject.
	Command, Subject string
	// Decision is the row intake_decisions keeps of an AI's or a person's decision; nil for the rules.
	Decision *IntakeDecisionInput
}

// IntakeDecisionInput is what intake_decisions keeps of one decision.
type IntakeDecisionInput struct {
	Action, TargetConversationID, Command, Subject, ReplyKind, Reason string
	// IgnoreReason says why a person set a batch aside: chat, handled_in_chat, duplicate, other.
	IgnoreReason       string
	Confidence         *float64
	CorrectsDecisionID string
	// FollowsDecisionID is the AI suggestion a person decided as.
	FollowsDecisionID string
}

// pendingGathering is an undecided gathering, locked, with its events.
type pendingGathering struct {
	id, state, rulesAction, rulesRule string
	events                            []string
}

// lockGathering locks a gathering that is still to be decided and checks it holds exactly eventIDs.
func lockGathering(ctx context.Context, tx pgx.Tx, workspaceID, id string, eventIDs []string) (pendingGathering, error) {
	var g pendingGathering
	if !isID(id) {
		return g, ErrGatheringNotFound
	}
	err := tx.QueryRow(ctx, `SELECT g.id::text,g.state,g.rules_action,g.rules_rule,
			COALESCE((SELECT array_agg(e.id::text ORDER BY e.id) FROM channel_inbound_events e WHERE e.workspace_id=g.workspace_id AND e.gathering_id=g.id),'{}')
		FROM intake_gatherings g WHERE g.workspace_id=$1 AND g.id=$2::uuid FOR UPDATE OF g`, workspaceID, id).Scan(&g.id, &g.state, &g.rulesAction, &g.rulesRule, &g.events)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrGatheringNotFound
	}
	if err != nil {
		return g, fmt.Errorf("lock gathering: %w", err)
	}
	if g.state != GatheringDeciding && g.state != GatheringReview {
		return g, ErrGatheringDecided
	}
	if eventIDs != nil {
		seen := slices.Clone(eventIDs)
		slices.Sort(seen)
		if !slices.Equal(seen, g.events) {
			return g, ErrGatheringDecided
		}
	}
	return g, nil
}

// basisOf is what the journal says decided a message of a gathering: a person, AI, or the rules as they
// decided when the gathering was applied. The rules joining it to the conversation they found when it
// closed (a join-open scope's only open conversation) keep that rule.
func (a IntakeApply) basisOf(decision rules.Decision, g pendingGathering) rules.Decision {
	switch {
	case a.Decider == DecidedByAgent:
		decision.Basis, decision.Rule = rules.Person, "intake_"+decision.Action
	case a.Decider == DecidedByAI:
		decision.Basis, decision.Rule = rules.Model, "intake_"+decision.Action
	case decision.Rule == rules.IntoConversation:
		decision.Basis, decision.Rule = rules.Default, g.rulesRule
	}
	return decision
}

// ConversationStillOpen says a conversation of the workspace is open or pending.
func (s *Store) ConversationStillOpen(ctx context.Context, workspaceID, conversationID string) (bool, error) {
	if !isID(conversationID) {
		return false, nil
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		var open bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2::uuid AND status IN ('open','pending'))`,
			workspaceID, conversationID).Scan(&open)
		return open, err
	})
}

// concludeGathering records how a gathering was decided once its message is stored, with the contact of
// the customer who wrote it (found only now: a held batch creates none) and the decision intake_decisions
// keeps.
func concludeGathering(ctx context.Context, tx pgx.Tx, workspaceID string, apply IntakeApply, g pendingGathering, state, conversationID, contactID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE intake_gatherings SET state=$3,review_reason=NULL,decide_by=NULL,conversation_id=NULLIF($4,'')::uuid,decided_by=$5,decided_at=$6,
			decided_by_agent_id=NULLIF($7,'')::uuid,decided_by_token_id=NULLIF($8,'')::uuid,fallback=$9,contact_id=COALESCE(NULLIF($10,'')::uuid,contact_id),updated_at=$6
		WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, g.id, state, conversationID, apply.Decider, now, apply.Actor.AgentID, apply.Actor.TokenID, apply.Fallback,
		contactID); err != nil {
		return fmt.Errorf("conclude gathering: %w", err)
	}
	if err := unblockLane(ctx, tx, workspaceID, g.id, now); err != nil {
		return err
	}
	return recordIntakeDecision(ctx, tx, workspaceID, apply, g, now)
}

func recordIntakeDecision(ctx context.Context, tx pgx.Tx, workspaceID string, apply IntakeApply, g pendingGathering, now time.Time) error {
	d := apply.Decision
	if d == nil {
		return nil
	}
	basis := "person"
	if apply.Decider == DecidedByAI {
		basis = "model"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO intake_decisions(workspace_id,gathering_id,decider,basis,action,target_conversation_id,command,subject,reply_kind,confidence,reason,
			applied,items_seen,fallback,corrects_decision_id,agent_id,token_id,created_at,ignore_reason,follows_decision_id)
		VALUES($1,$2::uuid,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,NULLIF($11,''),true,$12::uuid[],$13,NULLIF($14,'')::uuid,
			NULLIF($15,'')::uuid,NULLIF($16,'')::uuid,$17,NULLIF($18,''),NULLIF($19,'')::uuid)`,
		workspaceID, g.id, apply.Decider, basis, d.Action, d.TargetConversationID, d.Command, clip(d.Subject, 200), d.ReplyKind, d.Confidence, clip(d.Reason, 2000),
		g.events, apply.Fallback, d.CorrectsDecisionID, apply.Actor.AgentID, apply.Actor.TokenID, now, d.IgnoreReason, d.FollowsDecisionID); err != nil {
		return fmt.Errorf("record intake decision: %w", err)
	}
	return nil
}

// SetGatheringState decides a gathering without a message: ignore keeps what it said held and out of
// every conversation (requeue takes it back), review puts it in the queue with why. A gathering
// already decided otherwise is ErrGatheringDecided.
func (s *Store) SetGatheringState(ctx context.Context, workspaceID, id string, action, reviewReason string, apply IntakeApply, now time.Time) error {
	if !isID(id) {
		return ErrGatheringNotFound
	}
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if action == rules.Review && reviewReason == ReviewRequeued {
			// Requeue: an ignored gathering goes back to the queue; the decision stays in its history.
			tag, err := tx.Exec(ctx, `UPDATE intake_gatherings SET state='review',review_reason='requeued',requeued_at=$3,requeued_by_agent_id=NULLIF($4,'')::uuid,
					decided_by=NULL,decided_at=NULL,decided_by_agent_id=NULL,decided_by_token_id=NULL,ignore_reason=NULL,updated_at=$3
				WHERE workspace_id=$1 AND id=$2::uuid AND state='ignored'`, workspaceID, id, now, apply.Actor.AgentID)
			if err != nil {
				return struct{}{}, fmt.Errorf("requeue gathering: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return struct{}{}, gatheringMissingOr(ctx, tx, workspaceID, id, ErrGatheringDecided)
			}
			_, err = tx.Exec(ctx, `UPDATE channel_thread_items SET disposition='pending_review',disposition_reason='requeued'
				WHERE workspace_id=$1 AND gathering_id=$2::uuid`, workspaceID, id)
			return struct{}{}, err
		}
		g, err := lockGathering(ctx, tx, workspaceID, id, nil)
		if err != nil {
			return struct{}{}, err
		}
		switch action {
		case rules.Ignore:
			ignoreReason := ""
			if apply.Decision != nil {
				ignoreReason = apply.Decision.IgnoreReason
			}
			if _, err := tx.Exec(ctx, `UPDATE intake_gatherings SET state='ignored',review_reason=NULL,decide_by=NULL,decided_by=$3,decided_at=$4,
					decided_by_agent_id=NULLIF($5,'')::uuid,decided_by_token_id=NULLIF($6,'')::uuid,fallback=$7,ignore_reason=NULLIF($8,''),updated_at=$4
				WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, id, apply.Decider, now, apply.Actor.AgentID, apply.Actor.TokenID, apply.Fallback, ignoreReason); err != nil {
				return struct{}{}, fmt.Errorf("ignore gathering: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET disposition='not_request',disposition_reason='ignored'
				WHERE workspace_id=$1 AND gathering_id=$2::uuid`, workspaceID, id); err != nil {
				return struct{}{}, fmt.Errorf("ignore gathering items: %w", err)
			}
			if err := unblockLane(ctx, tx, workspaceID, id, now); err != nil {
				return struct{}{}, err
			}
		case rules.Review:
			if _, err := tx.Exec(ctx, `UPDATE intake_gatherings SET state='review',review_reason=$3,decide_by=NULL,fallback=$4,updated_at=$5
				WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, id, reviewReason, apply.Fallback, now); err != nil {
				return struct{}{}, fmt.Errorf("queue gathering: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET disposition='pending_review',disposition_reason=$3
				WHERE workspace_id=$1 AND gathering_id=$2::uuid`, workspaceID, id, reviewReason); err != nil {
				return struct{}{}, fmt.Errorf("queue gathering items: %w", err)
			}
		default:
			return struct{}{}, fmt.Errorf("a gathering is not decided %q without a message", action)
		}
		return struct{}{}, recordIntakeDecision(ctx, tx, workspaceID, apply, g, now)
	})
	return err
}

// gatheringMissingOr tells a gathering that is not there from one in another state.
func gatheringMissingOr(ctx context.Context, tx pgx.Tx, workspaceID, id string, otherwise error) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid)`, workspaceID, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrGatheringNotFound
	}
	return otherwise
}

// DueGathering is a gathering past its time to be decided, with what the rules decided when it closed.
type DueGathering struct {
	WorkspaceID, ID, RulesAction, RulesTarget string
}

// DueGatherings lists, across workspaces, gatherings AI did not decide in time, oldest first.
func (s *Store) DueGatherings(ctx context.Context, now time.Time, limit int) ([]DueGathering, error) {
	var due []DueGathering
	err := s.db.Privileged(ctx, database.PrivilegeIntakeDueClaim, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text,id::text,rules_action,COALESCE(rules_target::text,'') FROM intake_gatherings
			WHERE state='deciding' AND decide_by<=$1 ORDER BY decide_by LIMIT $2`, now, limit)
		if err != nil {
			return err
		}
		due, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (DueGathering, error) {
			var g DueGathering
			return g, row.Scan(&g.WorkspaceID, &g.ID, &g.RulesAction, &g.RulesTarget)
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list due gatherings: %w", err)
	}
	return due, nil
}

// GatheringOutcome is how a gathering stands: its state, the conversation it opened or joined, and who
// decided it (the person, for an agent).
type GatheringOutcome struct {
	State, ConversationID, DecidedBy, DecidedByAgentID string
}

// GatheringOutcomeOf reads how a gathering stands.
func (s *Store) GatheringOutcomeOf(ctx context.Context, workspaceID, id string) (GatheringOutcome, error) {
	if !isID(id) {
		return GatheringOutcome{}, ErrGatheringNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (GatheringOutcome, error) {
		var o GatheringOutcome
		err := tx.QueryRow(ctx, `SELECT state,COALESCE(conversation_id::text,''),COALESCE(decided_by,''),COALESCE(decided_by_agent_id::text,'')
			FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, id).Scan(&o.State, &o.ConversationID, &o.DecidedBy, &o.DecidedByAgentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return o, ErrGatheringNotFound
		}
		return o, err
	})
}
