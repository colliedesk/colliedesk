package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// What intake says a batch adds to the conversation it joins (messages.intake_reply_kind).
const (
	ReplySubstantive     = "substantive"
	ReplyAcknowledgement = "acknowledgement"
)

// ErrNotASuggestion means a decision named as AI's suggestion of a card is not one: another card's, a
// person's, another workspace's, or none.
var ErrNotASuggestion = errors.New("that is not an AI suggestion of this card")

// AISuggestion is what AI suggested of a card and did not do, as a person's decision is labelled by it.
type AISuggestion struct {
	ID, Action, TargetConversationID, ReplyKind string
}

// SuggestionToLabel is the AI suggestion a person's decision of a card follows or corrects: the one they
// name (ErrNotASuggestion unless it is an unapplied AI decision of the card), else the newest; nil when
// AI suggested nothing.
func (s *Store) SuggestionToLabel(ctx context.Context, workspaceID, gatheringID, decisionID string) (*AISuggestion, error) {
	if decisionID != "" && !isID(decisionID) {
		return nil, ErrNotASuggestion
	}
	if !isID(gatheringID) {
		return nil, ErrGatheringNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*AISuggestion, error) {
		var found AISuggestion
		err := tx.QueryRow(ctx, `SELECT id::text,action,COALESCE(target_conversation_id::text,''),COALESCE(reply_kind,'') FROM intake_decisions
			WHERE workspace_id=$1 AND gathering_id=$2::uuid AND decider='ai' AND NOT applied AND action<>'review' AND ($3='' OR id=NULLIF($3,'')::uuid)
			ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, gatheringID, decisionID).Scan(&found.ID, &found.Action, &found.TargetConversationID, &found.ReplyKind)
		switch {
		case errors.Is(err, pgx.ErrNoRows) && decisionID != "":
			return nil, ErrNotASuggestion
		case errors.Is(err, pgx.ErrNoRows):
			return nil, nil
		case err != nil:
			return nil, fmt.Errorf("read AI suggestion: %w", err)
		}
		return &found, nil
	})
}

// RecordAIDecision writes, in tx (the AI task's, so a cancelled task records nothing), what AI decided
// of a card and did not apply, with why. A card a person decided meanwhile says so (person_first).
// It reports whether the card still waits for its decider (deciding).
func RecordAIDecision(ctx context.Context, tx pgx.Tx, workspaceID, gatheringID string, d IntakeDecisionInput, now time.Time) (bool, error) {
	var state string
	var decidedBy *string
	err := tx.QueryRow(ctx, `SELECT state,decided_by FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, workspaceID, gatheringID).
		Scan(&state, &decidedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrGatheringNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock gathering: %w", err)
	}
	if decidedBy != nil && *decidedBy == DecidedByAgent {
		d.NotAppliedReason = "person_first"
	}
	if d.NotAppliedReason == "" {
		return false, errors.New("an AI decision recorded without being applied says why")
	}
	return state == GatheringDeciding, insertIntakeDecision(ctx, tx, workspaceID, gatheringID, DecidedByAI, "model", d, d.ItemsSeen, false, ChannelActor{}, now)
}

// IntakeReasonRetention is how long the reason AI gave for an intake decision is kept: written from what
// customers said, it goes before the decision does.
const IntakeReasonRetention = 90 * 24 * time.Hour

// PruneIntakeReasons erases, in every workspace, the reasons of AI's intake decisions older than
// IntakeReasonRetention. serve runs it at start and daily.
func (s *Store) PruneIntakeReasons(ctx context.Context) (int64, error) {
	var pruned int64
	err := s.db.Privileged(ctx, database.PrivilegeIntakeReasonPrune, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE intake_decisions SET reason=NULL WHERE decider='ai' AND reason IS NOT NULL AND created_at<$1`,
			s.now().Add(-IntakeReasonRetention))
		if err != nil {
			return fmt.Errorf("prune intake reasons: %w", err)
		}
		pruned = tag.RowsAffected()
		return nil
	})
	return pruned, err
}

// relatedConversation is the newest other conversation of the contact, for a new request of a customer
// who had one before.
func relatedConversation(ctx context.Context, tx pgx.Tx, workspaceID, contactID, conversationID string) (string, error) {
	var related string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT id::text FROM conversations WHERE workspace_id=$1 AND contact_id=$2::uuid AND id<>$3::uuid
		ORDER BY created_at DESC,id DESC LIMIT 1),'')`, workspaceID, contactID, conversationID).Scan(&related)
	if err != nil {
		return "", fmt.Errorf("read related conversation: %w", err)
	}
	return related, nil
}

// mergeDepth bounds how far a conversation merged into one merged in turn is followed.
const mergeDepth = 10

// SurvivingConversation is the conversation a message joins in place of conversationID: itself, or the
// one it was merged into (in turn, as far as that goes). pgx.ErrNoRows when the workspace has no such
// conversation.
func (s *Store) SurvivingConversation(ctx context.Context, workspaceID, conversationID string) (string, error) {
	if !isID(conversationID) {
		return "", pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (string, error) {
		var survivor string
		err := tx.QueryRow(ctx, `WITH RECURSIVE chain(id,merged_into_id,depth) AS (
				SELECT id,merged_into_id,0 FROM conversations WHERE workspace_id=$1 AND id=$2::uuid
				UNION ALL
				SELECT c.id,c.merged_into_id,chain.depth+1 FROM chain JOIN conversations c ON c.workspace_id=$1 AND c.id=chain.merged_into_id WHERE chain.depth<$3)
			SELECT id::text FROM chain ORDER BY depth DESC LIMIT 1`, workspaceID, conversationID, mergeDepth).Scan(&survivor)
		return survivor, err
	})
}

// enqueueIntake queues AI's task for a gathering (a suggestion of what the rules decided), unless one
// waits or runs for it already. The worker picks it up on its next round.
func enqueueIntake(ctx context.Context, tx pgx.Tx, workspaceID, gatheringID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO ai_tasks(workspace_id,kind,gathering_id,automatic,state,next_run_at,created_at)
		VALUES($1,'intake',$2::uuid,true,'queued',$3,$3)
		ON CONFLICT (workspace_id,gathering_id) WHERE kind='intake' AND state IN ('queued','running') DO NOTHING`, workspaceID, gatheringID, now); err != nil {
		return fmt.Errorf("queue intake task: %w", err)
	}
	return nil
}

// enqueueLaneTurn gives the lane of a gathering its next turn with AI: one card of a lane is with AI at
// a time, in the order they closed, and none while a card of the lane waits for a person. A card is
// given to AI once; one whose task ended without deciding waits for the rules.
func enqueueLaneTurn(ctx context.Context, tx pgx.Tx, workspaceID, gatheringID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `WITH lane AS (SELECT lane_key FROM intake_gatherings WHERE workspace_id=$1 AND id=$2::uuid)
		INSERT INTO ai_tasks(workspace_id,kind,gathering_id,automatic,state,next_run_at,created_at)
		SELECT g.workspace_id,'intake',g.id,true,'queued',$3,$3 FROM intake_gatherings g, lane
		WHERE g.workspace_id=$1 AND g.lane_key=lane.lane_key AND g.state='deciding'
			AND NOT EXISTS(SELECT 1 FROM ai_tasks t WHERE t.workspace_id=g.workspace_id AND t.gathering_id=g.id AND t.kind='intake')
			AND NOT EXISTS(SELECT 1 FROM intake_gatherings o WHERE o.workspace_id=g.workspace_id AND o.lane_key=g.lane_key AND o.state='review')
			AND NOT EXISTS(SELECT 1 FROM ai_tasks t JOIN intake_gatherings o ON o.workspace_id=t.workspace_id AND o.id=t.gathering_id
				WHERE t.workspace_id=g.workspace_id AND t.kind='intake' AND t.state IN ('queued','running') AND o.lane_key=g.lane_key)
		ORDER BY g.closed_at,g.id LIMIT 1
		ON CONFLICT (workspace_id,gathering_id) WHERE kind='intake' AND state IN ('queued','running') DO NOTHING`, workspaceID, gatheringID, now); err != nil {
		return fmt.Errorf("queue intake lane turn: %w", err)
	}
	return nil
}
