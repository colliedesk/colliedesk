package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// AI drafts: a reply and a triage suggestion the server writes when a customer writes
// (docs/internals/ai-drafts.md). This file is what people and agent tokens do with them; the worker's
// side is in ai-draft-worker.go.

// WithAI returns a store whose customer messages queue AI drafts: the server has an AI provider.
func (s *Store) WithAI(enabled bool) *Store {
	copy := *s
	copy.ai = enabled
	return &copy
}

// AIAvailable says whether this server has an AI provider.
func (s *Store) AIAvailable() bool { return s.ai }

const aiDraftColumns = `d.id::text,d.conversation_id::text,d.message_id::text,d.status,d.action,d.body,d.confidence,d.confidence_reasons,
	d.handoff_reason,d.topic_article_id::text,d.sources,d.triage,d.triage_applied_at,d.outcome,d.similarity,d.auto_send_at,d.error_code,d.model,d.created_at,d.ready_at`

func scanAIDraft(row pgx.Row) (domain.AIDraft, error) {
	var draft domain.AIDraft
	var sources, triage []byte
	var similarity *float32
	err := row.Scan(&draft.ID, &draft.ConversationID, &draft.MessageID, &draft.Status, &draft.Action, &draft.Body, &draft.Confidence, &draft.ConfidenceReasons,
		&draft.HandoffReason, &draft.TopicArticleID, &sources, &triage, &draft.TriageAppliedAt, &draft.Outcome, &similarity, &draft.AutoSendAt, &draft.ErrorCode,
		&draft.Model, &draft.CreatedAt, &draft.ReadyAt)
	if err != nil {
		return domain.AIDraft{}, err
	}
	if err := json.Unmarshal(sources, &draft.Sources); err != nil {
		return domain.AIDraft{}, fmt.Errorf("decode AI draft sources: %w", err)
	}
	if len(triage) > 0 {
		if err := json.Unmarshal(triage, &draft.Triage); err != nil {
			return domain.AIDraft{}, fmt.Errorf("decode AI draft triage: %w", err)
		}
	}
	if similarity != nil {
		value := float64(*similarity)
		draft.Similarity = &value
	}
	if draft.ConfidenceReasons == nil {
		draft.ConfidenceReasons = []string{}
	}
	draft.CreatedAt = domain.UTC(draft.CreatedAt)
	draft.ReadyAt, draft.TriageAppliedAt, draft.AutoSendAt = domain.UTCOptional(draft.ReadyAt), domain.UTCOptional(draft.TriageAppliedAt), domain.UTCOptional(draft.AutoSendAt)
	return draft, nil
}

func getAIDraft(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, draftID string, lock bool) (domain.AIDraft, error) {
	query := `SELECT ` + aiDraftColumns + ` FROM ai_drafts d WHERE d.workspace_id=$1 AND d.conversation_id=$2 AND d.id=$3`
	if lock {
		query += ` FOR UPDATE`
	}
	draft, err := scanAIDraft(tx.QueryRow(ctx, query, workspaceID, conversationID, draftID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AIDraft{}, domain.ErrAIDraftNotFound
	}
	return draft, err
}

func readAISettings(ctx context.Context, tx pgx.Tx, workspaceID string, available bool) (domain.AISettings, error) {
	settings := domain.DefaultAISettings(available)
	var updated time.Time
	err := tx.QueryRow(ctx, `SELECT drafts_enabled,auto_answer_enabled,auto_answer_delay_seconds,auto_answer_min_reviewed,auto_answer_min_accuracy,updated_at
		FROM workspace_ai_settings WHERE workspace_id=$1`, workspaceID).Scan(&settings.DraftsEnabled, &settings.AutoAnswerEnabled,
		&settings.AutoAnswerDelaySeconds, &settings.AutoAnswerMinReviewed, &settings.AutoAnswerMinAccuracy, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return domain.AISettings{}, fmt.Errorf("read AI settings: %w", err)
	}
	updated = domain.UTC(updated)
	settings.UpdatedAt = &updated
	return settings, nil
}

// AISettings returns the workspace's AI settings.
func (s *Store) AISettings(ctx context.Context, workspaceID string) (domain.AISettings, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AISettings, error) {
		return readAISettings(ctx, tx, workspaceID, s.ai)
	})
}

// AISettingsChange holds the settings to change; nil leaves one as it is. The caller validated ranges.
type AISettingsChange struct {
	DraftsEnabled          *bool
	AutoAnswerEnabled      *bool
	AutoAnswerDelaySeconds *int
	AutoAnswerMinReviewed  *int
	AutoAnswerMinAccuracy  *int
}

