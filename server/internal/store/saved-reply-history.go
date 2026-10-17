package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// savedReplyChangeColumns reads a version row v with its person a; they follow the reply columns.
const savedReplyChangeColumns = `v.version,v.kind,v.restored_from_version,v.changed_by_agent_id::text,COALESCE(NULLIF(a.display_name,''),a.email),
	v.token_id::text,v.token_label,v.agent_request_id::text,v.created_at`

// savedReplyListSelect reads replies r with their newest version; trash adds who deleted them.
func savedReplyListSelect(trash bool) string {
	extra, join := ``, ``
	if trash {
		extra = `,r.deleted_at,r.deleted_by_agent_id::text,COALESCE(NULLIF(d.display_name,''),d.email)`
		join = ` LEFT JOIN agents d ON d.workspace_id=r.workspace_id AND d.id=r.deleted_by_agent_id`
	}
	return `SELECT r.id::text,r.title,r.body,r.created_by_agent_id::text,r.created_at,r.updated_at,` + savedReplyChangeColumns + extra + `
	FROM saved_replies r
	LEFT JOIN LATERAL (SELECT * FROM saved_reply_versions x WHERE x.workspace_id=r.workspace_id AND x.saved_reply_id=r.id ORDER BY x.version DESC LIMIT 1) v ON true
	LEFT JOIN agents a ON a.workspace_id=v.workspace_id AND a.id=v.changed_by_agent_id` + join
}

// changeRow receives the version columns, which are all NULL for a reply no history reaches.
type changeRow struct {
	version, restoredFrom          *int32
	kind, agentID, name            *string
	tokenID, tokenLabel, requestID *string
	createdAt                      *time.Time
}

func (c *changeRow) targets() []any {
	return []any{&c.version, &c.kind, &c.restoredFrom, &c.agentID, &c.name, &c.tokenID, &c.tokenLabel, &c.requestID, &c.createdAt}
}

func (c *changeRow) change() *domain.SavedReplyChange {
	if c.version == nil {
		return nil
	}
	change := &domain.SavedReplyChange{Version: int(*c.version), Kind: *c.kind, AgentRequestID: c.requestID, CreatedAt: domain.UTC(*c.createdAt)}
	if c.restoredFrom != nil {
		from := int(*c.restoredFrom)
		change.RestoredFromVersion = &from
	}
	change.ChangedBy = savedReplyPerson(c.agentID, c.name)
	if c.tokenLabel != nil {
		change.Token = &domain.SavedReplyToken{ID: c.tokenID, Label: *c.tokenLabel}
	}
	return change
}

func savedReplyPerson(id, name *string) *domain.SavedReplyPerson {
	if id == nil || name == nil {
		return nil
	}
	return &domain.SavedReplyPerson{AgentID: id, Name: *name}
}

// scanListedSavedReply reads a row of savedReplyListSelect; trash receives the trash columns.
func scanListedSavedReply(row pgx.Row, trash *domain.TrashedSavedReply) (domain.TrashedSavedReply, error) {
	var item domain.TrashedSavedReply
	var change changeRow
	targets := append([]any{&item.ID, &item.Title, &item.Body, &item.CreatedByAgentID, &item.CreatedAt, &item.UpdatedAt}, change.targets()...)
	var deleterID, deleterName *string
	if trash != nil {
		targets = append(targets, &item.DeletedAt, &deleterID, &deleterName)
	}
	if err := row.Scan(targets...); err != nil {
		return item, err
	}
	item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
	item.LastChange = change.change()
	if trash != nil {
		item.DeletedAt = domain.UTC(item.DeletedAt)
		item.DeletedBy = savedReplyPerson(deleterID, deleterName)
		item.PurgesAt = item.DeletedAt.Add(domain.SavedReplyTrashRetention)
	}
	return item, nil
}

