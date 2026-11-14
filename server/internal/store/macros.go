package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MacroChange is a PATCH of a live macro. Seen is the updated_at the caller read.
type MacroChange struct {
	Seen    time.Time
	Title   *string
	Body    *string
	Scope   *string
	Actions *domain.MacroActions
}

const macroColumns = `id::text,title,body,scope,owner_agent_id::text,actions,created_at,updated_at,deleted_at`

// macroVisible is every read and write: a shared macro, or one of the caller's own. Another person's
// personal macro does not exist for the caller, in this workspace or any other.
const macroVisible = `(scope='shared' OR owner_agent_id=$2::uuid)`

func scanMacro(row pgx.Row) (domain.Macro, error) {
	var macro domain.Macro
	var actions []byte
	if err := row.Scan(&macro.ID, &macro.Title, &macro.Body, &macro.Scope, &macro.OwnerAgentID, &actions, &macro.CreatedAt, &macro.UpdatedAt, &macro.DeletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Macro{}, domain.ErrMacroNotFound
		}
		return domain.Macro{}, fmt.Errorf("scan macro: %w", err)
	}
	if err := json.Unmarshal(actions, &macro.Actions); err != nil {
		return domain.Macro{}, fmt.Errorf("scan macro actions: %w", err)
	}
	macro.CreatedAt, macro.UpdatedAt = domain.UTC(macro.CreatedAt), domain.UTC(macro.UpdatedAt)
	if macro.DeletedAt != nil {
		deleted := domain.UTC(*macro.DeletedAt)
		purges := deleted.Add(domain.MacroTrashRetention)
		macro.DeletedAt, macro.PurgesAt = &deleted, &purges
	}
	return macro, nil
}

func collectMacros(rows pgx.Rows) ([]domain.Macro, error) {
	defer rows.Close()
	macros := []domain.Macro{}
	for rows.Next() {
		macro, err := scanMacro(rows)
		if err != nil {
			return nil, err
		}
		macros = append(macros, macro)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate macros: %w", err)
	}
	return macros, nil
}

func lockMacros(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('macros:'||$1::text,0))`, workspaceID); err != nil {
		return fmt.Errorf("lock macros: %w", err)
	}
	return nil
}

func macroWriteError(err error) error {
	var database *pgconn.PgError
	if errors.As(err, &database) && database.Code == "23505" {
		return domain.ErrMacroTitleTaken
	}
	return err
}

func checkMacroRoom(ctx context.Context, tx pgx.Tx, workspaceID, scope, ownerID, exceptID string) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM macros WHERE workspace_id=$1 AND deleted_at IS NULL AND scope=$2
		AND ($2='shared' OR owner_agent_id=NULLIF($3,'')::uuid) AND id::text<>$4`, workspaceID, scope, ownerID, exceptID).Scan(&count); err != nil {
		return fmt.Errorf("count macros: %w", err)
	}
	limit := domain.MaxMacrosShared
	if scope == domain.MacroPersonal {
		limit = domain.MaxMacrosPersonal
	}
	if count >= limit {
		return domain.ErrMacroLimit
	}
	return nil
}

func purgeMacros(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `DELETE FROM macros WHERE workspace_id=$1 AND deleted_at IS NOT NULL AND deleted_at<$2`, workspaceID, now.Add(-domain.MacroTrashRetention)); err != nil {
		return fmt.Errorf("purge macros: %w", err)
	}
	return nil
}

// ListMacros returns the shared macros, then the agent's personal ones, each by title ignoring case.
func (s *Store) ListMacros(ctx context.Context, workspaceID, agentID string) ([]domain.Macro, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Macro, error) {
		rows, err := tx.Query(ctx, `SELECT `+macroColumns+` FROM macros
			WHERE workspace_id=$1 AND `+macroVisible+` AND deleted_at IS NULL
			ORDER BY scope='personal',lower(title),id`, workspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("list macros: %w", err)
		}
		return collectMacros(rows)
	})
}