// UpdateAISettings changes the workspace's AI settings. Turning drafts off stops the drafts still
// queued and every scheduled answer; turning auto-answer off stops the scheduled answers, which stay
// on their conversations as drafts for a person.
func (s *Store) UpdateAISettings(ctx context.Context, workspaceID, agentID string, change AISettingsChange) (domain.AISettings, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AISettings, error) {
		current, err := readAISettings(ctx, tx, workspaceID, s.ai)
		if err != nil {
			return domain.AISettings{}, err
		}
		next := current
		set := func(target *bool, value *bool) {
			if value != nil {
				*target = *value
			}
		}
		setInt := func(target *int, value *int) {
			if value != nil {
				*target = *value
			}
		}
		set(&next.DraftsEnabled, change.DraftsEnabled)
		set(&next.AutoAnswerEnabled, change.AutoAnswerEnabled)
		setInt(&next.AutoAnswerDelaySeconds, change.AutoAnswerDelaySeconds)
		setInt(&next.AutoAnswerMinReviewed, change.AutoAnswerMinReviewed)
		setInt(&next.AutoAnswerMinAccuracy, change.AutoAnswerMinAccuracy)
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_ai_settings(workspace_id,drafts_enabled,auto_answer_enabled,auto_answer_delay_seconds,
				auto_answer_min_reviewed,auto_answer_min_accuracy,updated_at,updated_by_agent_id) VALUES($1,$2,$3,$4,$5,$6,now(),NULLIF($7,'')::uuid)
			ON CONFLICT (workspace_id) DO UPDATE SET drafts_enabled=EXCLUDED.drafts_enabled,auto_answer_enabled=EXCLUDED.auto_answer_enabled,
				auto_answer_delay_seconds=EXCLUDED.auto_answer_delay_seconds,auto_answer_min_reviewed=EXCLUDED.auto_answer_min_reviewed,
				auto_answer_min_accuracy=EXCLUDED.auto_answer_min_accuracy,updated_at=now(),updated_by_agent_id=EXCLUDED.updated_by_agent_id`,
			workspaceID, next.DraftsEnabled, next.AutoAnswerEnabled, next.AutoAnswerDelaySeconds, next.AutoAnswerMinReviewed, next.AutoAnswerMinAccuracy, agentID); err != nil {
			return domain.AISettings{}, fmt.Errorf("save AI settings: %w", err)
		}
		if !next.DraftsEnabled {
			if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET status='superseded' WHERE workspace_id=$1 AND status='queued'`, workspaceID); err != nil {
				return domain.AISettings{}, fmt.Errorf("stop queued AI drafts: %w", err)
			}
		}
		if !next.DraftsEnabled || !next.AutoAnswerEnabled {
			if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET status='ready',auto_send_at=NULL WHERE workspace_id=$1 AND status='scheduled'`, workspaceID); err != nil {
				return domain.AISettings{}, fmt.Errorf("stop scheduled AI answers: %w", err)
			}
		}
		return readAISettings(ctx, tx, workspaceID, s.ai)
	})
}

// supersedeAIDrafts retires the conversation's drafts that are still waiting for someone: a newer one
// replaces them. A scheduled answer is stopped, so it never goes out after the customer wrote again or
// a person replied.
func supersedeAIDrafts(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) error {
	_, err := tx.Exec(ctx, `UPDATE ai_drafts SET status=CASE WHEN status='scheduled' THEN 'cancelled' ELSE 'superseded' END,
			outcome=CASE WHEN status='scheduled' THEN 'auto_cancelled' ELSE outcome END
		WHERE workspace_id=$1 AND conversation_id=$2 AND status IN ('queued','ready','scheduled')`, workspaceID, conversationID)
	if err != nil {
		return fmt.Errorf("supersede AI drafts: %w", err)
	}
	return nil
}

// queueAIDraft queues a draft for a customer message, inside the transaction that stores it. Nothing
// is queued on a server without AI or in a workspace that turned drafts off.
func (s *Store) queueAIDraft(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID string) error {
	if !s.ai {
		return nil
	}
	settings, err := readAISettings(ctx, tx, workspaceID, true)
	if err != nil || !settings.DraftsEnabled {
		return err
	}
	if err := supersedeAIDrafts(ctx, tx, workspaceID, conversationID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ai_drafts(workspace_id,conversation_id,message_id) VALUES($1,$2,$3)`, workspaceID, conversationID, messageID); err != nil {
		return fmt.Errorf("queue AI draft: %w", err)
	}
	return nil
}

