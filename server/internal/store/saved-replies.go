package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const savedReplyColumns = `id::text,title,body,created_by_agent_id::text,created_at,updated_at`

var savedReplyID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func scanSavedReply(row pgx.Row) (domain.SavedReply, error) {
	var reply domain.SavedReply
	err := row.Scan(&reply.ID, &reply.Title, &reply.Body, &reply.CreatedByAgentID, &reply.CreatedAt, &reply.UpdatedAt)
	reply.CreatedAt, reply.UpdatedAt = domain.UTC(reply.CreatedAt), domain.UTC(reply.UpdatedAt)
	return reply, err
}

func isDuplicateTitle(err error) bool {
	var violation *pgconn.PgError
	return errors.As(err, &violation) && violation.Code == uniqueViolation && violation.ConstraintName == "saved_replies_workspace_title_idx"
}

// ListSavedReplies returns the workspace's saved replies outside the trash, ordered by title ignoring
// case, each with its newest version.
func (s *Store) ListSavedReplies(ctx context.Context, workspaceID string) ([]domain.SavedReply, error) {
	replies, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.SavedReply, error) {
		rows, err := tx.Query(ctx, savedReplyListSelect(false)+` WHERE r.workspace_id=$1 AND r.deleted_at IS NULL ORDER BY lower(r.title), r.title`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SavedReply, error) {
			reply, err := scanListedSavedReply(row, nil)
			return reply.SavedReply, err
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list saved replies: %w", err)
	}
	return replies, nil
}

// InsertSavedReply adds a saved reply and its first version inside tx. It answers
// domain.ErrSavedReplyLimit when the workspace is full and domain.ErrDuplicateTitle when the title is
// taken; replies in the trash count for neither. An advisory lock keeps two concurrent inserts from
// passing the count together.
func InsertSavedReply(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, title, body string) (domain.SavedReply, error) {
	title, err := domain.CleanSavedReplyTitle(title)
	if err != nil {
		return domain.SavedReply{}, err
	}
	if err := domain.CheckSavedReplyBody(body); err != nil {
		return domain.SavedReply{}, err
	}
	if err := checkSavedReplyRoom(ctx, tx, workspaceID); err != nil {
		return domain.SavedReply{}, err
	}
	// A savepoint keeps a duplicate title from aborting the caller's transaction.
	sub, err := tx.Begin(ctx)
	if err != nil {
		return domain.SavedReply{}, fmt.Errorf("insert saved reply: %w", err)
	}
	reply, err := scanSavedReply(sub.QueryRow(ctx, `INSERT INTO saved_replies(workspace_id,title,body,created_by_agent_id) VALUES($1,$2,$3,$4)
		RETURNING `+savedReplyColumns, workspaceID, title, body, optionalID(actor.AgentID)))
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
	if reply.LastChange, err = addSavedReplyVersion(ctx, tx, workspaceID, reply.ID, actor, domain.SavedReplyCreated, nil); err != nil {
		return domain.SavedReply{}, err
	}
	return reply, nil
}

// checkSavedReplyRoom locks the workspace's saved replies and answers domain.ErrSavedReplyLimit when
// MaxSavedReplies are outside the trash.
func checkSavedReplyRoom(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('saved-replies|' || $1::text, 0))`, workspaceID); err != nil {
		return fmt.Errorf("lock saved replies: %w", err)
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM saved_replies WHERE workspace_id=$1 AND deleted_at IS NULL`, workspaceID).Scan(&count); err != nil {
		return fmt.Errorf("count saved replies: %w", err)
	}
	if count >= domain.MaxSavedReplies {
		return domain.ErrSavedReplyLimit
	}
	return nil
}

// CreateSavedReply adds a saved reply written by actor.
func (s *Store) CreateSavedReply(ctx context.Context, workspaceID string, actor Actor, title, body string) (domain.SavedReply, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedReply, error) {
		return InsertSavedReply(ctx, tx, workspaceID, actor, title, body)
	})
}

// UpdateSavedReply changes the title, the body or both and records the version; a nil field stays as
// it is. seen is the updated_at the client read: when the reply was saved since, it answers
// domain.ErrSavedReplyChanged. A reply in the trash is not found.
func (s *Store) UpdateSavedReply(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time, title, body *string) (domain.SavedReply, error) {
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
	return s.changeSavedReply(ctx, workspaceID, actor, id, domain.SavedReplyEdited, nil, func(tx pgx.Tx) pgx.Row {
		return tx.QueryRow(ctx, `UPDATE saved_replies SET title=COALESCE($3,title), body=COALESCE($4,body), updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND updated_at=$5 RETURNING `+savedReplyColumns, workspaceID, id, title, body, seen)
	})
}