// addSavedReplyVersion records the reply's current title and text as its next version. The caller
// changed the reply row in tx first, so the row lock orders concurrent versions of one reply.
func addSavedReplyVersion(ctx context.Context, tx pgx.Tx, workspaceID, replyID string, actor Actor, kind string, restoredFrom *int) (*domain.SavedReplyChange, error) {
	var change changeRow
	err := tx.QueryRow(ctx, `WITH v AS (
		INSERT INTO saved_reply_versions(workspace_id,saved_reply_id,version,kind,restored_from_version,title,body,changed_by_agent_id,token_id,token_label,agent_request_id,created_at)
		SELECT r.workspace_id,r.id,COALESCE((SELECT max(version) FROM saved_reply_versions WHERE workspace_id=$1 AND saved_reply_id=$2),0)+1,
			$3,$4,r.title,r.body,$5,t.id,t.label,$7,clock_timestamp()
		FROM saved_replies r LEFT JOIN sessions t ON t.workspace_id=r.workspace_id AND t.id=$6
		WHERE r.workspace_id=$1 AND r.id=$2
		RETURNING *)
		SELECT `+savedReplyChangeColumns+` FROM v LEFT JOIN agents a ON a.workspace_id=v.workspace_id AND a.id=v.changed_by_agent_id`,
		workspaceID, replyID, kind, restoredFrom, optionalID(actor.AgentID), optionalID(actor.TokenID), optionalID(actor.RequestID)).Scan(change.targets()...)
	if err != nil {
		return nil, fmt.Errorf("record saved reply version: %w", err)
	}
	return change.change(), nil
}

// ListTrashedSavedReplies returns the workspace's replies in the trash, most recently deleted first.
func (s *Store) ListTrashedSavedReplies(ctx context.Context, workspaceID string) ([]domain.TrashedSavedReply, error) {
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TrashedSavedReply, error) {
		rows, err := tx.Query(ctx, savedReplyListSelect(true)+` WHERE r.workspace_id=$1 AND r.deleted_at IS NOT NULL ORDER BY r.deleted_at DESC, r.id`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TrashedSavedReply, error) {
			return scanListedSavedReply(row, &domain.TrashedSavedReply{})
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list saved reply trash: %w", err)
	}
	return items, nil
}

// SavedReplyVersions returns every version of a reply, in or out of the trash, newest first.
func (s *Store) SavedReplyVersions(ctx context.Context, workspaceID, id string) ([]domain.SavedReplyVersion, error) {
	if !savedReplyID.MatchString(id) {
		return nil, domain.ErrSavedReplyNotFound
	}
	versions, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.SavedReplyVersion, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM saved_replies WHERE workspace_id=$1 AND id=$2)`, workspaceID, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, domain.ErrSavedReplyNotFound
		}
		rows, err := tx.Query(ctx, `SELECT `+savedReplyChangeColumns+`,v.title,v.body FROM saved_reply_versions v
			LEFT JOIN agents a ON a.workspace_id=v.workspace_id AND a.id=v.changed_by_agent_id
			WHERE v.workspace_id=$1 AND v.saved_reply_id=$2 ORDER BY v.version DESC`, workspaceID, id)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SavedReplyVersion, error) {
			var change changeRow
			var version domain.SavedReplyVersion
			if err := row.Scan(append(change.targets(), &version.Title, &version.Body)...); err != nil {
				return version, err
			}
			version.SavedReplyChange = *change.change()
			return version, nil
		})
	})
	if errors.Is(err, domain.ErrSavedReplyNotFound) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("list saved reply versions: %w", err)
	}
	return versions, nil
}

// SavedReplyStateOf returns a reply of the workspace as it is now, in or out of the trash.
func (s *Store) SavedReplyStateOf(ctx context.Context, workspaceID, id string) (domain.SavedReplyState, error) {
	if !savedReplyID.MatchString(id) {
		return domain.SavedReplyState{}, domain.ErrSavedReplyNotFound
	}
	state, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.SavedReplyState, error) {
		var state domain.SavedReplyState
		err := tx.QueryRow(ctx, `SELECT title,body,updated_at,deleted_at FROM saved_replies WHERE workspace_id=$1 AND id=$2`, workspaceID, id).
			Scan(&state.Title, &state.Body, &state.UpdatedAt, &state.DeletedAt)
		state.UpdatedAt, state.DeletedAt = domain.UTC(state.UpdatedAt), domain.UTCOptional(state.DeletedAt)
		return state, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SavedReplyState{}, domain.ErrSavedReplyNotFound
	}
	return state, err
}

// PurgeSavedReplyTrash deletes for good, in every workspace, the saved replies that have been in the
// trash longer than domain.SavedReplyTrashRetention, with their history. serve runs it at start and
// daily and logs how many it purged.
func (s *Store) PurgeSavedReplyTrash(ctx context.Context) (int64, error) {
	var purged int64
	err := s.db.Privileged(ctx, database.PrivilegeSavedReplyPurge, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM saved_replies WHERE deleted_at IS NOT NULL AND deleted_at<$1`, s.now().Add(-domain.SavedReplyTrashRetention))
		if err != nil {
			return fmt.Errorf("purge saved reply trash: %w", err)
		}
		purged = tag.RowsAffected()
		return nil
	})
	return purged, err
}