// ListDeletedMacros returns the macros the agent could see that were deleted in the last 30 days.
func (s *Store) ListDeletedMacros(ctx context.Context, workspaceID, agentID string) ([]domain.Macro, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Macro, error) {
		if err := purgeMacros(ctx, tx, workspaceID, s.now()); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT `+macroColumns+` FROM macros
			WHERE workspace_id=$1 AND `+macroVisible+` AND deleted_at IS NOT NULL
			ORDER BY deleted_at DESC,id`, workspaceID, agentID)
		if err != nil {
			return nil, fmt.Errorf("list deleted macros: %w", err)
		}
		return collectMacros(rows)
	})
}

// CreateMacro adds a macro the agent can see. The title is unique ignoring case in its group.
func (s *Store) CreateMacro(ctx context.Context, workspaceID, agentID, title, body, scope string, actions domain.MacroActions) (domain.Macro, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Macro, error) {
		if err := lockMacros(ctx, tx, workspaceID); err != nil {
			return domain.Macro{}, err
		}
		if err := macroTagsAndAssignee(ctx, tx, workspaceID, actions); err != nil {
			return domain.Macro{}, err
		}
		if err := checkMacroRoom(ctx, tx, workspaceID, scope, agentID, ""); err != nil {
			return domain.Macro{}, err
		}
		encoded, err := json.Marshal(actions)
		if err != nil {
			return domain.Macro{}, err
		}
		now := s.now()
		macro, err := scanMacro(tx.QueryRow(ctx, `INSERT INTO macros(workspace_id,scope,owner_agent_id,title,body,actions,created_by_agent_id,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8) RETURNING `+macroColumns,
			workspaceID, scope, ownerOf(scope, agentID), title, body, encoded, agentID, now))
		return macro, macroWriteError(err)
	})
}

// UpdateMacro changes a live macro the agent can see. Only its owner sees a personal macro.
func (s *Store) UpdateMacro(ctx context.Context, workspaceID, agentID, id string, change MacroChange) (domain.Macro, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Macro, error) {
		if err := lockMacros(ctx, tx, workspaceID); err != nil {
			return domain.Macro{}, err
		}
		current, err := scanMacro(tx.QueryRow(ctx, `SELECT `+macroColumns+` FROM macros
			WHERE workspace_id=$1 AND `+macroVisible+` AND id=$3::uuid AND deleted_at IS NULL FOR UPDATE`, workspaceID, agentID, id))
		if err != nil {
			return domain.Macro{}, err
		}
		if !current.UpdatedAt.Equal(change.Seen.UTC()) {
			return domain.Macro{}, domain.ErrMacroChanged
		}
		title, body, scope, actions := current.Title, current.Body, current.Scope, current.Actions
		if change.Title != nil {
			title = *change.Title
		}
		if change.Body != nil {
			body = *change.Body
		}
		if change.Actions != nil {
			actions = *change.Actions
		}
		if err := macroTagsAndAssignee(ctx, tx, workspaceID, actions); err != nil {
			return domain.Macro{}, err
		}
		owner := current.OwnerAgentID
		if change.Scope != nil && *change.Scope != current.Scope {
			scope, owner = *change.Scope, ownerOf(*change.Scope, agentID)
			if err := checkMacroRoom(ctx, tx, workspaceID, scope, agentID, current.ID); err != nil {
				return domain.Macro{}, err
			}
		}
		encoded, err := json.Marshal(actions)
		if err != nil {
			return domain.Macro{}, err
		}
		macro, err := scanMacro(tx.QueryRow(ctx, `UPDATE macros SET title=$3,body=$4,scope=$5,owner_agent_id=$6,actions=$7,updated_at=$8
			WHERE workspace_id=$1 AND id=$2 RETURNING `+macroColumns, workspaceID, current.ID, title, body, scope, owner, encoded, s.now()))
		return macro, macroWriteError(err)
	})
}

// DeleteMacro moves a macro the agent can see to the trash. One already there is returned as it is.
func (s *Store) DeleteMacro(ctx context.Context, workspaceID, agentID, id string, seen time.Time) (domain.Macro, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Macro, error) {
		current, err := scanMacro(tx.QueryRow(ctx, `SELECT `+macroColumns+` FROM macros
			WHERE workspace_id=$1 AND `+macroVisible+` AND id=$3::uuid FOR UPDATE`, workspaceID, agentID, id))
		if err != nil || current.DeletedAt != nil {
			return current, err
		}
		if !current.UpdatedAt.Equal(seen.UTC()) {
			return domain.Macro{}, domain.ErrMacroChanged
		}
		now := s.now()
		return scanMacro(tx.QueryRow(ctx, `UPDATE macros SET deleted_at=$3,deleted_by_agent_id=$4
			WHERE workspace_id=$1 AND id=$2 RETURNING `+macroColumns, workspaceID, current.ID, now, agentID))
	})
}

// RestoreMacro brings a macro out of the trash. A live one is answered unchanged.
func (s *Store) RestoreMacro(ctx context.Context, workspaceID, agentID, id string) (domain.Macro, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Macro, error) {
		if err := lockMacros(ctx, tx, workspaceID); err != nil {
			return domain.Macro{}, err
		}
		now := s.now()
		if err := purgeMacros(ctx, tx, workspaceID, now); err != nil {
			return domain.Macro{}, err
		}
		current, err := scanMacro(tx.QueryRow(ctx, `SELECT `+macroColumns+` FROM macros
			WHERE workspace_id=$1 AND `+macroVisible+` AND id=$3::uuid FOR UPDATE`, workspaceID, agentID, id))
		if err != nil || current.DeletedAt == nil {
			return current, err
		}
		owner := ""
		if current.OwnerAgentID != nil {
			owner = *current.OwnerAgentID
		}
		if err := checkMacroRoom(ctx, tx, workspaceID, current.Scope, owner, current.ID); err != nil {
			return domain.Macro{}, err
		}
		macro, err := scanMacro(tx.QueryRow(ctx, `UPDATE macros SET deleted_at=NULL,deleted_by_agent_id=NULL,updated_at=$3
			WHERE workspace_id=$1 AND id=$2 RETURNING `+macroColumns, workspaceID, current.ID, now))
		return macro, macroWriteError(err)
	})
}

func macroTagsAndAssignee(ctx context.Context, tx pgx.Tx, workspaceID string, actions domain.MacroActions) error {
	ids := append(append([]string{}, actions.AddTagIDs...), actions.RemoveTagIDs...)
	if len(ids) > 0 {
		var found int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tags WHERE workspace_id=$1 AND id=ANY($2::uuid[])`, workspaceID, ids).Scan(&found); err != nil {
			return fmt.Errorf("check macro tags: %w", err)
		}
		if found != len(ids) {
			return domain.ErrMacroTagUnknown
		}
	}
	if actions.SetAssignee && actions.AssigneeID != nil {
		if err := ValidateAssignee(ctx, tx, workspaceID, *actions.AssigneeID); err != nil {
			return domain.ErrMacroAssignee
		}
	}
	return nil
}

