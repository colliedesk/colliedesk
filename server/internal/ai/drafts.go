package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Draft states as people see them. A pending draft shows its task's state.
const (
	DraftQueued    = "queued"
	DraftRunning   = "running"
	DraftReady     = "ready"
	DraftFailed    = "failed"
	DraftCancelled = "cancelled"
)

const (
	maxPromptRunes = 2000
	// draftsPerMinute caps the drafts one workspace asks for in a minute: a looping agent token must
	// not burn a workspace's own key, which no allowance bounds.
	draftsPerMinute = 30
	// maxDraftWait bounds a long poll: one request, no connection held while it waits.
	maxDraftWait = 20 * time.Second
	// draftExpiry is how long a ready draft waits for a decision; draftTextRetention how long a draft's
	// text and prompt are kept after it ended.
	draftExpiry        = 7 * 24 * time.Hour
	draftTextRetention = 90 * 24 * time.Hour
)

var (
	// ErrDraftNotFound means no draft with that id on that conversation of this workspace.
	ErrDraftNotFound = errors.New("no AI draft with that id on this conversation")
	// ErrDraftDecided means the draft was already sent, discarded or replaced.
	ErrDraftDecided = errors.New("this AI draft was already sent, discarded or replaced")
	// ErrDraftsRateLimited means the workspace asked for draftsPerMinute drafts in the last minute.
	ErrDraftsRateLimited = fmt.Errorf("at most %d AI drafts a minute in a workspace", draftsPerMinute)
	// ErrConversationNotFound means no conversation with that id in this workspace.
	ErrConversationNotFound = errors.New("no conversation with that id in this workspace")
)

// DraftSource is what a draft used, named from the database: never from the model's text.
type DraftSource struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Locale string `json:"locale,omitempty"`
	Number int64  `json:"number,omitempty"`
	Title  string `json:"title"`
}

// DraftWarning is something in the draft the conversation does not hold: an address, a number or the
// name of another customer, which a model can copy from a past ticket or be told to add.
type DraftWarning struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Draft is one AI reply draft.
type Draft struct {
	ID                 string         `json:"id"`
	ConversationID     string         `json:"conversation_id"`
	Status             string         `json:"status"`
	ErrorCode          *string        `json:"error_code"`
	Prompt             string         `json:"prompt"`
	Text               *string        `json:"text"`
	MissingInfo        *string        `json:"missing_info"`
	Sources            []DraftSource  `json:"sources"`
	Warnings           []DraftWarning `json:"warnings"`
	Model              *string        `json:"model"`
	Outcome            *string        `json:"outcome"`
	Similarity         *float64       `json:"similarity"`
	SentMessageID      *string        `json:"sent_message_id"`
	RequestedByAgentID *string        `json:"requested_by_agent_id"`
	CreatedAt          time.Time      `json:"created_at"`
	ReadyAt            *time.Time     `json:"ready_at"`
	DecidedAt          *time.Time     `json:"decided_at"`
}

// draftColumns read a draft with the state its task gives it: a pending draft is queued, running,
// failed or cancelled as its task is; one whose task is gone (a restored workspace) failed.
const draftColumns = `d.id::text,d.conversation_id::text,
	CASE WHEN d.status<>'pending' THEN d.status
		WHEN t.state IN ('queued','running','cancelled') THEN t.state ELSE 'failed' END,
	CASE WHEN d.status='pending' AND t.id IS NULL THEN 'restored'
		WHEN d.status='pending' AND t.state='done' THEN 'internal'
		WHEN d.status='pending' AND t.state IN ('failed','cancelled') THEN t.error_code END,
	d.prompt,d.text,d.missing_info,d.sources,d.warnings,d.model,d.outcome,d.similarity::float8,d.sent_message_id::text,
	d.requested_by_agent_id::text,d.created_at,d.ready_at,d.decided_at
	FROM ai_drafts d LEFT JOIN ai_tasks t ON t.workspace_id=d.workspace_id AND t.id=d.task_id`

