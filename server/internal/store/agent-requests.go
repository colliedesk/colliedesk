package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// agentRequestSelect returns requests with the token's state and the names of the people involved.
// A token row that is gone counts as revoked. Without full the call and its answer are left out: the
// list every open tab polls carries only what a row shows.
func agentRequestSelect(full bool) string {
	payload := `r.body,r.edited_body,r.result`
	reply := `r.saved_reply_before,sr.title,sr.body,sr.updated_at,sr.deleted_at,r.notices`
	if !full {
		payload = `NULL::jsonb,NULL::jsonb,NULL::jsonb`
		reply = `NULL::jsonb,NULL::text,NULL::text,NULL::timestamptz,NULL::timestamptz,NULL::jsonb`
	}
	return `SELECT r.id::text,r.kind,r.status,r.method,r.route,r.path,` + payload + `,r.summary,r.conversation_id::text,
	r.token_id::text,r.token_label,r.token_scope,COALESCE(r.token_permissions,'{}'),(t.id IS NULL OR t.revoked_at IS NOT NULL),r.requested_by_agent_id::text,COALESCE(NULLIF(ra.display_name,''),ra.email),
	r.decided_by_agent_id::text,COALESCE(NULLIF(da.display_name,''),da.email),r.decided_at,r.reason,r.result_status,r.result_note,r.created_at,r.expires_at,` + reply + `
	FROM agent_requests r
	LEFT JOIN saved_replies sr ON sr.workspace_id=r.workspace_id AND sr.id=r.saved_reply_id
	LEFT JOIN sessions t ON t.workspace_id=r.workspace_id AND t.id=r.token_id
	LEFT JOIN agents ra ON ra.workspace_id=r.workspace_id AND ra.id=r.requested_by_agent_id
	LEFT JOIN agents da ON da.workspace_id=r.workspace_id AND da.id=r.decided_by_agent_id`
}

var requestID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func scanAgentRequest(row pgx.Row) (domain.AgentRequest, error) {
	var item domain.AgentRequest
	var body, edited, result []byte
	var resultStatus *int32
	var deciderID, deciderName *string
	var replyBefore, notices []byte
	var current struct {
		title, body          *string
		updatedAt, deletedAt *time.Time
	}
	err := row.Scan(&item.ID, &item.Kind, &item.Status, &item.Method, &item.Route, &item.Path, &body, &edited, &result, &item.Summary, &item.ConversationID,
		&item.RequestedBy.TokenID, &item.RequestedBy.TokenLabel, &item.RequestedBy.TokenScope, &item.RequestedBy.TokenPermissions, &item.RequestedBy.TokenRevoked,
		&item.RequestedBy.AgentID, &item.RequestedBy.AgentName, &deciderID, &deciderName, &item.DecidedAt, &item.Reason,
		&resultStatus, &item.ResultNote, &item.CreatedAt, &item.ExpiresAt,
		&replyBefore, &current.title, &current.body, &current.updatedAt, &current.deletedAt, &notices)
	if err != nil {
		return domain.AgentRequest{}, err
	}
	if len(notices) > 0 {
		if err := json.Unmarshal(notices, &item.Notices); err != nil {
			return domain.AgentRequest{}, fmt.Errorf("read status notices: %w", err)
		}
	}
	if len(replyBefore) > 0 {
		var before SavedReplySnapshot
		if err := json.Unmarshal(replyBefore, &before); err != nil {
			return domain.AgentRequest{}, fmt.Errorf("read saved reply snapshot: %w", err)
		}
		item.SavedReply = &domain.AgentRequestSavedReply{ID: before.ID, Before: &domain.SavedReplyText{Title: before.Title, Body: before.Body, UpdatedAt: domain.UTC(before.UpdatedAt)},
			Restores: before.Restores}
		if current.title != nil && current.body != nil && current.updatedAt != nil {
			item.SavedReply.Current = &domain.SavedReplyState{Title: *current.title, Body: *current.body, UpdatedAt: domain.UTC(*current.updatedAt), DeletedAt: domain.UTCOptional(current.deletedAt)}
		}
	}
	item.Body, item.EditedBody, item.Result = body, edited, result
	if resultStatus != nil {
		status := int(*resultStatus)
		item.ResultStatus = &status
	}
	if deciderID != nil || deciderName != nil {
		item.DecidedBy = &domain.AgentDecider{AgentID: deciderID}
		if deciderName != nil {
			item.DecidedBy.Name = *deciderName
		}
	}
	item.CreatedAt = domain.UTC(item.CreatedAt)
	item.ExpiresAt = domain.UTC(item.ExpiresAt)
	item.DecidedAt = domain.UTCOptional(item.DecidedAt)
	return item, nil
}