// macroSnap is the conversation fields a macro touched, before and after, so undo can put back only
// what is still as the macro left it.
type macroSnap struct {
	Status        string   `json:"status,omitempty"`
	Priority      string   `json:"priority,omitempty"`
	AssigneeID    *string  `json:"assignee_id,omitempty"`
	ClearAssignee bool     `json:"clear_assignee,omitempty"`
	TagIDs        []string `json:"tag_ids,omitempty"`
	Touched       []string `json:"touched"`
}

// MacroApplication is one apply, with the rendered text and what undo put back.
type MacroApplication struct {
	ID           string              `json:"id"`
	MacroID      *string             `json:"macro_id"`
	Title        string              `json:"title"`
	Body         string              `json:"body"`
	CreatedAt    time.Time           `json:"created_at"`
	UndoneAt     *time.Time          `json:"undone_at"`
	Restored     []string            `json:"restored"`
	Kept         []string            `json:"kept"`
	Conversation domain.Conversation `json:"-"`
}

// MacroResult is an apply or an undo: the application and the conversation as it stands after.
type MacroResult struct {
	Application  MacroApplication    `json:"application"`
	Conversation domain.Conversation `json:"conversation"`
}

// ApplyMacro fills the macro's text and applies its actions to the conversation in one step. A status
// it sets does not tell the customer; the reply is still in the composer's hands. The application is
// what undo reads.
func (s *Store) ApplyMacro(ctx context.Context, workspaceID, agentID, tokenID, conversationID, macroID string) (MacroResult, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MacroResult, error) {
		macro, err := scanMacro(tx.QueryRow(ctx, `SELECT `+macroColumns+` FROM macros
			WHERE workspace_id=$1 AND `+macroVisible+` AND id=$3::uuid AND deleted_at IS NULL`, workspaceID, agentID, macroID))
		if err != nil {
			return MacroResult{}, err
		}
		if err := macroTagsAndAssignee(ctx, tx, workspaceID, macro.Actions); err != nil {
			return MacroResult{}, err
		}
		current, err := getConversation(ctx, tx, workspaceID, conversationID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return MacroResult{}, domain.ErrMacroConversationGone
			}
			return MacroResult{}, err
		}
		names, err := macroNames(ctx, tx, workspaceID, agentID)
		if err != nil {
			return MacroResult{}, err
		}
		body := domain.RenderMacro(macro.Body, domain.MacroContext{CustomerName: current.ContactName, AgentName: names, TicketID: current.DisplayRef})
		before, applied, update := macroUpdate(current, macro.Actions, agentID, tokenID)
		var item domain.Conversation
		if len(applied.Touched) == 0 {
			item = current
		} else {
			item, err = UpdateConversationIn(ctx, tx, workspaceID, conversationID, update)
			if err != nil {
				return MacroResult{}, err
			}
		}
		beforeJSON, err := json.Marshal(before)
		if err != nil {
			return MacroResult{}, err
		}
		appliedJSON, err := json.Marshal(applied)
		if err != nil {
			return MacroResult{}, err
		}
		var applicationID string
		var created time.Time
		if err := tx.QueryRow(ctx, `INSERT INTO macro_applications(workspace_id,conversation_id,macro_id,macro_title,agent_id,rendered_body,before,applied,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text,created_at`,
			workspaceID, conversationID, macro.ID, macro.Title, agentID, body, beforeJSON, appliedJSON, s.now()).Scan(&applicationID, &created); err != nil {
			return MacroResult{}, fmt.Errorf("record macro application: %w", err)
		}
		item, err = s.finishConversation(ctx, tx, workspaceID, item)
		if err != nil {
			return MacroResult{}, err
		}
		return MacroResult{Application: MacroApplication{
			ID: applicationID, MacroID: &macro.ID, Title: macro.Title, Body: body, CreatedAt: domain.UTC(created), Restored: []string{}, Kept: []string{},
		}, Conversation: item}, nil
	})
}

