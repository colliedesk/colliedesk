package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The worker's side of AI drafts: claim a queued draft with what the model may read, write the result,
// meter every call, and send the answers auto-answer scheduled (docs/internals/ai-drafts.md).

const (
	// aiDraftMaxAttempts is how many calls one draft may cost before it fails for good.
	aiDraftMaxAttempts = 3
	// aiDraftTurns is how many of the newest public messages a draft reads.
	aiDraftTurns = 12
	// aiDraftTags is how many of the workspace's tags triage chooses from.
	aiDraftTags = 200
)

// AIDraftTurn is one public message of the conversation, oldest first; notes never reach a model.
type AIDraftTurn struct {
	FromCustomer bool
	Name         string
	Body         string
}

// AIDraftJob is a claimed draft and everything its model call may read, all from its own workspace.
type AIDraftJob struct {
	DraftID        string
	WorkspaceID    string
	ConversationID string
	Attempt        int
	WorkspaceName  string
	Subject        string
	Turns          []AIDraftTurn
	Sources        []AIRetrievedDoc
	Tags           []string
}

// AIUsage is one call to a provider. Reported false means the provider sent no token counts.
type AIUsage struct {
	DraftID          string
	Feature          string
	Model            string
	Reported         bool
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ErrorCode        string
}

// AIDraftWorkspacesDue lists the workspaces with a queued draft whose time has come or a scheduled
// answer that is due. Only ids leave the transaction; each is worked in its own scope.
func (s *Store) AIDraftWorkspacesDue(ctx context.Context) ([]string, error) {
	var workspaces []string
	err := s.db.Privileged(ctx, database.PrivilegeAIDraftQueue, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT workspace_id::text FROM ai_drafts
			WHERE (status='queued' AND next_attempt_at<=now()) OR (status='scheduled' AND auto_send_at<=now())`)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list workspaces with AI drafts due: %w", err)
	}
	return workspaces, nil
}

// ClaimAIDraft takes the workspace's oldest queued draft that is due, counts the attempt, and holds it
// for lease (another worker skips it until then), then reads what the model may see: the newest public
// messages, the sources retrieval finds for them and the workspace's tags. Nil means nothing is due.
func (s *Store) ClaimAIDraft(ctx context.Context, workspaceID string, lease time.Duration) (*AIDraftJob, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*AIDraftJob, error) {
		job := AIDraftJob{WorkspaceID: workspaceID}
		err := tx.QueryRow(ctx, `UPDATE ai_drafts SET attempts=attempts+1,next_attempt_at=now()+$2::interval WHERE id=(
				SELECT id FROM ai_drafts WHERE workspace_id=$1 AND status='queued' AND next_attempt_at<=now()
				ORDER BY next_attempt_at, created_at LIMIT 1 FOR UPDATE SKIP LOCKED)
			RETURNING id::text,conversation_id::text,attempts`, workspaceID, lease.String()).Scan(&job.DraftID, &job.ConversationID, &job.Attempt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("claim AI draft: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT w.name,c.subject FROM conversations c JOIN workspaces w ON w.id=c.workspace_id
			WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, job.ConversationID).Scan(&job.WorkspaceName, &job.Subject); err != nil {
			return nil, fmt.Errorf("read conversation for AI draft: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT kind='incoming',COALESCE(CASE WHEN kind='incoming' THEN NULLIF(ct.name,'') ELSE NULLIF(a.display_name,'') END,''),m.body
			FROM (SELECT * FROM messages WHERE workspace_id=$1 AND conversation_id=$2 AND kind IN ('incoming','outgoing')
				ORDER BY created_at DESC, id DESC LIMIT $3) m
			JOIN conversations c ON c.workspace_id=m.workspace_id AND c.id=m.conversation_id
			LEFT JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			LEFT JOIN agents a ON a.workspace_id=m.workspace_id AND a.id=m.author_agent_id
			ORDER BY m.created_at, m.id`, workspaceID, job.ConversationID, aiDraftTurns)
		if err != nil {
			return nil, fmt.Errorf("read messages for AI draft: %w", err)
		}
		job.Turns, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (AIDraftTurn, error) {
			var turn AIDraftTurn
			return turn, row.Scan(&turn.FromCustomer, &turn.Name, &turn.Body)
		})
		if err != nil {
			return nil, fmt.Errorf("read messages for AI draft: %w", err)
		}
		job.Sources, err = AIRetrieve(ctx, tx, workspaceID, AIRetrievalQuery{Text: aiDraftQuery(job.Subject, job.Turns), ExcludeConversationID: job.ConversationID,
			KBLimit: 5, ConversationLimit: 3})
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(ctx, `SELECT name FROM tags WHERE workspace_id=$1 ORDER BY name LIMIT $2`, workspaceID, aiDraftTags)
		if err != nil {
			return nil, fmt.Errorf("read tags for AI draft: %w", err)
		}
		job.Tags, err = pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, fmt.Errorf("read tags for AI draft: %w", err)
		}
		return &job, nil
	})
}

