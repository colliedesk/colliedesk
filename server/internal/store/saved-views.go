package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SavedViewChange holds the fields a PATCH sets; nil leaves a field as it is. Seen is the updated_at the
// caller read: a view saved since is not changed (domain.ErrSavedViewChanged).
type SavedViewChange struct {
	Seen    time.Time
	Name    *string
	Scope   *string
	Filters json.RawMessage
}

const savedViewColumns = `id::text,name,scope,filters,owner_agent_id::text,created_at,updated_at,deleted_at`

// visibleToAgent is the condition every read and write of a view carries: a shared view, or one of
// the caller's own. Another person's personal view does not exist for the caller.
const visibleToAgent = `(scope='shared' OR owner_agent_id=$2::uuid)`

func scanSavedView(row pgx.Row) (domain.SavedView, error) {
	var view domain.SavedView
	var filters []byte
	if err := row.Scan(&view.ID, &view.Name, &view.Scope, &filters, &view.OwnerAgentID, &view.CreatedAt, &view.UpdatedAt, &view.DeletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.SavedView{}, domain.ErrSavedViewNotFound
		}
		return domain.SavedView{}, fmt.Errorf("scan saved view: %w", err)
	}
	view.Filters = filters
	view.CreatedAt, view.UpdatedAt = domain.UTC(view.CreatedAt), domain.UTC(view.UpdatedAt)
	if view.DeletedAt != nil {
		deleted := domain.UTC(*view.DeletedAt)
		purges := deleted.Add(domain.SavedViewTrashRetention)
		view.DeletedAt, view.PurgesAt = &deleted, &purges
	}
	return view, nil
}

func collectSavedViews(rows pgx.Rows) ([]domain.SavedView, error) {
	defer rows.Close()
	views := []domain.SavedView{}
	for rows.Next() {
		view, err := scanSavedView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved views: %w", err)
	}
	return views, nil
}

// ListSavedViews returns the shared views, then the agent's personal ones, each by name ignoring case.
func (s *Store) ListSavedViews(ctx context.Context, workspaceID, agentID string) ([]domain.SavedView, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.SavedView, error) {
		rows, err := tx.Query(ctx, `SELECT `+savedViewColumns+` FROM saved_views
			WHERE workspace_id=$1 AND `+visibleToAgent+` AND deleted_at IS NULL
			ORDER BY scope='personal',lower(name),id`, workspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("list saved views: %w", err)
		}
		return collectSavedViews(rows)
	})
}

// ListDeletedSavedViews returns the views the agent could see that were deleted in the last 30
// days, newest deletion first. Older ones are removed for good first.
func (s *Store) ListDeletedSavedViews(ctx context.Context, workspaceID, agentID string) ([]domain.SavedView, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.SavedView, error) {
		if err := purgeSavedViews(ctx, tx, workspaceID, s.now()); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT `+savedViewColumns+` FROM saved_views
			WHERE workspace_id=$1 AND `+visibleToAgent+` AND deleted_at IS NOT NULL
			ORDER BY deleted_at DESC,id`, workspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("list deleted saved views: %w", err)
		}
		return collectSavedViews(rows)
	})
}

func purgeSavedViews(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM saved_views WHERE workspace_id=$1 AND deleted_at<$2`, workspaceID, now.Add(-domain.SavedViewTrashRetention)); err != nil {
		return fmt.Errorf("purge saved views: %w", err)
	}
	return nil
}

// lockSavedViews serializes the writes that count or name views of one workspace, so two at once
// cannot both pass the limit or the name check.
func lockSavedViews(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('saved_views:'||$1::text,0))`, workspaceID); err != nil {
		return fmt.Errorf("lock saved views: %w", err)
	}
	return nil
}

// checkSavedViewRoom refuses a view that would be the 51st of its group.
func checkSavedViewRoom(ctx context.Context, tx pgx.Tx, workspaceID, scope, ownerID, exceptID string) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM saved_views WHERE workspace_id=$1 AND deleted_at IS NULL AND scope=$2
		AND ($2='shared' OR owner_agent_id=NULLIF($3,'')::uuid) AND id::text<>$4`, workspaceID, scope, ownerID, exceptID).Scan(&count); err != nil {
		return fmt.Errorf("count saved views: %w", err)
	}
	if count >= domain.MaxSavedViews {
		return domain.ErrSavedViewLimit
	}
	return nil
}

func savedViewWriteError(err error) error {
	var database *pgconn.PgError
	if errors.As(err, &database) && database.Code == "23505" {
		return domain.ErrSavedViewNameTaken
	}
	return err
}

func ownerOf(scope, agentID string) *string {
	if scope == domain.SavedViewPersonal {
		return &agentID
	}
	return nil
}

