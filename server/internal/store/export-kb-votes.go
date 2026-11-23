package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportKBVotes writes every reset of an article's votes, then every vote, current and closed alike:
// after a reset the export is the only place the closed votes and their lines can still be read.
func exportKBVotes(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,article_id::text,helpful,not_helpful,reset_by_agent_id::text,token_id::text,token_label,created_at
		FROM kb_article_vote_resets WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base vote resets: %w", err)
	}
	for rows.Next() {
		var record export.KBArticleVoteReset
		if err := rows.Scan(&record.ID, &record.ArticleID, &record.Helpful, &record.NotHelpful, &record.ResetByAgentID, &record.TokenID, &record.TokenLabel, &record.CreatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base vote resets: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.KBArticleVoteReset(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base vote resets", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT id::text,article_id::text,encode(voter,'hex'),helpful,missing,reset_id::text,conversation_id::text,created_at,updated_at
		FROM kb_article_votes WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base votes: %w", err)
	}
	for rows.Next() {
		var record export.KBArticleVote
		if err := rows.Scan(&record.ID, &record.ArticleID, &record.Voter, &record.Helpful, &record.Missing, &record.ResetID, &record.ConversationID, &record.CreatedAt, &record.UpdatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base votes: %w", err)
		}
		record.CreatedAt, record.UpdatedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt)
		if err := sink.KBArticleVote(record); err != nil {
			rows.Close()
			return err
		}
	}
	return rowsError("export knowledge base votes", rows)
}
