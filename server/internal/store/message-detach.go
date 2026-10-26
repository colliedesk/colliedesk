package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Taking chat messages back to the intake queue (docs/internals/message-moves.md "Back to the queue"):
// the messages leave the conversation's timeline (detached_at) without being deleted, and their chat items
// become a card of the queue, decided again like any other: a person adds it to a conversation, opens one
// with it, or sets it aside. Email and web forms have no queue.

// RuleDetached is the rule of a card a person made by taking messages back to the queue.
const RuleDetached = "detached"

// detachMessages takes chat messages of conversation fromID back to the intake queue as one card: they
// came in one chat place from one customer, through a scope with an intake queue.
func (s *Store) detachMessages(ctx context.Context, workspaceID, fromID string, ids []string, actor Actor, now time.Time) (MoveResult, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MoveResult, error) {
		locked, err := lockMoveConversations(ctx, tx, workspaceID, fromID)
		if err != nil {
			return MoveResult{}, err
		}
		if _, ok := locked[fromID]; !ok {
			return MoveResult{}, pgx.ErrNoRows
		}
		if _, err := lockMovedMessages(ctx, tx, workspaceID, fromID, ids); err != nil {
			return MoveResult{}, err
		}
		// Where the messages came from: every one through a gathering of a chat's intake scope, all in one
		// place from one customer (a card is one lane).
		var withQueue, places int
		if err := tx.QueryRow(ctx, `SELECT count(DISTINCT m.id) FILTER (WHERE g.id IS NOT NULL),count(DISTINCT (g.channel_id,g.thread_id,e.sender_key))
			FROM messages m LEFT JOIN channel_inbound_events e ON e.workspace_id=m.workspace_id AND e.message_id=m.id
			LEFT JOIN intake_gatherings g ON g.workspace_id=e.workspace_id AND g.id=e.gathering_id
			WHERE m.workspace_id=$1 AND m.id=ANY($2::uuid[])`, workspaceID, ids).Scan(&withQueue, &places); err != nil {
			return MoveResult{}, fmt.Errorf("find where messages came from: %w", err)
		}
		if withQueue != len(ids) {
			return MoveResult{}, ErrNoIntakeQueue
		}
		if places != 1 {
			return MoveResult{}, &MoveInvalidError{"Take back the messages of one customer in one chat place at a time."}
		}
		move := messageMoveRow{fromID: fromID, ids: ids, actor: actor, now: now}
		// What AI applied is corrected on its own card, before the items join the new one.
		if move.decisionID, err = recordMoveCorrectionsAs(ctx, tx, workspaceID, ids, "", "review", actor, now); err != nil {
			return MoveResult{}, err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,contact_id,state,review_reason,rules_action,
				rules_basis,rules_rule,opened_at,closed_at)
			SELECT g.workspace_id,g.channel_id,g.scope,'detached:'||$3,g.thread_id,g.contact_id,'review','requeued','review','default',$4,
				(SELECT min(e.received_at) FROM channel_inbound_events e WHERE e.workspace_id=$1 AND e.message_id=ANY($2::uuid[])),$5
			FROM channel_inbound_events e JOIN intake_gatherings g ON g.workspace_id=e.workspace_id AND g.id=e.gathering_id
			WHERE e.workspace_id=$1 AND e.message_id=ANY($2::uuid[]) ORDER BY g.closed_at DESC LIMIT 1
			RETURNING id::text`, workspaceID, ids, ids[0], RuleDetached, now).Scan(&move.gatheringID); err != nil {
			return MoveResult{}, fmt.Errorf("open the card of taken back messages: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET gathering_id=$3,disposition='pending_review',disposition_reason=$4
			WHERE workspace_id=$1 AND message_id=ANY($2::uuid[])`, workspaceID, ids, move.gatheringID, RuleDetached); err != nil {
			return MoveResult{}, fmt.Errorf("queue taken back chat items: %w", err)
		}
		if err := relinkItems(ctx, tx, workspaceID, ids, fromID, "", "", actor.AgentID, now); err != nil {
			return MoveResult{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE messages SET detached_at=$3,detached_by_agent_id=NULLIF($4,'')::uuid WHERE workspace_id=$1 AND id=ANY($2::uuid[])`,
			workspaceID, ids, now, actor.AgentID); err != nil {
			return MoveResult{}, fmt.Errorf("take messages out of the conversation: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET updated_at=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, fromID, now); err != nil {
			return MoveResult{}, fmt.Errorf("touch conversation: %w", err)
		}
		recorded, err := move.insert(ctx, tx, workspaceID)
		if err != nil {
			return MoveResult{}, err
		}
		recorded.Undoable = true
		return MoveResult{Move: recorded, Warnings: []string{}}, nil
	})
}

// undoDetach puts messages taken back to the queue in their conversation again, while their card still
// waits: the card is cancelled and its items return to where they came from.
func undoDetach(ctx context.Context, tx pgx.Tx, workspaceID, fromID, gatheringID string, ids []string, actor Actor, now time.Time) error {
	if gatheringID == "" {
		return ErrMoveDecided
	}
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, workspaceID, gatheringID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && state != GatheringReview) {
		return ErrMoveDecided
	}
	if err != nil {
		return fmt.Errorf("lock the card of taken back messages: %w", err)
	}
	if _, err := lockMoveConversations(ctx, tx, workspaceID, fromID); err != nil {
		return err
	}
	if err := attachDetached(ctx, tx, workspaceID, ids, fromID, actor, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE intake_gatherings SET state='cancelled',review_reason=NULL,decided_by='agent',decided_at=$3,
			decided_by_agent_id=NULLIF($4,'')::uuid,decided_by_token_id=NULLIF($5,'')::uuid,updated_at=$3 WHERE workspace_id=$1 AND id=$2::uuid`,
		workspaceID, gatheringID, now, actor.AgentID, actor.TokenID); err != nil {
		return fmt.Errorf("cancel the card of taken back messages: %w", err)
	}
	// The items go back to the gathering they came in, as the journal still names it.
	if _, err := tx.Exec(ctx, `UPDATE channel_thread_items i SET gathering_id=e.gathering_id FROM channel_inbound_events e
		WHERE i.workspace_id=$1 AND i.message_id=ANY($2::uuid[]) AND e.workspace_id=i.workspace_id AND e.id=i.inbound_event_id`, workspaceID, ids); err != nil {
		return fmt.Errorf("give items their gathering back: %w", err)
	}
	return nil
}