// NewAgentRequest is the call an agent token made that waits for a person.
type NewAgentRequest struct {
	TokenID, AgentID     string
	Kind, Method         string
	Route, Path, Summary string
	Body                 json.RawMessage
	ConversationID       *string
	// SavedReply is the saved reply the call edits, deletes or restores, as the token saw it.
	SavedReply *SavedReplySnapshot
	// TokenPermissions is what the token held when it asked, kept for the person deciding.
	TokenPermissions []string
	// Notices are what the status change would tell customers when the token asked.
	Notices []domain.StatusNoticePreview
}

// SavedReplySnapshot is a saved reply as an agent request keeps it: what the person deciding compares
// the proposed change with.
type SavedReplySnapshot struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	UpdatedAt time.Time `json:"updated_at"`
	// Restores is the version a restore request applies.
	Restores *domain.SavedReplyRestoredVersion `json:"restores,omitempty"`
}

// expireAgentRequests marks the workspace's requests that waited past their expiry. Reads call it, so
// a request is never shown as pending after it expired.
func (s *Store) expireAgentRequests(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `UPDATE agent_requests SET status='expired',decided_at=expires_at
		WHERE workspace_id=$1 AND status='pending' AND expires_at<=$2`, workspaceID, s.now()); err != nil {
		return fmt.Errorf("expire agent requests: %w", err)
	}
	return nil
}

func getAgentRequest(ctx context.Context, tx pgx.Tx, workspaceID, id string, lock bool) (domain.AgentRequest, error) {
	query := agentRequestSelect(true) + ` WHERE r.workspace_id=$1 AND r.id=$2`
	if lock {
		query += ` FOR UPDATE OF r`
	}
	return scanAgentRequest(tx.QueryRow(ctx, query, workspaceID, id))
}

// CreateAgentRequest stores a request of the token in in.TokenID. It answers
// domain.ErrTooManyAgentRequests when the token has domain.MaxPendingAgentRequests waiting.
func (s *Store) CreateAgentRequest(ctx context.Context, workspaceID string, in NewAgentRequest) (domain.AgentRequest, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AgentRequest, error) {
		if err := s.expireAgentRequests(ctx, tx, workspaceID); err != nil {
			return domain.AgentRequest{}, err
		}
		// Serializes one token's requests so two calls at once cannot both pass the count.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('agent-requests|' || $1::text, 0))`, in.TokenID); err != nil {
			return domain.AgentRequest{}, fmt.Errorf("lock agent requests: %w", err)
		}
		var waiting int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_requests WHERE workspace_id=$1 AND token_id=$2 AND status='pending'`,
			workspaceID, in.TokenID).Scan(&waiting); err != nil {
			return domain.AgentRequest{}, fmt.Errorf("count agent requests: %w", err)
		}
		if waiting >= domain.MaxPendingAgentRequests {
			return domain.AgentRequest{}, domain.ErrTooManyAgentRequests
		}
		now := s.now()
		var filed int
		var oldest *time.Time
		if err := tx.QueryRow(ctx, `SELECT count(*),min(created_at) FROM agent_requests WHERE workspace_id=$1 AND token_id=$2 AND created_at>$3`,
			workspaceID, in.TokenID, now.Add(-time.Hour)).Scan(&filed, &oldest); err != nil {
			return domain.AgentRequest{}, fmt.Errorf("count recent agent requests: %w", err)
		}
		if filed >= domain.MaxAgentRequestsPerHour && oldest != nil {
			return domain.AgentRequest{}, &domain.AgentRequestRateError{RetryAfter: oldest.Add(time.Hour).Sub(now)}
		}
		var body any
		if len(in.Body) > 0 {
			body = string(in.Body)
		}
		var replyID, before any
		if in.SavedReply != nil {
			encoded, err := json.Marshal(in.SavedReply)
			if err != nil {
				return domain.AgentRequest{}, fmt.Errorf("encode saved reply snapshot: %w", err)
			}
			replyID, before = in.SavedReply.ID, string(encoded)
		}
		var notices any
		if len(in.Notices) > 0 {
			encoded, err := json.Marshal(in.Notices)
			if err != nil {
				return domain.AgentRequest{}, fmt.Errorf("encode status notices: %w", err)
			}
			notices = string(encoded)
		}
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,token_permissions,requested_by_agent_id,kind,method,route,path,body,summary,conversation_id,created_at,expires_at,saved_reply_id,saved_reply_before,notices)
			SELECT $1,s.id,s.label,s.scope,$15,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14::jsonb,$16::jsonb FROM sessions s
			WHERE s.workspace_id=$1 AND s.id=$2 AND s.scope IS NOT NULL
			RETURNING id::text`, workspaceID, in.TokenID, in.AgentID, in.Kind, in.Method, in.Route, in.Path, body, in.Summary,
			in.ConversationID, now, now.Add(domain.AgentRequestLifetime), replyID, before, in.TokenPermissions, notices).Scan(&id)
		if err != nil {
			return domain.AgentRequest{}, fmt.Errorf("insert agent request: %w", err)
		}
		return getAgentRequest(ctx, tx, workspaceID, id, false)
	})
}

