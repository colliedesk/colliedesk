package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbVotesListed is how many current votes the team reads of one article, newest change first.
const kbVotesListed = 200

// KBArticleVotes are the votes of article id of workspaceID as the team reads them: the counts its page
// shows, the newest current votes and every reset. An article in the trash keeps them.
// domain.ErrKBArticleNotFound when the workspace has no such article.
func (s *Store) KBArticleVotes(ctx context.Context, workspaceID, id string) (domain.KBArticleVotes, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticleVotes{}, domain.ErrKBArticleNotFound
	}
	votes, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleVotes, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND id=$2)`, workspaceID, id).Scan(&exists); err != nil {
			return domain.KBArticleVotes{}, err
		}
		if !exists {
			return domain.KBArticleVotes{}, domain.ErrKBArticleNotFound
		}
		return readKBArticleVotes(ctx, tx, workspaceID, id)
	})
	return votes, kbError("read knowledge base article votes", err)
}

// ResetKBArticleVotes starts the count of article id again from 0: it keeps the counts the page showed
// as a reset by actor and closes the current votes, deleting nothing. Imported votes stop counting from
// the first reset. With nothing counted it stores nothing. domain.ErrKBArticleNotFound for an article in the trash or of another workspace.
func (s *Store) ResetKBArticleVotes(ctx context.Context, workspaceID string, actor Actor, id string) (domain.KBArticleVotes, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBArticleVotes{}, domain.ErrKBArticleNotFound
	}
	votes, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBArticleVotes, error) {
		// FOR UPDATE waits for votes in flight (they hold FOR SHARE), so each is counted here or after.
		err := tx.QueryRow(ctx, `SELECT id::text FROM kb_articles WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, workspaceID, id).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.KBArticleVotes{}, domain.ErrKBArticleNotFound
		}
		if err != nil {
			return domain.KBArticleVotes{}, err
		}
		current, imported, err := kbVoteCounts(ctx, tx, workspaceID, id)
		if err != nil {
			return domain.KBArticleVotes{}, err
		}
		if current.Helpful+current.NotHelpful+imported.Helpful+imported.NotHelpful == 0 {
			// Nothing counts yet (a repeated press): no empty reset joins the history.
			return readKBArticleVotes(ctx, tx, workspaceID, id)
		}
		var resetID string
		if err := tx.QueryRow(ctx, `INSERT INTO kb_article_vote_resets (workspace_id, article_id, helpful, not_helpful, reset_by_agent_id, token_id, token_label)
			SELECT $1, $2, $3, $4, $5, t.id, t.label FROM (SELECT 1) one LEFT JOIN sessions t ON t.workspace_id=$1 AND t.id=$6 RETURNING id::text`,
			workspaceID, id, current.Helpful+imported.Helpful, current.NotHelpful+imported.NotHelpful, optionalID(actor.AgentID), optionalID(actor.TokenID)).Scan(&resetID); err != nil {
			return domain.KBArticleVotes{}, fmt.Errorf("record votes reset: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE kb_article_votes SET reset_id=$3 WHERE workspace_id=$1 AND article_id=$2 AND reset_id IS NULL`, workspaceID, id, resetID); err != nil {
			return domain.KBArticleVotes{}, fmt.Errorf("close reset votes: %w", err)
		}
		if _, err := tx.Exec(ctx, bumpKBVotes, workspaceID); err != nil {
			return domain.KBArticleVotes{}, fmt.Errorf("bump votes version: %w", err)
		}
		return readKBArticleVotes(ctx, tx, workspaceID, id)
	})
	return votes, kbError("reset knowledge base article votes", err)
}

// readKBArticleVotes reads the counts, the newest current votes and the resets of article id in tx.
func readKBArticleVotes(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.KBArticleVotes, error) {
	current, imported, err := kbVoteCounts(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.KBArticleVotes{}, err
	}
	out := domain.KBArticleVotes{ArticleID: id, Helpful: current.Helpful + imported.Helpful, NotHelpful: current.NotHelpful + imported.NotHelpful, Imported: imported}
	rows, err := tx.Query(ctx, `SELECT v.helpful, v.missing, c.id::text, COALESCE(NULLIF(c.name, ''), c.email), v.created_at, v.updated_at
		FROM kb_article_votes v LEFT JOIN contacts c ON c.workspace_id=v.workspace_id AND c.id=v.contact_id
		WHERE v.workspace_id=$1 AND v.article_id=$2 AND v.reset_id IS NULL ORDER BY v.updated_at DESC, v.id LIMIT $3`, workspaceID, id, kbVotesListed)
	if err != nil {
		return out, fmt.Errorf("list article votes: %w", err)
	}
	if out.Votes, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleVote, error) {
		var vote domain.KBArticleVote
		var contactID, contactName *string
		if err := row.Scan(&vote.Helpful, &vote.Missing, &contactID, &contactName, &vote.CreatedAt, &vote.UpdatedAt); err != nil {
			return vote, err
		}
		if contactID != nil && contactName != nil {
			vote.Contact = &domain.KBVoteContact{ID: *contactID, Name: *contactName}
		}
		vote.CreatedAt, vote.UpdatedAt = domain.UTC(vote.CreatedAt), domain.UTC(vote.UpdatedAt)
		return vote, nil
	}); err != nil {
		return out, fmt.Errorf("list article votes: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT r.id::text, r.helpful, r.not_helpful, r.reset_by_agent_id::text, COALESCE(NULLIF(p.display_name, ''), p.email),
			r.token_id::text, r.token_label, r.created_at
		FROM kb_article_vote_resets r LEFT JOIN agents p ON p.workspace_id=r.workspace_id AND p.id=r.reset_by_agent_id
		WHERE r.workspace_id=$1 AND r.article_id=$2 ORDER BY r.created_at DESC, r.id`, workspaceID, id)
	if err != nil {
		return out, fmt.Errorf("list votes resets: %w", err)
	}
	if out.Resets, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBArticleVoteReset, error) {
		var reset domain.KBArticleVoteReset
		var helpful, notHelpful int64
		var agentID, name, tokenID, tokenLabel *string
		var at time.Time
		if err := row.Scan(&reset.ID, &helpful, &notHelpful, &agentID, &name, &tokenID, &tokenLabel, &at); err != nil {
			return reset, err
		}
		reset.Helpful, reset.NotHelpful, reset.ResetBy, reset.CreatedAt = int(helpful), int(notHelpful), savedReplyPerson(agentID, name), domain.UTC(at)
		if tokenLabel != nil {
			reset.Token = &domain.SavedReplyToken{ID: tokenID, Label: *tokenLabel}
		}
		return reset, nil
	}); err != nil {
		return out, fmt.Errorf("list votes resets: %w", err)
	}
	return out, nil
}
