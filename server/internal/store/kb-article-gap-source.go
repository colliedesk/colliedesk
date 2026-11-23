package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbArticleGapMeta is what source_meta.gap holds on an article drafted from a knowledge gap.
type kbArticleGapMeta struct {
	ID              string   `json:"id"`
	ConversationIDs []string `json:"conversation_ids"`
	Requested       bool     `json:"requested,omitempty"`
}

// kbArticleReplyMeta is what source_meta.reply holds on an article made from a sent reply: the
// subject and the customer question, both masked when the article was made.
type kbArticleReplyMeta struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Subject        string `json:"subject"`
	Question       string `json:"question"`
}

// writeKBArticleSource records the gap a draft was written for and the conversations it read, and
// the sent reply it was made from. Without either it does nothing.
func writeKBArticleSource(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, input KBArticleInput) error {
	meta := map[string]any{}
	if input.GapID != "" {
		ids := input.GapConversationIDs
		if ids == nil {
			ids = []string{}
		}
		meta["gap"] = kbArticleGapMeta{ID: input.GapID, ConversationIDs: ids, Requested: input.GapRequested}
	}
	if reply := input.Reply; reply != nil {
		meta["reply"] = kbArticleReplyMeta{ConversationID: reply.ConversationID, MessageID: reply.MessageID,
			Subject: reply.Subject, Question: reply.Question}
	}
	if len(meta) == 0 {
		return nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET source_meta=COALESCE(source_meta,'{}'::jsonb) || $3::jsonb WHERE workspace_id=$1 AND id=$2`,
		workspaceID, articleID, raw); err != nil {
		return fmt.Errorf("record what the article was drafted from: %w", err)
	}
	return nil
}

// readKBArticleGapSource turns source_meta.gap into the gap and the conversations it names, in the
// stored order. A conversation of another workspace or one since deleted is left out.
func readKBArticleGapSource(ctx context.Context, tx pgx.Tx, workspaceID string, raw []byte) (*domain.KBArticleGapSource, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var meta kbArticleGapMeta
	if err := json.Unmarshal(raw, &meta); err != nil || meta.ID == "" {
		return nil, nil
	}
	source := &domain.KBArticleGapSource{GapID: meta.ID, Requested: meta.Requested, Conversations: []domain.KBArticleGapConversation{}}
	if len(meta.ConversationIDs) == 0 {
		return source, nil
	}
	rows, err := tx.Query(ctx, `SELECT c.id::text, COALESCE(c.display_ref,''), c.subject
		FROM unnest($2::uuid[]) WITH ORDINALITY AS u(id, place)
		JOIN conversations c ON c.workspace_id=$1 AND c.id=u.id
		ORDER BY u.place`, workspaceID, meta.ConversationIDs)
	if err != nil {
		return nil, fmt.Errorf("read the conversations an article was drafted from: %w", err)
	}
	conversations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleGapConversation, error) {
		var c domain.KBArticleGapConversation
		return c, row.Scan(&c.ConversationID, &c.DisplayRef, &c.Subject)
	})
	if err != nil {
		return nil, fmt.Errorf("read the conversations an article was drafted from: %w", err)
	}
	source.Conversations = conversations
	return source, nil
}

// readKBArticleReplySource turns source_meta.reply into the reply and its conversation with its code
// today; nil once the conversation is deleted.
func readKBArticleReplySource(ctx context.Context, tx pgx.Tx, workspaceID string, raw []byte) (*domain.KBArticleReplySource, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var meta kbArticleReplyMeta
	if err := json.Unmarshal(raw, &meta); err != nil || !savedReplyID.MatchString(meta.ConversationID) {
		return nil, nil
	}
	source := &domain.KBArticleReplySource{ConversationID: meta.ConversationID, MessageID: meta.MessageID,
		Subject: meta.Subject, Question: meta.Question}
	err := tx.QueryRow(ctx, `SELECT COALESCE(display_ref,'') FROM conversations WHERE workspace_id=$1 AND id=$2`,
		workspaceID, meta.ConversationID).Scan(&source.DisplayRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the conversation an article was made from: %w", err)
	}
	return source, nil
}