// RestoreSavedReplyVersion makes version the current text of the reply as a new version; the history
// stays as it is. seen works as in UpdateSavedReply.
func (s *Store) RestoreSavedReplyVersion(ctx context.Context, workspaceID string, actor Actor, id string, version int, seen time.Time) (domain.SavedReply, error) {
	if !savedReplyID.MatchString(id) {
		return domain.SavedReply{}, domain.ErrSavedReplyNotFound
	}
	return s.changeSavedReply(ctx, workspaceID, actor, id, domain.SavedReplyRestored, &version, func(tx pgx.Tx) pgx.Row {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saved_reply_versions WHERE workspace_id=$1 AND saved_reply_id=$2 AND version=$3)`,
			workspaceID, id, version).Scan(&exists); err == nil && !exists {
			return errorRow{domain.ErrSavedReplyVersionNotFound}
		}
		return tx.QueryRow(ctx, `UPDATE saved_replies r SET title=v.title, body=v.body, updated_at=clock_timestamp() FROM saved_reply_versions v
			WHERE r.workspace_id=$1 AND r.id=$2 AND r.deleted_at IS NULL AND r.updated_at=$4
			AND v.workspace_id=r.workspace_id AND v.saved_reply_id=r.id AND v.version=$3
			RETURNING r.id::text,r.title,r.body,r.created_by_agent_id::text,r.created_at,r.updated_at`, workspaceID, id, version, seen)
	})
}

// changeSavedReply runs an optimistic write of a live reply and records it as a version of kind. No
// row means the reply is gone or in the trash (not found) or was saved since (changed).
func (s *Store) changeSavedReply(ctx context.Context, workspaceID string, actor Actor, id, kind string, restoredFrom *int, write func(pgx.Tx) pgx.Row) (domain.SavedReply, error) {
	reply, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedReply, error) {
		reply, err := scanSavedReply(write(tx))
		if errors.Is(err, pgx.ErrNoRows) {
			var live bool
			if checkErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saved_replies WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL)`, workspaceID, id).Scan(&live); checkErr == nil && live {
				return domain.SavedReply{}, domain.ErrSavedReplyChanged
			}
		}
		if err != nil {
			return domain.SavedReply{}, err
		}
		reply.LastChange, err = addSavedReplyVersion(ctx, tx, workspaceID, id, actor, kind, restoredFrom)
		return reply, err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.SavedReply{}, domain.ErrSavedReplyNotFound
	case isDuplicateTitle(err):
		return domain.SavedReply{}, domain.ErrDuplicateTitle
	case errors.Is(err, domain.ErrSavedReplyChanged), errors.Is(err, domain.ErrSavedReplyVersionNotFound):
		return domain.SavedReply{}, err
	case err != nil:
		return domain.SavedReply{}, fmt.Errorf("change saved reply: %w", err)
	}
	return reply, nil
}

// DeleteSavedReply moves a reply to the trash and reports whether it was outside it in the workspace.
func (s *Store) DeleteSavedReply(ctx context.Context, workspaceID string, actor Actor, id string) (bool, error) {
	if !savedReplyID.MatchString(id) {
		return false, nil
	}
	deleted, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `UPDATE saved_replies SET deleted_at=$3, deleted_by_agent_id=$4 WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`,
			workspaceID, id, s.now(), optionalID(actor.AgentID))
		if err != nil || result.RowsAffected() != 1 {
			return false, err
		}
		_, err = addSavedReplyVersion(ctx, tx, workspaceID, id, actor, domain.SavedReplyDeleted, nil)
		return err == nil, err
	})
	if err != nil {
		return false, fmt.Errorf("delete saved reply: %w", err)
	}
	return deleted, nil
}

// UndeleteSavedReply takes a reply out of the trash as it was. A reply not in the trash is not found;
// a title another reply took meanwhile is domain.ErrDuplicateTitle, a full workspace
// domain.ErrSavedReplyLimit.
func (s *Store) UndeleteSavedReply(ctx context.Context, workspaceID string, actor Actor, id string) (domain.SavedReply, error) {
	if !savedReplyID.MatchString(id) {
		return domain.SavedReply{}, domain.ErrSavedReplyNotFound
	}
	reply, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedReply, error) {
		if err := checkSavedReplyRoom(ctx, tx, workspaceID); err != nil {
			return domain.SavedReply{}, err
		}
		reply, err := scanSavedReply(tx.QueryRow(ctx, `UPDATE saved_replies SET deleted_at=NULL, deleted_by_agent_id=NULL
			WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NOT NULL RETURNING `+savedReplyColumns, workspaceID, id))
		if err != nil {
			return domain.SavedReply{}, err
		}
		reply.LastChange, err = addSavedReplyVersion(ctx, tx, workspaceID, id, actor, domain.SavedReplyUndeleted, nil)
		return reply, err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.SavedReply{}, domain.ErrSavedReplyNotFound
	case isDuplicateTitle(err):
		return domain.SavedReply{}, domain.ErrDuplicateTitle
	case errors.Is(err, domain.ErrSavedReplyLimit):
		return domain.SavedReply{}, err
	case err != nil:
		return domain.SavedReply{}, fmt.Errorf("undelete saved reply: %w", err)
	}
	return reply, nil
}

// errorRow is a pgx.Row whose Scan answers err, for a write that is refused before it runs.
type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
