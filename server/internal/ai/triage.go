package ai

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrTriageNotFound means no suggestion with that id on that conversation of this workspace.
	ErrTriageNotFound = errors.New("no AI triage suggestion with that id on this conversation")
	// ErrTriageDecided means the suggestion was already accepted, dismissed or replaced, or is not ready.
	ErrTriageDecided = errors.New("this AI triage suggestion is not ready or was already accepted, dismissed or replaced")
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
	(SELECT a.id::text FROM agents a WHERE a.workspace_id=s.workspace_id AND a.id=s.assignee_agent_id AND a.deactivated_at IS NULL),s.reason,s.sources,s.stale,s.model,s.outcome,s.applied,s.created_at,s.ready_at,s.decided_at,s.decided_by_agent_id::text
	FROM ai_triage_suggestions s LEFT JOIN ai_tasks t ON t.workspace_id=s.workspace_id AND t.id=s.task_id`

func scanTriage(row pgx.Row) (Triage, error) {
	var t Triage
	err := row.Scan(&t.ID, &t.ConversationID, &t.Status, &t.ErrorCode, &t.Automatic, &t.TagIDs, &t.Priority, &t.AssigneeAgentID, &t.Reason, &t.Sources, &t.Stale, &t.Model,
		&t.Outcome, &t.Applied, &t.CreatedAt, &t.ReadyAt, &t.DecidedAt, &t.DecidedByAgentID)
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
	if t.Applied.TagIDs == nil {
		t.Applied.TagIDs = []string{}
	}
	t.CreatedAt, t.ReadyAt, t.DecidedAt = t.CreatedAt.UTC(), utcPointer(t.ReadyAt), utcPointer(t.DecidedAt)
	return t, nil
}

// LatestTriage reads the conversation's newest suggestion; nil when it has none.
func (s *Service) LatestTriage(ctx context.Context, workspaceID, conversationID string) (*Triage, error) {
	if !isUUID(conversationID) {
		return nil, ErrConversationNotFound
	}
	var found *Triage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := conversationExists(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 ORDER BY s.created_at DESC, s.id LIMIT 1`,
			workspaceID, conversationID))
		if errors.Is(err, ErrTriageNotFound) {
			return nil
		}
		found = &t
		return err
	})
	return found, err
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

// TriageToApply checks what a person accepts against the open suggestion: only its own tags, its
// priority and its assignee. It returns the part not applied yet; the caller applies that to the
// conversation, then calls AcceptedTriage.
func (s *Service) TriageToApply(ctx context.Context, workspaceID, conversationID, id string, want TriageSet) (TriageSet, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return TriageSet{}, ErrTriageNotFound
	}
	var set TriageSet
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 AND s.id=$3`, workspaceID, conversationID, id))
		if err != nil {
			return err
		}
		if t.Status != DraftReady || t.Outcome != nil {
			return ErrTriageDecided
		}
		set.TagIDs = []string{}
		for _, tag := range want.TagIDs {
			if !slices.Contains(t.TagIDs, tag) {
				return invalid("Accept only the tags the suggestion names.")
			}
			if !slices.Contains(set.TagIDs, tag) && !slices.Contains(t.Applied.TagIDs, tag) {
				set.TagIDs = append(set.TagIDs, tag)
			}
		}
		if want.Priority != nil {
			if t.Priority == nil || *want.Priority != *t.Priority {
				return invalid("Accept only the priority the suggestion names.")
			}
			if t.Applied.Priority == nil {
				set.Priority = t.Priority
			}
		}
		if want.AssigneeAgentID != nil {
			if t.AssigneeAgentID == nil || *want.AssigneeAgentID != *t.AssigneeAgentID {
				return invalid("Accept only the assignee the suggestion names.")
			}
			if t.Applied.AssigneeAgentID == nil {
				set.AssigneeAgentID = t.AssigneeAgentID
			}
		}
		return nil
	})
	return set, err
}

// AcceptedTriage records a part a person applied. The suggestion stays open for the rest until all of
// it is applied (accepted), or the rest is dismissed, replaced or expires.
func (s *Service) AcceptedTriage(ctx context.Context, workspaceID string, actor Actor, conversationID, id string, applied TriageSet) (Triage, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Triage{}, ErrTriageNotFound
	}
	var triage Triage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		// Under the row's lock, so two people accepting two parts at once both count.
		t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 AND s.id=$3 FOR UPDATE OF s`, workspaceID, conversationID, id))
		if err != nil {
			return err
		}
		if t.Status != DraftReady || t.Outcome != nil {
			return ErrTriageDecided
		}
		if len(t.TagIDs) == 0 && t.Priority == nil && t.AssigneeAgentID == nil {
			return ErrTriageNothing
		}
		merged := t.Applied
		for _, tag := range applied.TagIDs {
			if !slices.Contains(merged.TagIDs, tag) {
				merged.TagIDs = append(merged.TagIDs, tag)
			}
		}
		if applied.Priority != nil {
			merged.Priority = applied.Priority
		}
		if applied.AssigneeAgentID != nil {
			merged.AssigneeAgentID = applied.AssigneeAgentID
		}
		all := (t.Priority == nil || merged.Priority != nil) && (t.AssigneeAgentID == nil || merged.AssigneeAgentID != nil)
		for _, tag := range t.TagIDs {
			all = all && slices.Contains(merged.TagIDs, tag)
		}
		var outcome, decidedAt, decidedBy any
		if all {
			outcome, decidedAt, decidedBy = "accepted", s.now().UTC(), nullable(actor.AgentID)
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET applied=$3,outcome=$4,decided_at=$5,decided_by_agent_id=$6 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, merged, outcome, decidedAt, decidedBy); err != nil {
			return err
		}
		triage, err = scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, id))
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
