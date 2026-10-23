package ai

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrTriageNotFound means no suggestion with that id on that conversation of this workspace.
	ErrTriageNotFound = errors.New("no AI triage suggestion with that id on this conversation")
	// ErrTriageDecided means the suggestion was already accepted, dismissed or replaced, or is not ready.
	ErrTriageDecided = errors.New("this AI triage suggestion is not ready or was already accepted, dismissed or replaced")
	// ErrTriageNotAutoApplied means AI applied nothing of the suggestion, so there is nothing to undo.
	ErrTriageNotAutoApplied = errors.New("AI applied nothing of this suggestion by itself")
	// ErrTriageNothing means the suggestion names nothing to apply.
	ErrTriageNothing = errors.New("this AI triage suggestion names nothing to apply")
)

// triageAppliedSome is true for a suggestion of which a person applied a part.
const triageAppliedSome = `(jsonb_array_length(COALESCE(applied->'tag_ids','[]'))>0 OR jsonb_typeof(applied->'priority')='string'
	OR jsonb_typeof(applied->'assignee_agent_id')='string')`

// triageBase reads what the suggestion compares against when its answer comes: the conversation's
// priority and assignee now.
const triageBase = `(SELECT jsonb_build_object('priority',c.priority,'assignee_agent_id',c.assignee_id) FROM conversations c WHERE c.workspace_id=$1 AND c.id=$2)`

// triageExpiry is how long a ready suggestion waits for a decision.
const triageExpiry = 7 * 24 * time.Hour

// Triage is one AI triage suggestion: tags, a priority and an assignee for a conversation.
type Triage struct {
	ID               string        `json:"id"`
	ConversationID   string        `json:"conversation_id"`
	Status           string        `json:"status"`
	ErrorCode        *string       `json:"error_code"`
	Automatic        bool          `json:"automatic"`
	TagIDs           []string      `json:"tag_ids"`
	Priority         *string       `json:"priority"`
	AssigneeAgentID  *string       `json:"assignee_agent_id"`
	Reason           *string       `json:"reason"`
	Sources          []DraftSource `json:"sources"`
	Stale            TriageStale   `json:"stale"`
	Model            *string       `json:"model"`
	Outcome          *string       `json:"outcome"`
	Applied          TriageSet     `json:"applied"`
	CreatedAt        time.Time     `json:"created_at"`
	ReadyAt          *time.Time    `json:"ready_at"`
	DecidedAt        *time.Time    `json:"decided_at"`
	DecidedByAgentID *string       `json:"decided_by_agent_id"`
	// What AI applied by itself (automatic apply), what it left because a person had set it, and an
	// undo: what it took back and what it kept because a person changed it after AI.
	AutoAppliedAt   *time.Time            `json:"auto_applied_at"`
	AutoApplied     TriageSet             `json:"auto_applied"`
	Skipped         map[string]TriageSkip `json:"skipped"`
	UndoneParts     TriageSet             `json:"undone_parts"`
	Kept            TriageStale           `json:"kept"`
	UndoneAt        *time.Time            `json:"undone_at"`
	UndoneByAgentID *string               `json:"undone_by_agent_id"`
}

// TriageSkip is a part AI did not apply by itself, and why: set_by_person (a person had set the
// priority), agent_conversation (a teammate started it), already_assigned. Value is what the
// conversation had.
type TriageSkip struct {
	Reason string  `json:"reason"`
	Value  *string `json:"value"`
}

// TriageStale is what a person set on the conversation before the suggestion came, in place of a
// part it no longer suggests.
type TriageStale struct {
	Priority        *string `json:"priority"`
	AssigneeAgentID *string `json:"assignee_agent_id"`
}

// TriageSet is what of a suggestion a person applies.
type TriageSet struct {
	TagIDs          []string `json:"tag_ids"`
	Priority        *string  `json:"priority"`
	AssigneeAgentID *string  `json:"assignee_agent_id"`
}