// AgentRequestFilter narrows a list. TokenID, when set, keeps only that token's requests; Full adds
// each request's call and answer.
type AgentRequestFilter struct {
	Status  string
	TokenID string
	Full    bool
}

// maxListedAgentRequests bounds one list; pending requests come first.
const maxListedAgentRequests = 200

// ListAgentRequests returns the workspace's requests, pending first, then newest first.
func (s *Store) ListAgentRequests(ctx context.Context, workspaceID string, filter AgentRequestFilter) ([]domain.AgentRequest, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.AgentRequest, error) {
		if err := s.expireAgentRequests(ctx, tx, workspaceID); err != nil {
			return nil, err
		}
		query := agentRequestSelect(filter.Full) + ` WHERE r.workspace_id=$1`
		args := []any{workspaceID}
		if filter.Status != "" {
			args = append(args, filter.Status)
			query += fmt.Sprintf(` AND r.status=$%d`, len(args))
		}
		if filter.TokenID != "" {
			args = append(args, filter.TokenID)
			query += fmt.Sprintf(` AND r.token_id=$%d`, len(args))
		}
		query += fmt.Sprintf(` ORDER BY (r.status='pending') DESC, r.created_at DESC LIMIT %d`, maxListedAgentRequests)
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("list agent requests: %w", err)
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.AgentRequest, error) { return scanAgentRequest(row) })
		if err != nil {
			return nil, fmt.Errorf("list agent requests: %w", err)
		}
		return items, nil
	})
}

// GetAgentRequest returns one request. A non-empty tokenID finds only that token's request.
func (s *Store) GetAgentRequest(ctx context.Context, workspaceID, id, tokenID string) (domain.AgentRequest, error) {
	if !requestID.MatchString(id) {
		return domain.AgentRequest{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AgentRequest, error) {
		if err := s.expireAgentRequests(ctx, tx, workspaceID); err != nil {
			return domain.AgentRequest{}, err
		}
		item, err := getAgentRequest(ctx, tx, workspaceID, id, false)
		if err == nil && tokenID != "" && (item.RequestedBy.TokenID == nil || *item.RequestedBy.TokenID != tokenID) {
			return domain.AgentRequest{}, pgx.ErrNoRows
		}
		return item, err
	})
}

// decide locks a pending request and applies change to it. It answers domain.ErrAgentRequestDecided
// when the request is no longer pending (an expired one included), so a decision happens once.
func (s *Store) decide(ctx context.Context, workspaceID, id string, change func(tx pgx.Tx, item domain.AgentRequest) error) (domain.AgentRequest, error) {
	if !requestID.MatchString(id) {
		return domain.AgentRequest{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AgentRequest, error) {
		if err := s.expireAgentRequests(ctx, tx, workspaceID); err != nil {
			return domain.AgentRequest{}, err
		}
		item, err := getAgentRequest(ctx, tx, workspaceID, id, true)
		if err != nil {
			return domain.AgentRequest{}, err
		}
		if item.Status != domain.AgentRequestPending {
			return domain.AgentRequest{}, domain.ErrAgentRequestDecided
		}
		if err := change(tx, item); err != nil {
			return domain.AgentRequest{}, err
		}
		return getAgentRequest(ctx, tx, workspaceID, id, false)
	})
}

// ClaimAgentRequest marks a pending request approved by agentID before its call runs, so a second
// approval (a double click, a second tab) finds it decided and runs nothing. A non-nil edited is the
// call as the person edited it (a reply draft); it is kept beside the agent's own. A request of a
// revoked token is refused.
func (s *Store) ClaimAgentRequest(ctx context.Context, workspaceID, id, agentID string, edited json.RawMessage) (domain.AgentRequest, error) {
	return s.decide(ctx, workspaceID, id, func(tx pgx.Tx, item domain.AgentRequest) error {
		if item.RequestedBy.TokenRevoked {
			return domain.ErrAgentRequestTokenRevoked
		}
		var replacement any
		if edited != nil {
			replacement = string(edited)
		}
		_, err := tx.Exec(ctx, `UPDATE agent_requests SET status='approved',decided_by_agent_id=$3,decided_at=$4,edited_body=$5::jsonb
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, agentID, s.now(), replacement)
		return err
	})
}

// FinishAgentRequest records what the approved call answered: status approved for a 2xx answer,
// failed otherwise. result is nil when the answer is not kept; note says why.
func (s *Store) FinishAgentRequest(ctx context.Context, workspaceID, id string, resultStatus int, result json.RawMessage, note *string) (domain.AgentRequest, error) {
	status := domain.AgentRequestApproved
	if resultStatus < 200 || resultStatus > 299 {
		status = domain.AgentRequestFailed
	}
	var stored any
	if len(result) > 0 {
		stored = string(result)
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.AgentRequest, error) {
		if _, err := tx.Exec(ctx, `UPDATE agent_requests SET status=$3,result_status=$4,result=$5::jsonb,result_note=$6
			WHERE workspace_id=$1 AND id=$2 AND status='approved' AND result_status IS NULL`, workspaceID, id, status, resultStatus, stored, note); err != nil {
			return domain.AgentRequest{}, fmt.Errorf("record agent request result: %w", err)
		}
		return getAgentRequest(ctx, tx, workspaceID, id, false)
	})
}

// RejectAgentRequest records that agentID refused the request; nothing runs.
func (s *Store) RejectAgentRequest(ctx context.Context, workspaceID, id, agentID string, reason *string) (domain.AgentRequest, error) {
	return s.decide(ctx, workspaceID, id, func(tx pgx.Tx, _ domain.AgentRequest) error {
		_, err := tx.Exec(ctx, `UPDATE agent_requests SET status='rejected',decided_by_agent_id=$3,decided_at=$4,reason=$5
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, agentID, s.now(), reason)
		return err
	})
}