// RequestAIDraft queues a new draft for the conversation's newest customer message, replacing the
// conversation's open drafts. A person asks when the draft is missing, failed or off the mark.
func (s *Store) RequestAIDraft(ctx context.Context, workspaceID, conversationID, agentID string) (domain.AIDraft, error) {
	if !s.ai {
		return domain.AIDraft{}, domain.ErrAIUnavailable
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AIDraft, error) {
		settings, err := readAISettings(ctx, tx, workspaceID, true)
		if err != nil {
			return domain.AIDraft{}, err
		}
		if !settings.DraftsEnabled {
			return domain.AIDraft{}, domain.ErrAIDraftsOff
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
			return domain.AIDraft{}, fmt.Errorf("find conversation: %w", err)
		}
		if !exists {
			return domain.AIDraft{}, domain.ErrAIDraftNotFound
		}
		if err := supersedeAIDrafts(ctx, tx, workspaceID, conversationID); err != nil {
			return domain.AIDraft{}, err
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO ai_drafts(workspace_id,conversation_id,message_id,requested_by_agent_id)
			VALUES($1,$2,(SELECT m.id FROM messages m WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND m.kind='incoming' ORDER BY m.created_at DESC, m.id DESC LIMIT 1),
				NULLIF($3,'')::uuid) RETURNING id::text`, workspaceID, conversationID, agentID).Scan(&id); err != nil {
			return domain.AIDraft{}, fmt.Errorf("queue AI draft: %w", err)
		}
		return getAIDraft(ctx, tx, workspaceID, conversationID, id, false)
	})
}

// LatestAIDraft returns the conversation's newest draft, or nil when it has none.
// domain.ErrAIDraftNotFound means the conversation is not in the workspace.
func (s *Store) LatestAIDraft(ctx context.Context, workspaceID, conversationID string) (*domain.AIDraft, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*domain.AIDraft, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2)`, workspaceID, conversationID).Scan(&exists); err != nil {
			return nil, fmt.Errorf("find conversation: %w", err)
		}
		if !exists {
			return nil, domain.ErrAIDraftNotFound
		}
		draft, err := scanAIDraft(tx.QueryRow(ctx, `SELECT `+aiDraftColumns+` FROM ai_drafts d WHERE d.workspace_id=$1 AND d.conversation_id=$2
			ORDER BY d.created_at DESC, d.id DESC LIMIT 1`, workspaceID, conversationID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return &draft, nil
	})
}

// changeAIDraft locks a draft of the conversation and applies change to it.
func (s *Store) changeAIDraft(ctx context.Context, workspaceID, conversationID, draftID string, change func(tx pgx.Tx, draft domain.AIDraft) error) (domain.AIDraft, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AIDraft, error) {
		draft, err := getAIDraft(ctx, tx, workspaceID, conversationID, draftID, true)
		if err != nil {
			return domain.AIDraft{}, err
		}
		if err := change(tx, draft); err != nil {
			return domain.AIDraft{}, err
		}
		return getAIDraft(ctx, tx, workspaceID, conversationID, draftID, false)
	})
}

// DiscardAIDraft records that a person did not want the draft. It counts against its topic's record.
// A scheduled answer is stopped with it. Discarding it again changes nothing; a draft already sent
// from, or replaced by a newer one, is ErrAIDraftState.
func (s *Store) DiscardAIDraft(ctx context.Context, workspaceID, conversationID, draftID, agentID string) (domain.AIDraft, error) {
	return s.changeAIDraft(ctx, workspaceID, conversationID, draftID, func(tx pgx.Tx, draft domain.AIDraft) error {
		if draft.Status == domain.AIDraftDiscarded {
			return nil
		}
		if draft.Status != domain.AIDraftReady && draft.Status != domain.AIDraftScheduled && draft.Status != domain.AIDraftFailed {
			return domain.ErrAIDraftState
		}
		outcome := any(domain.AIOutcomeDiscarded)
		if draft.Status == domain.AIDraftFailed {
			outcome = nil
		}
		_, err := tx.Exec(ctx, `UPDATE ai_drafts SET status='discarded',outcome=$4,auto_send_at=NULL,decided_by_agent_id=NULLIF($5,'')::uuid,decided_at=now()
			WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3`, workspaceID, conversationID, draftID, outcome, agentID)
		return err
	})
}