func scanDraft(row pgx.Row) (Draft, error) {
	var d Draft
	var sources, warnings []byte
	err := row.Scan(&d.ID, &d.ConversationID, &d.Status, &d.ErrorCode, &d.Prompt, &d.Text, &d.MissingInfo, &sources, &warnings, &d.Model,
		&d.Outcome, &d.Similarity, &d.SentMessageID, &d.RequestedByAgentID, &d.CreatedAt, &d.ReadyAt, &d.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, ErrDraftNotFound
	}
	if err != nil {
		return Draft{}, err
	}
	d.Sources, d.Warnings = []DraftSource{}, []DraftWarning{}
	_ = json.Unmarshal(sources, &d.Sources)
	_ = json.Unmarshal(warnings, &d.Warnings)
	d.CreatedAt, d.ReadyAt, d.DecidedAt = d.CreatedAt.UTC(), utcPointer(d.ReadyAt), utcPointer(d.DecidedAt)
	return d, nil
}

// CreateDraft queues a draft of a reply to the conversation, with the person's optional prompt. It
// refuses at once what could never run: the feature off, the server's allowance spent, too many drafts
// this minute. The person's earlier drafts on the conversation are replaced: a pending one is
// cancelled, a ready one nobody decided on is superseded.
func (s *Service) CreateDraft(ctx context.Context, workspaceID string, actor Actor, conversationID, prompt string) (Draft, error) {
	prompt = strings.TrimSpace(prompt)
	if utf8.RuneCountInString(prompt) > maxPromptRunes {
		return Draft{}, invalid("A prompt has at most %d characters.", maxPromptRunes)
	}
	if !isUUID(conversationID) {
		return Draft{}, ErrConversationNotFound
	}
	binding, err := s.Resolve(ctx, workspaceID, SlotDraft)
	if err != nil {
		return Draft{}, err
	}
	now := s.now().UTC()
	var draft Draft
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
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-drafts:"+workspaceID); err != nil {
			return err
		}
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ai_drafts WHERE workspace_id=$1 AND created_at>$2`, workspaceID, now.Add(-time.Minute)).Scan(&recent); err != nil {
			return err
		}
		if recent >= draftsPerMinute {
			return ErrDraftsRateLimited
		}
		if actor.AgentID != "" {
			// Only the asker's own: an agent token acting for a person never replaces the person's draft.
			rows, err := tx.Query(ctx, `UPDATE ai_drafts SET status='cancelled' WHERE workspace_id=$1 AND conversation_id=$2 AND requested_by_agent_id=$3
				AND requested_by_token_id IS NOT DISTINCT FROM $4::uuid AND status='pending' RETURNING task_id::text`, workspaceID, conversationID, actor.AgentID, nullable(actor.TokenID))
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
			if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET outcome='superseded',decided_at=$4,decided_by_agent_id=$3
				WHERE workspace_id=$1 AND conversation_id=$2 AND requested_by_agent_id=$3 AND requested_by_token_id IS NOT DISTINCT FROM $5::uuid AND status='ready' AND outcome IS NULL`,
				workspaceID, conversationID, actor.AgentID, now, nullable(actor.TokenID)); err != nil {
				return err
			}
		}
		taskID, err := s.Enqueue(ctx, tx, workspaceID, NewTask{Kind: SlotDraft, ConversationID: conversationID, Actor: actor})
		if err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO ai_drafts(workspace_id,conversation_id,task_id,requested_by_agent_id,requested_by_token_id,prompt,status,created_at)
			VALUES($1,$2,$3,$4,$5,$6,'pending',$7) RETURNING id::text`, workspaceID, conversationID, taskID, nullable(actor.AgentID), nullable(actor.TokenID),
			prompt, now).Scan(&id); err != nil {
			return err
		}
		draft, err = scanDraft(tx.QueryRow(ctx, `SELECT `+draftColumns+` WHERE d.workspace_id=$1 AND d.id=$2`, workspaceID, id))
		return err
	})
	return draft, err
}

func conversationExists(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrConversationNotFound
	}
	return nil
}

// GetDraft reads one draft of the conversation.
func (s *Service) GetDraft(ctx context.Context, workspaceID, conversationID, id string) (Draft, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Draft{}, ErrDraftNotFound
	}
	var draft Draft
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		draft, err = scanDraft(tx.QueryRow(ctx, `SELECT `+draftColumns+` WHERE d.workspace_id=$1 AND d.conversation_id=$2 AND d.id=$3`, workspaceID, conversationID, id))
		return err
	})
	return draft, err
}

// WaitDraft reads a draft, waiting up to wait (at most 20 s) while it is queued or running. It reads
// in short transactions; no connection is held between reads.
func (s *Service) WaitDraft(ctx context.Context, workspaceID, conversationID, id string, wait time.Duration) (Draft, error) {
	deadline := time.Now().Add(min(wait, maxDraftWait))
	for {
		draft, err := s.GetDraft(ctx, workspaceID, conversationID, id)
		if err != nil || (draft.Status != DraftQueued && draft.Status != DraftRunning) || !time.Now().Before(deadline) {
			return draft, err
		}
		select {
		case <-ctx.Done():
			return draft, nil
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// ListDrafts lists the conversation's newest drafts, newest first.
func (s *Service) ListDrafts(ctx context.Context, workspaceID, conversationID string) ([]Draft, error) {
	drafts := []Draft{}
	if !isUUID(conversationID) {
		return drafts, ErrConversationNotFound
	}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := conversationExists(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+draftColumns+` WHERE d.workspace_id=$1 AND d.conversation_id=$2 ORDER BY d.created_at DESC, d.id LIMIT 20`, workspaceID, conversationID)
		if err != nil {
			return err
		}
		drafts, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Draft, error) { return scanDraft(row) })
		return err
	})
	if drafts == nil {
		drafts = []Draft{}
	}
	return drafts, err
}

// DiscardDraft stops a draft being written (cancelled, which does not count as discarded: nobody saw
// it) or discards a ready one nobody decided on.
func (s *Service) DiscardDraft(ctx context.Context, workspaceID string, actor Actor, conversationID, id string) (Draft, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Draft{}, ErrDraftNotFound
	}
	now := s.now().UTC()
	var draft Draft
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var status string
		var outcome, task *string
		err := tx.QueryRow(ctx, `SELECT status,outcome,task_id::text FROM ai_drafts WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE`,
			workspaceID, conversationID, id).Scan(&status, &outcome, &task)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDraftNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case status == "pending":
			if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET status='cancelled' WHERE workspace_id=$1 AND id=$2`, workspaceID, id); err != nil {
				return err
			}
			if task != nil {
				if _, err := s.CancelTask(ctx, tx, workspaceID, *task); err != nil {
					return err
				}
			}
		case status == "ready" && outcome == nil:
			if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET outcome='discarded',decided_at=$3,decided_by_agent_id=$4 WHERE workspace_id=$1 AND id=$2`,
				workspaceID, id, now, nullable(actor.AgentID)); err != nil {
				return err
			}
		case status == "cancelled" || (outcome != nil && *outcome == "discarded"):
			// Already stopped or discarded: discarding again changes nothing.
		default:
			return ErrDraftDecided
		}
		draft, err = scanDraft(tx.QueryRow(ctx, `SELECT `+draftColumns+` WHERE d.workspace_id=$1 AND d.id=$2`, workspaceID, id))
		return err
	})
	return draft, err
}

// DraftOutcomes counts the drafts asked for in [from, to) by how they ended, for the team's numbers
// (#100): sent nearly as written, sent edited, discarded, superseded, expired, cancelled, failed, open.
func (s *Service) DraftOutcomes(ctx context.Context, workspaceID string, from, to time.Time) (map[string]int, error) {
	counts := map[string]int{}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT CASE WHEN d.outcome IS NOT NULL THEN d.outcome
				WHEN d.status='cancelled' OR t.state='cancelled' THEN 'cancelled'
				WHEN d.status='ready' THEN 'open'
				WHEN t.state IN ('queued','running') THEN 'open' ELSE 'failed' END, count(*)
			FROM ai_drafts d LEFT JOIN ai_tasks t ON t.workspace_id=d.workspace_id AND t.id=d.task_id
			WHERE d.workspace_id=$1 AND d.created_at>=$2 AND d.created_at<$3 GROUP BY 1`, workspaceID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var outcome string
			var n int
			if err := rows.Scan(&outcome, &n); err != nil {
				return err
			}
			counts[outcome] = n
		}
		return rows.Err()
	})
	return counts, err
}

// ArticleDraftsThisMonth counts the drafts asked for this UTC month that used the article as a source.
func (s *Service) ArticleDraftsThisMonth(ctx context.Context, workspaceID, articleID string) (int, error) {
	var n int
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM ai_draft_sources WHERE workspace_id=$1 AND kb_article_id=$2 AND created_at>=$3`,
			workspaceID, articleID, monthStart(s.now())).Scan(&n)
	})
	return n, err
}