// attachDetached puts messages taken back to the queue in conversation toID: back in its timeline, their
// writer kept when it is not toID's contact, their items linked there, and toID reopened when it was
// resolved or closed.
func attachDetached(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string, toID string, actor Actor, now time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE messages m SET detached_at=NULL,detached_by_agent_id=NULL,
			other_contact_id=NULLIF(COALESCE(m.other_contact_id,f.contact_id),t.contact_id),conversation_id=t.id
		FROM conversations f, conversations t
		WHERE m.workspace_id=$1 AND m.id=ANY($2::uuid[]) AND f.workspace_id=m.workspace_id AND f.id=m.conversation_id AND t.workspace_id=m.workspace_id AND t.id=$3`,
		workspaceID, ids, toID); err != nil {
		return fmt.Errorf("put taken back messages in a conversation: %w", err)
	}
	if err := relinkItems(ctx, tx, workspaceID, ids, "", toID, "agent", actor.AgentID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE channel_thread_items SET disposition='conversation',disposition_reason=NULL WHERE workspace_id=$1 AND message_id=ANY($2::uuid[])`,
		workspaceID, ids); err != nil {
		return fmt.Errorf("conclude taken back chat items: %w", err)
	}
	if err := setStatusActor(ctx, tx, actor.AgentID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE conversations SET status=CASE WHEN status IN ('resolved','closed') THEN 'open' ELSE status END,updated_at=$3
		WHERE workspace_id=$1 AND id=$2`, workspaceID, toID, now); err != nil {
		return fmt.Errorf("reopen conversation: %w", err)
	}
	return nil
}

// DetachedCard is a card a person made by taking messages back to the queue, with its messages and the
// conversation they were taken from; nil for any other card.
type DetachedCard struct {
	MessageIDs     []string
	ConversationID string
}

// DetachedCardOf reads whether a card holds messages taken back to the queue.
func (s *Store) DetachedCardOf(ctx context.Context, workspaceID, gatheringID string) (*DetachedCard, error) {
	if !isID(gatheringID) {
		return nil, ErrGatheringNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*DetachedCard, error) {
		var rule string
		err := tx.QueryRow(ctx, `SELECT rules_rule FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid`, workspaceID, gatheringID).Scan(&rule)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGatheringNotFound
		}
		if err != nil || rule != RuleDetached {
			return nil, err
		}
		var card DetachedCard
		err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT m.id::text),'{}'),COALESCE(min(m.conversation_id::text),'') FROM channel_thread_items i
			JOIN messages m ON m.workspace_id=i.workspace_id AND m.id=i.message_id WHERE i.workspace_id=$1 AND i.gathering_id=$2::uuid`, workspaceID, gatheringID).
			Scan(&card.MessageIDs, &card.ConversationID)
		return &card, err
	})
}