// UndoMacro puts back the fields the application changed, where they are still as the macro left
// them. A field changed again since is left (kept). Undoing twice changes nothing more.
func (s *Store) UndoMacro(ctx context.Context, workspaceID, agentID, tokenID, conversationID, applicationID string) (MacroResult, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MacroResult, error) {
		var macroID *string
		var title, body string
		var beforeJSON, appliedJSON []byte
		var created time.Time
		var undoneAt *time.Time
		err := tx.QueryRow(ctx, `SELECT macro_id::text,macro_title,rendered_body,before,applied,created_at,undone_at FROM macro_applications
			WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE`, workspaceID, conversationID, applicationID).
			Scan(&macroID, &title, &body, &beforeJSON, &appliedJSON, &created, &undoneAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return MacroResult{}, domain.ErrMacroApplicationGone
		}
		if err != nil {
			return MacroResult{}, fmt.Errorf("read macro application: %w", err)
		}
		current, err := getConversation(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return MacroResult{}, err
		}
		application := MacroApplication{ID: applicationID, MacroID: macroID, Title: title, Body: body, CreatedAt: domain.UTC(created), Restored: []string{}, Kept: []string{}}
		if undoneAt != nil {
			undone := domain.UTC(*undoneAt)
			application.UndoneAt = &undone
			item, err := s.finishConversation(ctx, tx, workspaceID, current)
			return MacroResult{Application: application, Conversation: item}, err
		}
		var before, applied macroSnap
		if err := json.Unmarshal(beforeJSON, &before); err != nil || json.Unmarshal(appliedJSON, &applied) != nil {
			return MacroResult{}, fmt.Errorf("read macro application snapshot: %w", err)
		}
		update := ConversationUpdate{ActorID: agentID, TokenID: tokenID, Quiet: true}
		for _, field := range applied.Touched {
			switch field {
			case "status":
				if current.Status == applied.Status {
					update.Status = &before.Status
					application.Restored = append(application.Restored, "status")
				} else {
					application.Kept = append(application.Kept, "status")
				}
			case "priority":
				if current.Priority == applied.Priority {
					update.Priority = &before.Priority
					application.Restored = append(application.Restored, "priority")
				} else {
					application.Kept = append(application.Kept, "priority")
				}
			case "assignee":
				if sameAssignee(current.AssigneeID, applied) {
					var id *string
					if !before.ClearAssignee {
						copied := *before.AssigneeID
						id = &copied
					}
					update.AssigneeID = &id
					application.Restored = append(application.Restored, "assignee")
				} else {
					application.Kept = append(application.Kept, "assignee")
				}
			case "tags":
				if sameIDs(tagIDs(current), applied.TagIDs) {
					ids := append([]string{}, before.TagIDs...)
					update.TagIDs = &ids
					application.Restored = append(application.Restored, "tags")
				} else {
					application.Kept = append(application.Kept, "tags")
				}
			}
		}
		item := current
		if update.Status != nil || update.Priority != nil || update.AssigneeID != nil || update.TagIDs != nil {
			item, err = UpdateConversationIn(ctx, tx, workspaceID, conversationID, update)
			if err != nil {
				return MacroResult{}, err
			}
		}
		now := s.now()
		if _, err := tx.Exec(ctx, `UPDATE macro_applications SET undone_at=$3,undone_by_agent_id=$4 WHERE workspace_id=$1 AND id=$2`, workspaceID, applicationID, now, agentID); err != nil {
			return MacroResult{}, fmt.Errorf("undo macro: %w", err)
		}
		undone := domain.UTC(now)
		application.UndoneAt = &undone
		item, err = s.finishConversation(ctx, tx, workspaceID, item)
		return MacroResult{Application: application, Conversation: item}, err
	})
}

