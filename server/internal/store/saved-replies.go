package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const savedReplyColumns = `id::text,title,body,created_by_agent_id::text,created_at,updated_at`

var savedReplyID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func scanSavedReply(row pgx.Row) (domain.SavedReply, error) {
	var reply domain.SavedReply
	err := row.Scan(&reply.ID, &reply.Title, &reply.Body, &reply.CreatedByAgentID, &reply.CreatedAt, &reply.UpdatedAt)
	return reply, err
}

func isDuplicateTitle(err error) bool {
	var violation *pgconn.PgError
	return errors.As(err, &violation) && violation.Code == uniqueViolation && violation.ConstraintName == "saved_replies_workspace_title_idx"
}

// ListSavedReplies returns the workspace's saved replies ordered by title ignoring case.
func (s *Store) ListSavedReplies(ctx context.Context, workspaceID string) ([]domain.SavedReply, error) {
	replies, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.SavedReply, error) {
		rows, err := tx.Query(ctx, `SELECT `+savedReplyColumns+` FROM saved_replies WHERE workspace_id=$1 ORDER BY lower(title), title`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SavedReply, error) { return scanSavedReply(row) })
	})
	if err != nil {
		return nil, fmt.Errorf("list saved replies: %w", err)
	}
	return replies, nil
}

// InsertSavedReply adds a saved reply inside tx. It answers domain.ErrSavedReplyLimit when the
// workspace is full and domain.ErrDuplicateTitle when the title is taken. An advisory lock keeps two
// concurrent inserts from passing the count together.
func InsertSavedReply(ctx context.Context, tx pgx.Tx, workspaceID string, agentID *string, title, body string) (domain.SavedReply, error) {
	title, err := domain.CleanSavedReplyTitle(title)
	if err != nil {
		return domain.SavedReply{}, err
	}
	if err := domain.CheckSavedReplyBody(body); err != nil {
		return domain.SavedReply{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('saved-replies|' || $1::text, 0))`, workspaceID); err != nil {
		return domain.SavedReply{}, fmt.Errorf("lock saved replies: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM saved_replies WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
		return domain.SavedReply{}, fmt.Errorf("count saved replies: %w", err)
	}
	if count >= domain.MaxSavedReplies {
		return domain.SavedReply{}, domain.ErrSavedReplyLimit
	}
	// A savepoint keeps a duplicate title from aborting the caller's transaction.
	sub, err := tx.Begin(ctx)
	if err != nil {
		return domain.SavedReply{}, fmt.Errorf("insert saved reply: %w", err)
	}
	reply, err := scanSavedReply(sub.QueryRow(ctx, `INSERT INTO saved_replies(workspace_id,title,body,created_by_agent_id) VALUES($1,$2,$3,$4)
		RETURNING `+savedReplyColumns, workspaceID, title, body, agentID))
	if err != nil {
		_ = sub.Rollback(ctx)
		if isDuplicateTitle(err) {
			return domain.SavedReply{}, domain.ErrDuplicateTitle
		}
		return domain.SavedReply{}, fmt.Errorf("insert saved reply: %w", err)
	}
	if err := sub.Commit(ctx); err != nil {
		return domain.SavedReply{}, fmt.Errorf("insert saved reply: %w", err)
	}
	return reply, nil
}

// CreateSavedReply adds a saved reply written by an agent.
func (s *Store) CreateSavedReply(ctx context.Context, workspaceID, agentID, title, body string) (domain.SavedReply, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedReply, error) {
		return InsertSavedReply(ctx, tx, workspaceID, &agentID, title, body)
	})
}

// UpdateSavedReply changes the title, the body or both; a nil field stays as it is.
func (s *Store) UpdateSavedReply(ctx context.Context, workspaceID, id string, title, body *string) (domain.SavedReply, error) {
	if !savedReplyID.MatchString(id) {
		return domain.SavedReply{}, domain.ErrSavedReplyNotFound
	}
	if title != nil {
		cleaned, err := domain.CleanSavedReplyTitle(*title)
		if err != nil {
			return domain.SavedReply{}, err
		}
		title = &cleaned
	}
	if body != nil {
		if err := domain.CheckSavedReplyBody(*body); err != nil {
			return domain.SavedReply{}, err
		}
	}
	reply, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedReply, error) {
		return scanSavedReply(tx.QueryRow(ctx, `UPDATE saved_replies SET title=COALESCE($3,title), body=COALESCE($4,body), updated_at=now()
			WHERE workspace_id=$1 AND id=$2 RETURNING `+savedReplyColumns, workspaceID, id, title, body))
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.SavedReply{}, domain.ErrSavedReplyNotFound
	case isDuplicateTitle(err):
		return domain.SavedReply{}, domain.ErrDuplicateTitle
	case err != nil:
		return domain.SavedReply{}, fmt.Errorf("update saved reply: %w", err)
	}
	return reply, nil
}

// DeleteSavedReply removes a saved reply and reports whether it existed in the workspace.
func (s *Store) DeleteSavedReply(ctx context.Context, workspaceID, id string) (bool, error) {
	if !savedReplyID.MatchString(id) {
		return false, nil
	}
	deleted, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `DELETE FROM saved_replies WHERE workspace_id=$1 AND id=$2`, workspaceID, id)
		return result.RowsAffected() == 1, err
	})
	if err != nil {
		return false, fmt.Errorf("delete saved reply: %w", err)
	}
	return deleted, nil
}