// triageColumns read a suggestion with the state its task gives it, as draftColumns do. A task the
// worker stopped (the automatic limit, the feature turned off) reads as failed with its reason; only a
// person's stop reads as cancelled. A tag deleted or a teammate deactivated since is no longer
// suggested, so accepting the rest never fails on it.
const triageColumns = `s.id::text,s.conversation_id::text,
	CASE WHEN s.status<>'pending' THEN s.status
		WHEN t.state IN ('queued','running') THEN t.state
		WHEN t.state='cancelled' AND t.error_code='` + CodeCancelledByPerson + `' THEN 'cancelled' ELSE 'failed' END,
	CASE WHEN s.status='pending' AND t.id IS NULL THEN 'restored'
		WHEN s.status='pending' AND t.state='done' THEN 'internal'
		WHEN s.status='pending' AND t.state IN ('failed','cancelled') THEN t.error_code END,
	s.automatic,
	ARRAY(SELECT x.id::text FROM unnest(s.tag_ids) WITH ORDINALITY x(id,n) WHERE EXISTS (SELECT 1 FROM tags g WHERE g.workspace_id=s.workspace_id AND g.id=x.id) ORDER BY x.n),
	s.priority,
	(SELECT a.id::text FROM agents a WHERE a.workspace_id=s.workspace_id AND a.id=s.assignee_agent_id AND a.deactivated_at IS NULL),s.reason,s.sources,s.stale,s.model,s.outcome,s.applied,s.created_at,s.ready_at,s.decided_at,s.decided_by_agent_id::text,
	s.auto_applied_at,s.auto_applied,s.skipped,s.undone_parts,s.kept,s.undone_at,s.undone_by_agent_id::text
	FROM ai_triage_suggestions s LEFT JOIN ai_tasks t ON t.workspace_id=s.workspace_id AND t.id=s.task_id`

func scanTriage(row pgx.Row) (Triage, error) {
	var t Triage
	err := row.Scan(&t.ID, &t.ConversationID, &t.Status, &t.ErrorCode, &t.Automatic, &t.TagIDs, &t.Priority, &t.AssigneeAgentID, &t.Reason, &t.Sources, &t.Stale, &t.Model,
		&t.Outcome, &t.Applied, &t.CreatedAt, &t.ReadyAt, &t.DecidedAt, &t.DecidedByAgentID,
		&t.AutoAppliedAt, &t.AutoApplied, &t.Skipped, &t.UndoneParts, &t.Kept, &t.UndoneAt, &t.UndoneByAgentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Triage{}, ErrTriageNotFound
	}
	if err != nil {
		return Triage{}, err
	}
	if t.TagIDs == nil {
		t.TagIDs = []string{}
	}
	if t.Sources == nil {
		t.Sources = []DraftSource{}
	}
	for _, set := range []*TriageSet{&t.Applied, &t.AutoApplied, &t.UndoneParts} {
		if set.TagIDs == nil {
			set.TagIDs = []string{}
		}
	}
	if t.Skipped == nil {
		t.Skipped = map[string]TriageSkip{}
	}
	t.CreatedAt, t.ReadyAt, t.DecidedAt = t.CreatedAt.UTC(), utcPointer(t.ReadyAt), utcPointer(t.DecidedAt)
	t.AutoAppliedAt, t.UndoneAt = utcPointer(t.AutoAppliedAt), utcPointer(t.UndoneAt)
	return t, nil
}

