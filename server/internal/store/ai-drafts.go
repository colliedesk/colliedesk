package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// SentUnchangedSimilarity is the similarity from which a reply sent from an AI draft counts as sent
// nearly as written (#100); below it, sent edited. The number itself is kept, so the line can move.
const SentUnchangedSimilarity = 0.9

// AIDraftSimilarity is 1 minus the word-level edit distance between the draft and the reply sent,
// over the longer of the two, after collapsing whitespace: 1 is the same words in the same order.
func AIDraftSimilarity(draft, sent string) float64 {
	a, b := strings.Fields(draft), strings.Fields(sent)
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return 1 - float64(previous[len(b)])/float64(max(len(a), len(b)))
}

// lockAIDraft reads and locks the text of the AI draft a reply is written from: it must be this
// conversation's, ready and undecided. No draft id reads "".
func lockAIDraft(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	var draft string
	err := tx.QueryRow(ctx, `SELECT text FROM ai_drafts WHERE workspace_id=$1 AND id=$2 AND conversation_id=$3 AND status='ready' AND outcome IS NULL AND text IS NOT NULL FOR UPDATE`,
		workspaceID, id, conversationID).Scan(&draft)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrAIDraftNotReady
	}
	if err != nil {
		return "", fmt.Errorf("read the AI draft: %w", err)
	}
	return draft, nil
}

// recordAIDraftSend records how close the reply stayed to the draft lockAIDraft read, in the
// transaction that stores the reply. A reply sent without a draft discards the sender's ready drafts
// on the conversation (an agent token's, its own): they wrote something else.
func recordAIDraftSend(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string, actor Actor, messageID, draft, body string) error {
	if actor.AIDraftID == "" {
		if actor.AgentID == "" {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE ai_drafts SET outcome='discarded',decided_at=now(),decided_by_agent_id=$3
			WHERE workspace_id=$1 AND conversation_id=$2 AND requested_by_agent_id=$3 AND requested_by_token_id IS NOT DISTINCT FROM NULLIF($4,'')::uuid
				AND status='ready' AND outcome IS NULL`,
			workspaceID, conversationID, actor.AgentID, actor.TokenID)
		return err
	}
	similarity := AIDraftSimilarity(draft, body)
	outcome := "sent_edited"
	if similarity >= SentUnchangedSimilarity {
		outcome = "sent_unchanged"
	}
	_, err := tx.Exec(ctx, `UPDATE ai_drafts SET outcome=$3,similarity=round($4::numeric,3),sent_message_id=$5,decided_at=now(),decided_by_agent_id=$6
		WHERE workspace_id=$1 AND id=$2`, workspaceID, actor.AIDraftID, outcome, similarity, messageID, nullableID(actor.AgentID))
	return err
}

func nullableID(value string) any {
	if value == "" {
		return nil
	}
	return value
}