// CancelAutoAnswer stops a scheduled answer. The draft stays on the conversation, ready for a person.
// A draft that is not scheduled is left as it is, since nothing will go out; one auto-answer already
// sent is ErrAIDraftState, so the caller learns it is too late.
func (s *Store) CancelAutoAnswer(ctx context.Context, workspaceID, conversationID, draftID, agentID string) (domain.AIDraft, error) {
	return s.changeAIDraft(ctx, workspaceID, conversationID, draftID, func(tx pgx.Tx, draft domain.AIDraft) error {
		if draft.Status == domain.AIDraftAutoSent {
			return domain.ErrAIDraftState
		}
		if draft.Status != domain.AIDraftScheduled {
			return nil
		}
		_, err := tx.Exec(ctx, `UPDATE ai_drafts SET status='ready',auto_send_at=NULL,decided_by_agent_id=NULLIF($4,'')::uuid,decided_at=now()
			WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3`, workspaceID, conversationID, draftID, agentID)
		return err
	})
}

// ApplyAITriage adds the suggested tags that still exist and sets the suggested priority. Each is
// undone like any other change of the conversation.
func (s *Store) ApplyAITriage(ctx context.Context, workspaceID, conversationID, draftID, agentID string) (domain.AIDraft, error) {
	return s.changeAIDraft(ctx, workspaceID, conversationID, draftID, func(tx pgx.Tx, draft domain.AIDraft) error {
		if draft.Triage == nil {
			return domain.ErrAIDraftState
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2 FOR NO KEY UPDATE`, workspaceID, conversationID); err != nil {
			return fmt.Errorf("lock conversation: %w", err)
		}
		if len(draft.Triage.Tags) > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO conversation_tags(workspace_id,conversation_id,tag_id)
				SELECT $1,$2,t.id FROM tags t WHERE t.workspace_id=$1 AND t.name=ANY($3) ON CONFLICT DO NOTHING`, workspaceID, conversationID, draft.Triage.Tags); err != nil {
				return fmt.Errorf("add suggested tags: %w", err)
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE conversations SET priority=COALESCE(NULLIF($3,''),priority),updated_at=now() WHERE workspace_id=$1 AND id=$2`,
			workspaceID, conversationID, draft.Triage.Priority); err != nil {
			return fmt.Errorf("set suggested priority: %w", err)
		}
		_, err := tx.Exec(ctx, `UPDATE ai_drafts SET triage_applied_at=now(),triage_applied_by_agent_id=NULLIF($4,'')::uuid
			WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3`, workspaceID, conversationID, draftID, agentID)
		return err
	})
}

// recordAIDraftUse links a reply to the draft it came from and records how much the person changed
// it. Any other reply by a person answers the customer, so the conversation's open drafts are retired.
func recordAIDraftUse(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID, draftID, sent string, automatic bool) error {
	if draftID == "" {
		return supersedeAIDrafts(ctx, tx, workspaceID, conversationID)
	}
	var body *string
	var status string
	err := tx.QueryRow(ctx, `SELECT body,status FROM ai_drafts WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3 FOR UPDATE`, workspaceID, conversationID, draftID).Scan(&body, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrAIDraftNotFound
	}
	if err != nil {
		return fmt.Errorf("read AI draft: %w", err)
	}
	if body == nil || (status != domain.AIDraftReady && status != domain.AIDraftScheduled && status != domain.AIDraftSuperseded) {
		return domain.ErrAIDraftState
	}
	similarity := domain.ReplySimilarity(*body, sent)
	status, outcome := domain.AIDraftUsed, domain.AIOutcomeSentEdited
	if automatic {
		status, outcome = domain.AIDraftAutoSent, domain.AIOutcomeAutoSent
	} else if similarity >= 0.999 {
		outcome = domain.AIOutcomeSentUnchanged
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET ai_draft_id=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, messageID, draftID); err != nil {
		return fmt.Errorf("link reply to AI draft: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_drafts SET status=$4,outcome=$5,similarity=$6,auto_send_at=NULL,decided_at=now()
		WHERE workspace_id=$1 AND conversation_id=$2 AND id=$3`, workspaceID, conversationID, draftID, status, outcome, similarity); err != nil {
		return fmt.Errorf("record AI draft use: %w", err)
	}
	// Other drafts still open on the conversation are answered by this reply too.
	_, err = tx.Exec(ctx, `UPDATE ai_drafts SET status=CASE WHEN status='scheduled' THEN 'cancelled' ELSE 'superseded' END,
			outcome=CASE WHEN status='scheduled' THEN 'auto_cancelled' ELSE outcome END
		WHERE workspace_id=$1 AND conversation_id=$2 AND id<>$3 AND status IN ('queued','ready','scheduled')`, workspaceID, conversationID, draftID)
	return err
}

// AITopicRecords lists, per knowledge base article, how its high-confidence drafts did with people:
// the record auto-answer is earned with.
func (s *Store) AITopicRecords(ctx context.Context, workspaceID string) ([]domain.AITopicRecord, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.AITopicRecord, error) {
		settings, err := readAISettings(ctx, tx, workspaceID, s.ai)
		if err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT a.id::text,a.number,a.title,r.reviewed,r.accepted FROM (`+aiTopicRecordQuery+`) r
			JOIN kb_articles a ON a.workspace_id=$1 AND a.id=r.topic_article_id ORDER BY r.reviewed DESC, a.number`, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list AI topic records: %w", err)
		}
		records, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.AITopicRecord, error) {
			var record domain.AITopicRecord
			err := row.Scan(&record.ArticleID, &record.ArticleNumber, &record.Title, &record.Reviewed, &record.Accepted)
			record.Eligible = domain.AutoAnswerEligible(settings, record.Reviewed, record.Accepted)
			return record, err
		})
		if records == nil {
			records = []domain.AITopicRecord{}
		}
		return records, err
	})
}

// aiTopicRecordQuery counts the reviewed high-confidence drafts of each topic of workspace $1: a draft
// is right when a person sent it with small changes at most. Answers sent on their own never count, so
// auto-answer cannot earn its own record.
const aiTopicRecordQuery = `SELECT topic_article_id,
		count(*)::int AS reviewed,
		(count(*) FILTER (WHERE outcome IN ('sent_unchanged','sent_edited') AND similarity >= ` + aiAcceptedSimilaritySQL + `))::int AS accepted
	FROM ai_drafts WHERE workspace_id=$1 AND confidence='high' AND topic_article_id IS NOT NULL
		AND outcome IN ('sent_unchanged','sent_edited','discarded')
	GROUP BY topic_article_id`

// aiAcceptedSimilaritySQL is domain.AIAcceptedSimilarity written into the query.
const aiAcceptedSimilaritySQL = "0.85"

// AIUsageSummary counts a workspace's AI drafts and the tokens they cost since a time.
type AIUsageSummary struct {
	Drafts       int   `json:"drafts"`
	Calls        int   `json:"calls"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	// What happened to the drafts: sent by a person unchanged or edited, discarded, sent on their own.
	SentUnchanged int `json:"sent_unchanged"`
	SentEdited    int `json:"sent_edited"`
	Discarded     int `json:"discarded"`
	AutoSent      int `json:"auto_sent"`
}

// AIUsageSummary counts the workspace's drafts, calls and tokens since a time, and what people did with
// the drafts: what the results of AI are measured with.
func (s *Store) AIUsageSummary(ctx context.Context, workspaceID string, since time.Time) (AIUsageSummary, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (AIUsageSummary, error) {
		var summary AIUsageSummary
		if err := tx.QueryRow(ctx, `SELECT count(*)::int, COALESCE(sum(input_tokens),0)::bigint, COALESCE(sum(output_tokens),0)::bigint
			FROM ai_usage WHERE workspace_id=$1 AND created_at>=$2`, workspaceID, since).Scan(&summary.Calls, &summary.InputTokens, &summary.OutputTokens); err != nil {
			return AIUsageSummary{}, fmt.Errorf("count AI usage: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::int,
				(count(*) FILTER (WHERE outcome='sent_unchanged'))::int, (count(*) FILTER (WHERE outcome='sent_edited'))::int,
				(count(*) FILTER (WHERE outcome='discarded'))::int, (count(*) FILTER (WHERE outcome='auto_sent'))::int
			FROM ai_drafts WHERE workspace_id=$1 AND created_at>=$2 AND ready_at IS NOT NULL`, workspaceID, since).Scan(&summary.Drafts,
			&summary.SentUnchanged, &summary.SentEdited, &summary.Discarded, &summary.AutoSent); err != nil {
			return AIUsageSummary{}, fmt.Errorf("count AI drafts: %w", err)
		}
		return summary, nil
	})
}