// aiDraftQuery is what retrieval searches for: the subject and the newest customer messages, which
// say what is being asked now.
func aiDraftQuery(subject string, turns []AIDraftTurn) string {
	parts := []string{subject}
	for i := len(turns) - 1; i >= 0 && len(parts) < 3; i-- {
		if turns[i].FromCustomer {
			parts = append(parts, cutRunes(turns[i].Body, 2000))
		}
	}
	return strings.Join(parts, "\n")
}

// FinishAIDraft writes the model's draft. A draft retired while the model wrote it (the customer wrote
// again) keeps its status. A high-confidence answer whose topic has earned it is scheduled to send on
// its own after the workspace's delay, unless a person replied meanwhile.
func (s *Store) FinishAIDraft(ctx context.Context, workspaceID, draftID string, result domain.AIDraftResult) (domain.AIDraft, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AIDraft, error) {
		var conversationID string
		var triage any
		if result.Triage != nil {
			triage = result.Triage
		}
		reasons := result.ConfidenceReasons
		if reasons == nil {
			reasons = []string{}
		}
		err := tx.QueryRow(ctx, `UPDATE ai_drafts SET status=CASE WHEN status='queued' THEN 'ready' ELSE status END,
				action=$3,body=NULLIF($4,''),confidence=$5,confidence_reasons=$6,handoff_reason=NULLIF($7,''),topic_article_id=NULLIF($8,'')::uuid,
				sources=$9,triage=$10,model=$11,error_code=NULL,ready_at=now()
			WHERE workspace_id=$1 AND id=$2 RETURNING conversation_id::text`,
			workspaceID, draftID, result.Action, result.Body, result.Confidence, reasons, result.HandoffReason, result.TopicArticleID,
			domain.SourcesJSON(result.Sources), triage, result.Model).Scan(&conversationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AIDraft{}, domain.ErrAIDraftNotFound
		}
		if err != nil {
			return domain.AIDraft{}, fmt.Errorf("write AI draft: %w", err)
		}
		if err := scheduleAutoAnswer(ctx, tx, workspaceID, draftID); err != nil {
			return domain.AIDraft{}, err
		}
		return getAIDraft(ctx, tx, workspaceID, conversationID, draftID, false)
	})
}

// scheduleAutoAnswer schedules a ready draft when auto-answer may send it (autoAnswerAllowed).
func scheduleAutoAnswer(ctx context.Context, tx pgx.Tx, workspaceID, draftID string) error {
	delay, ok, err := autoAnswerAllowed(ctx, tx, workspaceID, draftID, domain.AIDraftReady)
	if err != nil || !ok {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE ai_drafts SET status='scheduled',auto_send_at=now()+make_interval(secs => $3) WHERE workspace_id=$1 AND id=$2`,
		workspaceID, draftID, delay)
	return err
}

// autoAnswerAllowed says whether auto-answer may send a draft in the given status, and after how many
// seconds: the workspace turned it on, the draft is a high-confidence answer, its topic's record meets
// the workspace's bar, and the newest public message of the conversation is still the customer's.
func autoAnswerAllowed(ctx context.Context, tx pgx.Tx, workspaceID, draftID, status string) (int, bool, error) {
	settings, err := readAISettings(ctx, tx, workspaceID, true)
	if err != nil || !settings.DraftsEnabled || !settings.AutoAnswerEnabled {
		return 0, false, err
	}
	var topic *string
	var waiting bool
	err = tx.QueryRow(ctx, `SELECT d.topic_article_id::text,
			(SELECT m.kind='incoming' FROM messages m WHERE m.workspace_id=d.workspace_id AND m.conversation_id=d.conversation_id AND m.kind IN ('incoming','outgoing')
				ORDER BY m.created_at DESC, m.id DESC LIMIT 1) IS TRUE
		FROM ai_drafts d WHERE d.workspace_id=$1 AND d.id=$2 AND d.status=$3 AND d.action='answer' AND d.confidence='high' AND d.body IS NOT NULL`,
		workspaceID, draftID, status).Scan(&topic, &waiting)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read AI draft for auto-answer: %w", err)
	}
	if topic == nil || !waiting {
		return 0, false, nil
	}
	var reviewed, accepted int
	err = tx.QueryRow(ctx, `SELECT reviewed,accepted FROM (`+aiTopicRecordQuery+`) r WHERE r.topic_article_id=$2`, workspaceID, *topic).Scan(&reviewed, &accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read AI topic record: %w", err)
	}
	return settings.AutoAnswerDelaySeconds, domain.AutoAnswerEligible(settings, reviewed, accepted), nil
}

// FailAIDraft records a failed call. A retryable one is tried again after retryAfter until the draft
// has used its attempts; then, or for any other error, the draft fails with the code.
func (s *Store) FailAIDraft(ctx context.Context, workspaceID, draftID, code string, retryable bool, retryAfter time.Duration) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE ai_drafts SET error_code=$3,
				status=CASE WHEN status<>'queued' THEN status WHEN $4 AND attempts<$5 THEN 'queued' ELSE 'failed' END,
				next_attempt_at=now()+$6::interval
			WHERE workspace_id=$1 AND id=$2`, workspaceID, draftID, code, retryable, aiDraftMaxAttempts, retryAfter.String())
		if err != nil {
			return fmt.Errorf("record failed AI draft: %w", err)
		}
		return nil
	})
}