// DecideDetachedCard adds the messages of a card taken back to the queue to conversation toID, or to a new
// conversation with subject when toID is empty: the messages move, nothing is replayed. A card decided
// meanwhile is ErrGatheringDecided.
func (s *Store) DecideDetachedCard(ctx context.Context, workspaceID, gatheringID, toID, subject string, apply IntakeApply, now time.Time) (GatheringOutcome, error) {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		g, err := lockGathering(ctx, tx, workspaceID, gatheringID, nil)
		if err != nil {
			return struct{}{}, err
		}
		var ids []string
		var fromID string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT m.id::text),'{}'),COALESCE(min(m.conversation_id::text),'') FROM channel_thread_items i
			JOIN messages m ON m.workspace_id=i.workspace_id AND m.id=i.message_id WHERE i.workspace_id=$1 AND i.gathering_id=$2::uuid AND m.detached_at IS NOT NULL`,
			workspaceID, gatheringID).Scan(&ids, &fromID); err != nil {
			return struct{}{}, fmt.Errorf("read taken back messages: %w", err)
		}
		if len(ids) == 0 {
			return struct{}{}, ErrGatheringDecided
		}
		locked, err := lockMoveConversations(ctx, tx, workspaceID, fromID, toID)
		if err != nil {
			return struct{}{}, err
		}
		// A card joins only a conversation it offers, so its reply never goes to someone else.
		if toID != "" {
			may, err := gatheringMayJoin(ctx, tx, workspaceID, gatheringID, toID)
			if err != nil {
				return struct{}{}, err
			}
			if !may {
				return struct{}{}, ErrNotACandidate
			}
		}
		state := GatheringAttached
		if toID == "" && subject == "" {
			// Like a chat's new request: the first line of what the customer wrote first.
			if err := tx.QueryRow(ctx, `SELECT left(split_part(body,E'\n',1),200) FROM messages WHERE workspace_id=$1 AND id=ANY($2::uuid[])
				ORDER BY created_at,id LIMIT 1`, workspaceID, ids).Scan(&subject); err != nil {
				return struct{}{}, fmt.Errorf("read the subject of taken back messages: %w", err)
			}
		}
		if toID == "" {
			source := locked[fromID]
			created, err := splitConversation(ctx, tx, workspaceID, source, nil, subject, now)
			if err != nil {
				return struct{}{}, err
			}
			toID, state = created.id, GatheringCreated
		} else if _, ok := locked[toID]; !ok {
			return struct{}{}, pgx.ErrNoRows
		}
		if err := attachDetached(ctx, tx, workspaceID, ids, toID, apply.Actor.asActor(), now); err != nil {
			return struct{}{}, err
		}
		if apply.Decision != nil {
			apply.Decision.TargetConversationID = toID
		}
		return struct{}{}, concludeGathering(ctx, tx, workspaceID, apply, g, state, toID, "", now)
	})
	if err != nil {
		return GatheringOutcome{}, err
	}
	return s.GatheringOutcomeOf(ctx, workspaceID, gatheringID)
}