// CancelAgentRequest withdraws a request. With a tokenID only that token's request is found; without
// one, agentID must own the token that asked (domain.ErrNotRequestOwner otherwise).
func (s *Store) CancelAgentRequest(ctx context.Context, workspaceID, id, agentID, tokenID string) (domain.AgentRequest, error) {
	if tokenID != "" {
		if _, err := s.GetAgentRequest(ctx, workspaceID, id, tokenID); err != nil {
			return domain.AgentRequest{}, err
		}
	}
	return s.decide(ctx, workspaceID, id, func(tx pgx.Tx, item domain.AgentRequest) error {
		if tokenID == "" && (item.RequestedBy.AgentID == nil || *item.RequestedBy.AgentID != agentID) {
			return domain.ErrNotRequestOwner
		}
		_, err := tx.Exec(ctx, `UPDATE agent_requests SET status='cancelled',decided_by_agent_id=$3,decided_at=$4
			WHERE workspace_id=$1 AND id=$2`, workspaceID, id, agentID, s.now())
		return err
	})
}

// PruneAgentRequests clears the call, the edit, the saved reply snapshot and the answer of every request decided more than
// domain.AgentRequestCallRetention ago, in every workspace. The row stays as the record of who asked
// for what and who decided. serve runs it at start and daily.
func (s *Store) PruneAgentRequests(ctx context.Context) (int64, error) {
	var pruned int64
	err := s.db.Privileged(ctx, database.PrivilegeAgentRequestPrune, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE agent_requests SET body=NULL,edited_body=NULL,result=NULL,saved_reply_before=NULL,notices=NULL
			WHERE status<>'pending' AND decided_at<$1 AND (body IS NOT NULL OR edited_body IS NOT NULL OR result IS NOT NULL OR saved_reply_before IS NOT NULL OR notices IS NOT NULL)`,
			s.now().Add(-domain.AgentRequestCallRetention))
		if err != nil {
			return fmt.Errorf("prune agent requests: %w", err)
		}
		pruned = tag.RowsAffected()
		return nil
	})
	return pruned, err
}

// ConversationParticipants returns the conversation's contact address and every address already on
// it: the senders and copies of its mail, and the copies of the team's replies, all lowercased.
func (s *Store) ConversationParticipants(ctx context.Context, workspaceID, conversationID string) (string, map[string]bool, error) {
	var contact string
	known := map[string]bool{}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(lower(ct.email),'') FROM conversations c JOIN contacts ct ON ct.workspace_id=c.workspace_id AND ct.id=c.contact_id
			WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, conversationID).Scan(&contact); err != nil {
			return err
		}
		known[contact] = true
		rows, err := tx.Query(ctx, `SELECT lower(a) FROM messages m, LATERAL (
				SELECT m.email_meta->>'from' UNION ALL SELECT jsonb_array_elements_text(COALESCE(m.email_meta->'cc','[]'::jsonb))
				UNION ALL SELECT m.origin->>'from' UNION ALL SELECT jsonb_array_elements_text(COALESCE(m.origin->'cc','[]'::jsonb))
				UNION ALL SELECT unnest(COALESCE(m.cc,'{}'::text[]))) AS addresses(a)
			WHERE m.workspace_id=$1 AND m.conversation_id=$2 AND a IS NOT NULL AND m.other_contact_id IS NULL AND m.detached_at IS NULL`, workspaceID, conversationID)
		if err != nil {
			return fmt.Errorf("load conversation participants: %w", err)
		}
		addresses, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, address := range addresses {
			known[address] = true
		}
		return err
	})
	return contact, known, err
}
