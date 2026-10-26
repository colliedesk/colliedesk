package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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