func (s *Store) CreateSavedView(ctx context.Context, workspaceID, agentID, name, scope string, filters json.RawMessage) (domain.SavedView, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedView, error) {
		if err := lockSavedViews(ctx, tx, workspaceID); err != nil {
			return domain.SavedView{}, err
		}
		if err := checkSavedViewRoom(ctx, tx, workspaceID, scope, agentID, ""); err != nil {
			return domain.SavedView{}, err
		}
		now := s.now()
		view, err := scanSavedView(tx.QueryRow(ctx, `INSERT INTO saved_views(workspace_id,scope,owner_agent_id,name,filters,created_by_agent_id,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$7) RETURNING `+savedViewColumns, workspaceID, scope, ownerOf(scope, agentID), name, []byte(filters), agentID, now))
		return view, savedViewWriteError(err)
	})
}

// UpdateSavedView changes a live view the agent can see. Only the owner may share a personal view
// (the other person's view is not found); making a shared view personal gives it to the agent.
func (s *Store) UpdateSavedView(ctx context.Context, workspaceID, agentID, id string, change SavedViewChange) (domain.SavedView, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedView, error) {
		if err := lockSavedViews(ctx, tx, workspaceID); err != nil {
			return domain.SavedView{}, err
		}
		current, err := scanSavedView(tx.QueryRow(ctx, `SELECT `+savedViewColumns+` FROM saved_views
			WHERE workspace_id=$1 AND `+visibleToAgent+` AND id=$3::uuid AND deleted_at IS NULL FOR UPDATE`, workspaceID, agentID, id))
		if err != nil {
			return domain.SavedView{}, err
		}
		if !current.UpdatedAt.Equal(change.Seen.UTC()) {
			return domain.SavedView{}, domain.ErrSavedViewChanged
		}
		name, scope, filters := current.Name, current.Scope, current.Filters
		if change.Name != nil {
			name = *change.Name
		}
		if change.Filters != nil {
			filters = change.Filters
		}
		owner := current.OwnerAgentID
		if change.Scope != nil && *change.Scope != current.Scope {
			scope, owner = *change.Scope, ownerOf(*change.Scope, agentID)
			if err := checkSavedViewRoom(ctx, tx, workspaceID, scope, agentID, current.ID); err != nil {
				return domain.SavedView{}, err
			}
		}
		view, err := scanSavedView(tx.QueryRow(ctx, `UPDATE saved_views SET name=$3,scope=$4,owner_agent_id=$5,filters=$6,updated_at=$7
			WHERE workspace_id=$1 AND id=$2 RETURNING `+savedViewColumns, workspaceID, current.ID, name, scope, owner, []byte(filters), s.now()))
		return view, savedViewWriteError(err)
	})
}

// DeleteSavedView moves a view the agent can see to the trash. A view already there is returned as
// it is, so a retried delete answers the same; one past the trash period is gone.
func (s *Store) DeleteSavedView(ctx context.Context, workspaceID, agentID, id string) (domain.SavedView, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedView, error) {
		now := s.now()
		return scanSavedView(tx.QueryRow(ctx, `UPDATE saved_views SET deleted_at=COALESCE(deleted_at,$4)
			WHERE workspace_id=$1 AND `+visibleToAgent+` AND id=$3::uuid AND (deleted_at IS NULL OR deleted_at>=$5) RETURNING `+savedViewColumns,
			workspaceID, agentID, id, now, now.Add(-domain.SavedViewTrashRetention)))
	})
}

// RestoreSavedView brings back a view of the trash the agent can see. Its name or its group's room
// may have been taken meanwhile; then it stays in the trash.
func (s *Store) RestoreSavedView(ctx context.Context, workspaceID, agentID, id string) (domain.SavedView, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedView, error) {
		if err := lockSavedViews(ctx, tx, workspaceID); err != nil {
			return domain.SavedView{}, err
		}
		now := s.now()
		if err := purgeSavedViews(ctx, tx, workspaceID, now); err != nil {
			return domain.SavedView{}, err
		}
		current, err := scanSavedView(tx.QueryRow(ctx, `SELECT `+savedViewColumns+` FROM saved_views
			WHERE workspace_id=$1 AND `+visibleToAgent+` AND id=$3::uuid FOR UPDATE`, workspaceID, agentID, id))
		if err != nil || current.DeletedAt == nil {
			// A live view is already restored: a retried restore answers the same.
			return current, err
		}
		owner := ""
		if current.OwnerAgentID != nil {
			owner = *current.OwnerAgentID
		}
		if err := checkSavedViewRoom(ctx, tx, workspaceID, current.Scope, owner, current.ID); err != nil {
			return domain.SavedView{}, err
		}
		view, err := scanSavedView(tx.QueryRow(ctx, `UPDATE saved_views SET deleted_at=NULL,updated_at=$3
			WHERE workspace_id=$1 AND id=$2 RETURNING `+savedViewColumns, workspaceID, current.ID, now))
		return view, savedViewWriteError(err)
	})
}
