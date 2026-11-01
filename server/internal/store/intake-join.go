package store

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotACandidate refuses a decision that joins a card to a conversation where its reply would go to
// someone else: of another place or customer, or of another channel.
var ErrNotACandidate = errors.New("the conversation is not one this card may join")

// gatheringMayJoin says whether card gatheringID may join conversationID, read again in the transaction
// that applies the decision, so it holds whoever decides (a person, an agent token, AI or the rules): in a
// group, a conversation that holds or held something said in the same place (a message taken back from it
// still makes it one); in a one-to-one chat, one of the same customer on the same
// channel; or the conversation the rules named. Unlike the
// candidates a card offers, age and status do not matter here: a person may join a quiet or closed
// conversation of the same place, which then opens.
func gatheringMayJoin(ctx context.Context, tx Querier, workspaceID, gatheringID, conversationID string) (bool, error) {
	var may bool
	err := tx.QueryRow(ctx, `WITH card AS (
			SELECT g.channel_id,g.thread_id,g.rules_target,t.kind,COALESCE(g.contact_id,t.contact_id) AS contact_id
			FROM intake_gatherings g LEFT JOIN channel_threads t ON t.workspace_id=g.workspace_id AND t.id=g.thread_id
			WHERE g.workspace_id=$1 AND g.id=$2::uuid)
		SELECT EXISTS(SELECT 1 FROM conversations c, card WHERE c.workspace_id=$1 AND c.id=$3::uuid AND (
			c.id=card.rules_target
			OR (card.kind='group' AND EXISTS(SELECT 1 FROM channel_thread_items i JOIN conversation_thread_items l ON l.workspace_id=i.workspace_id
				AND l.item_id=i.id WHERE i.workspace_id=$1 AND i.thread_id=card.thread_id AND l.conversation_id=c.id))
			OR (card.kind IS DISTINCT FROM 'group' AND c.contact_id=card.contact_id AND c.channel_id=card.channel_id)))`,
		workspaceID, gatheringID, conversationID).Scan(&may)
	if err != nil {
		return false, fmt.Errorf("check the conversation a card joins: %w", err)
	}
	return may, nil
}