func (s *Store) finishConversation(ctx context.Context, tx pgx.Tx, workspaceID string, item domain.Conversation) (domain.Conversation, error) {
	item, err := s.withChannelRef(ctx, tx, workspaceID)(item, nil)
	if err != nil {
		return item, err
	}
	return item, s.attachStatusNotices(ctx, tx, workspaceID, &item)
}

func macroNames(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) (string, error) {
	var display, email string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(display_name,''),email FROM agents WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID).Scan(&display, &email); err != nil {
		return "", fmt.Errorf("read agent name: %w", err)
	}
	return domain.AgentSignatureName(display, email), nil
}

func macroUpdate(current domain.Conversation, actions domain.MacroActions, agentID, tokenID string) (before, applied macroSnap, update ConversationUpdate) {
	update = ConversationUpdate{ActorID: agentID, TokenID: tokenID, Quiet: true}
	before.Touched, applied.Touched = []string{}, []string{}
	if actions.Status != nil && *actions.Status != current.Status {
		before.Status, applied.Status = current.Status, *actions.Status
		update.Status = actions.Status
		before.Touched = append(before.Touched, "status")
		applied.Touched = append(applied.Touched, "status")
	}
	if actions.Priority != nil && *actions.Priority != current.Priority {
		before.Priority, applied.Priority = current.Priority, *actions.Priority
		update.Priority = actions.Priority
		before.Touched = append(before.Touched, "priority")
		applied.Touched = append(applied.Touched, "priority")
	}
	if actions.SetAssignee && !sameString(current.AssigneeID, actions.AssigneeID) {
		if current.AssigneeID == nil {
			before.ClearAssignee = true
		} else {
			before.AssigneeID = current.AssigneeID
		}
		if actions.AssigneeID == nil {
			applied.ClearAssignee = true
		} else {
			applied.AssigneeID = actions.AssigneeID
		}
		id := actions.AssigneeID
		update.AssigneeID = &id
		before.Touched = append(before.Touched, "assignee")
		applied.Touched = append(applied.Touched, "assignee")
	}
	if len(actions.AddTagIDs) > 0 || len(actions.RemoveTagIDs) > 0 {
		next := tagIDs(current)
		remove := map[string]bool{}
		for _, id := range actions.RemoveTagIDs {
			remove[id] = true
		}
		kept := make([]string, 0, len(next))
		for _, id := range next {
			if !remove[id] {
				kept = append(kept, id)
			}
		}
		have := map[string]bool{}
		for _, id := range kept {
			have[id] = true
		}
		for _, id := range actions.AddTagIDs {
			if !have[id] {
				kept = append(kept, id)
				have[id] = true
			}
		}
		if !sameIDs(next, kept) {
			before.TagIDs, applied.TagIDs = next, kept
			update.TagIDs = &kept
			before.Touched = append(before.Touched, "tags")
			applied.Touched = append(applied.Touched, "tags")
		}
	}
	return before, applied, update
}

func tagIDs(conversation domain.Conversation) []string {
	ids := make([]string, 0, len(conversation.Tags))
	for _, tag := range conversation.Tags {
		ids = append(ids, tag.ID)
	}
	return ids
}

func sameIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string{}, left...), append([]string{}, right...)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func sameString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameAssignee(current *string, applied macroSnap) bool {
	if applied.ClearAssignee {
		return current == nil
	}
	return sameString(current, applied.AssigneeID)
}