// RecordAIUsage stores one provider call. Token counts the provider did not report stay NULL.
func (s *Store) RecordAIUsage(ctx context.Context, workspaceID string, usage AIUsage) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		count := func(value int64) any {
			if !usage.Reported {
				return nil
			}
			return value
		}
		_, err := tx.Exec(ctx, `INSERT INTO ai_usage(workspace_id,ai_draft_id,feature,model,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,error_code)
			VALUES($1,NULLIF($2,'')::uuid,$3,$4,$5,$6,$7,$8,NULLIF($9,''))`, workspaceID, usage.DraftID, usage.Feature, usage.Model,
			count(usage.InputTokens), count(usage.OutputTokens), count(usage.CacheReadTokens), count(usage.CacheWriteTokens), usage.ErrorCode)
		if err != nil {
			return fmt.Errorf("record AI usage: %w", err)
		}
		return nil
	})
}

// SendDueAutoAnswers sends the workspace's scheduled answers whose time has come, each as a reply with
// no author that names its draft, and sets the conversation to pending: it waits for the customer.
// The rules are checked again first: an answer whose topic lost its record, or whose conversation got a
// reply meanwhile, goes back to being a draft for a person.
func (s *Store) SendDueAutoAnswers(ctx context.Context, workspaceID string) (int, error) {
	sent := 0
	for {
		done, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
			var draftID, conversationID string
			var body *string
			err := tx.QueryRow(ctx, `SELECT id::text,conversation_id::text,body FROM ai_drafts WHERE workspace_id=$1 AND status='scheduled' AND auto_send_at<=now()
				ORDER BY auto_send_at LIMIT 1 FOR UPDATE SKIP LOCKED`, workspaceID).Scan(&draftID, &conversationID, &body)
			if errors.Is(err, pgx.ErrNoRows) {
				return true, nil
			}
			if err != nil {
				return false, fmt.Errorf("claim scheduled AI answer: %w", err)
			}
			if _, ok, err := autoAnswerAllowed(ctx, tx, workspaceID, draftID, domain.AIDraftScheduled); err != nil || !ok || body == nil {
				if err != nil {
					return false, err
				}
				// Checked again and refused: it stays a draft for a person.
				_, err := tx.Exec(ctx, `UPDATE ai_drafts SET status='ready',auto_send_at=NULL WHERE workspace_id=$1 AND id=$2`, workspaceID, draftID)
				return false, err
			}
			message, err := s.createMessage(ctx, tx, workspaceID, conversationID, Actor{AIDraftID: draftID, AutoAnswer: true}, "outgoing", *body, nil, nil)
			if err != nil {
				return false, err
			}
			if _, err := tx.Exec(ctx, `UPDATE conversations SET status='pending',updated_at=now() WHERE workspace_id=$1 AND id=$2 AND status='open'`,
				workspaceID, message.ConversationID); err != nil {
				return false, fmt.Errorf("set auto-answered conversation pending: %w", err)
			}
			sent++
			return false, nil
		})
		if err != nil || done {
			return sent, err
		}
	}
}