// LatestTriage reads the conversation's newest suggestion, and the newest one AI applied by itself
// (the same one, or an older one a suggestion asked for by hand came after); nil when there is none.
func (s *Service) LatestTriage(ctx context.Context, workspaceID, conversationID string) (latest, autoApplied *Triage, err error) {
	if !isUUID(conversationID) {
		return nil, nil, ErrConversationNotFound
	}
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := conversationExists(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		read := func(query string) (*Triage, error) {
			t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 `+query, workspaceID, conversationID))
			if errors.Is(err, ErrTriageNotFound) {
				return nil, nil
			}
			return &t, err
		}
		var err error
		if latest, err = read(`ORDER BY s.created_at DESC, s.id LIMIT 1`); err != nil {
			return err
		}
		autoApplied, err = read(`AND s.auto_applied_at IS NOT NULL ORDER BY s.auto_applied_at DESC, s.id LIMIT 1`)
		return err
	})
	return latest, autoApplied, err
}

// CreateTriage asks for a suggestion by hand (not automatic: no automatic limit applies). It refuses at
// once what could never run, like CreateDraft, and replaces the conversation's open suggestion.
func (s *Service) CreateTriage(ctx context.Context, workspaceID string, actor Actor, conversationID string) (Triage, error) {
	if !isUUID(conversationID) {
		return Triage{}, ErrConversationNotFound
	}
	binding, err := s.Resolve(ctx, workspaceID, SlotTriage)
	if err != nil {
		return Triage{}, err
	}
	now := s.now().UTC()
	var triage Triage
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := conversationExists(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		if binding.call.BilledTo == BilledToServer {
			left, _, limited, err := s.allowanceLeft(ctx, tx, workspaceID)
			if err != nil {
				return err
			}
			if limited && left <= 0 {
				return ErrAllowanceReached
			}
		}
		if err := s.underMinuteCap(ctx, tx, workspaceID, now); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `UPDATE ai_triage_suggestions SET status='cancelled' WHERE workspace_id=$1 AND conversation_id=$2 AND status='pending' RETURNING task_id::text`,
			workspaceID, conversationID)
		if err != nil {
			return err
		}
		tasks, err := pgx.CollectRows(rows, pgx.RowTo[*string])
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if task != nil {
				if _, err := s.CancelTask(ctx, tx, workspaceID, *task); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET outcome=CASE WHEN `+triageAppliedSome+` THEN 'partly_accepted' ELSE 'superseded' END,
			decided_at=$3,decided_by_agent_id=$4
			WHERE workspace_id=$1 AND conversation_id=$2 AND status='ready' AND outcome IS NULL`, workspaceID, conversationID, now, nullable(actor.AgentID)); err != nil {
			return err
		}
		taskID, err := s.Enqueue(ctx, tx, workspaceID, NewTask{Kind: SlotTriage, ConversationID: conversationID, Actor: actor})
		if err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO ai_triage_suggestions(workspace_id,conversation_id,task_id,requested_by_agent_id,requested_by_token_id,status,created_at,base)
			VALUES($1,$2,$3,$4,$5,'pending',$6,`+triageBase+`) RETURNING id::text`, workspaceID, conversationID, taskID, nullable(actor.AgentID), nullable(actor.TokenID), now).Scan(&id); err != nil {
			return err
		}
		triage, err = scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, id))
		return err
	})
	return triage, err
}

// underMinuteCap counts the drafts and suggestions a workspace asked for by hand in the last minute
// (draftsPerMinute together), under one advisory lock.
func (s *Service) underMinuteCap(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-drafts:"+workspaceID); err != nil {
		return err
	}
	var recent int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM ai_drafts WHERE workspace_id=$1 AND created_at>$2)
		+ (SELECT count(*) FROM ai_triage_suggestions WHERE workspace_id=$1 AND NOT automatic AND created_at>$2)`, workspaceID, now.Add(-time.Minute)).Scan(&recent); err != nil {
		return err
	}
	if recent >= draftsPerMinute {
		return ErrDraftsRateLimited
	}
	return nil
}

// GetTriage reads one suggestion of the conversation.
func (s *Service) GetTriage(ctx context.Context, workspaceID, conversationID, id string) (Triage, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Triage{}, ErrTriageNotFound
	}
	var triage Triage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		triage, err = scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 AND s.id=$3`, workspaceID, conversationID, id))
		return err
	})
	return triage, err
}

// DismissTriage stops a suggestion still being written, or closes an open one: dismissed, or partly
// accepted when a part of it was applied. Dismissing again changes nothing.
func (s *Service) DismissTriage(ctx context.Context, workspaceID string, actor Actor, conversationID, id string) (Triage, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Triage{}, ErrTriageNotFound
	}
	var triage Triage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 AND s.id=$3 FOR UPDATE OF s`, workspaceID, conversationID, id))
		if err != nil {
			return err
		}
		switch {
		case t.Status == DraftCancelled || (t.Outcome != nil && *t.Outcome == "dismissed"):
			triage = t
			return nil
		case t.Status == DraftQueued || t.Status == DraftRunning:
			var task *string
			if err := tx.QueryRow(ctx, `UPDATE ai_triage_suggestions SET status='cancelled' WHERE workspace_id=$1 AND id=$2 RETURNING task_id::text`, workspaceID, id).Scan(&task); err != nil {
				return err
			}
			if task != nil {
				if _, err := s.CancelTask(ctx, tx, workspaceID, *task); err != nil {
					return err
				}
			}
		case t.Status != DraftReady || t.Outcome != nil:
			return ErrTriageDecided
		default:
			if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET outcome=CASE WHEN `+triageAppliedSome+` THEN 'partly_accepted' ELSE 'dismissed' END,
				decided_at=$3,decided_by_agent_id=$4 WHERE workspace_id=$1 AND id=$2`, workspaceID, id, s.now().UTC(), nullable(actor.AgentID)); err != nil {
				return err
			}
		}
		triage, err = scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, id))
		return err
	})
	return triage, err
}
