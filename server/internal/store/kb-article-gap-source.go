package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// kbArticleGapMeta is what source_meta.gap holds on an article AI drafted from a knowledge gap.
type kbArticleGapMeta struct {
	ID              string   `json:"id"`
	ConversationIDs []string `json:"conversation_ids"`
}

// writeKBArticleGapSource records the gap an AI draft was written for and the conversations it read.
// Without a gap it does nothing.
func writeKBArticleGapSource(ctx context.Context, tx pgx.Tx, workspaceID, articleID, gapID string, conversationIDs []string) error {
	if gapID == "" {
		return nil
	}
	if conversationIDs == nil {
		conversationIDs = []string{}
	}
	meta, err := json.Marshal(map[string]kbArticleGapMeta{"gap": {ID: gapID, ConversationIDs: conversationIDs}})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET source_meta=COALESCE(source_meta,'{}'::jsonb) || $3::jsonb WHERE workspace_id=$1 AND id=$2`,
		workspaceID, articleID, meta); err != nil {
		return fmt.Errorf("record the gap the article was drafted from: %w", err)
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
	source := &domain.KBArticleGapSource{GapID: meta.ID, Conversations: []domain.KBArticleGapConversation{}}
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
