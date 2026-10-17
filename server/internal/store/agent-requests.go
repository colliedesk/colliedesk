package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// agentRequestSelect returns requests with the token's state and the names of the people involved.
// A token row that is gone counts as revoked.
const agentRequestSelect = `SELECT r.id::text,r.kind,r.status,r.method,r.route,r.path,r.body,r.summary,r.conversation_id::text,
	r.token_id::text,r.token_label,r.token_scope,(t.id IS NULL OR t.revoked_at IS NOT NULL),r.requested_by_agent_id::text,COALESCE(NULLIF(ra.display_name,''),ra.email),
	r.decided_by_agent_id::text,COALESCE(NULLIF(da.display_name,''),da.email),r.decided_at,r.reason,r.result_status,r.result,r.result_note,r.created_at,r.expires_at
	FROM agent_requests r
	LEFT JOIN sessions t ON t.workspace_id=r.workspace_id AND t.id=r.token_id
	LEFT JOIN agents ra ON ra.workspace_id=r.workspace_id AND ra.id=r.requested_by_agent_id
	LEFT JOIN agents da ON da.workspace_id=r.workspace_id AND da.id=r.decided_by_agent_id`

var requestID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func scanAgentRequest(row pgx.Row) (domain.AgentRequest, error) {
	var item domain.AgentRequest
	var body, result []byte
	var resultStatus *int32
	var deciderID, deciderName *string
	err := row.Scan(&item.ID, &item.Kind, &item.Status, &item.Method, &item.Route, &item.Path, &body, &item.Summary, &item.ConversationID,
		&item.RequestedBy.TokenID, &item.RequestedBy.TokenLabel, &item.RequestedBy.TokenScope, &item.RequestedBy.TokenRevoked,
		&item.RequestedBy.AgentID, &item.RequestedBy.AgentName, &deciderID, &deciderName, &item.DecidedAt, &item.Reason,
		&resultStatus, &result, &item.ResultNote, &item.CreatedAt, &item.ExpiresAt)
	if err != nil {
		return domain.AgentRequest{}, err
	}
	item.Body, item.Result = body, result
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
	query := agentRequestSelect + ` WHERE r.workspace_id=$1 AND r.id=$2`
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
		var body any
		if len(in.Body) > 0 {
			body = string(in.Body)
		}
		now := s.now()
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,requested_by_agent_id,kind,method,route,path,body,summary,conversation_id,created_at,expires_at)
			SELECT $1,s.id,s.label,s.scope,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12 FROM sessions s
			WHERE s.workspace_id=$1 AND s.id=$2 AND s.scope IS NOT NULL
			RETURNING id::text`, workspaceID, in.TokenID, in.AgentID, in.Kind, in.Method, in.Route, in.Path, body, in.Summary,
			in.ConversationID, now, now.Add(domain.AgentRequestLifetime)).Scan(&id)
		if err != nil {
			return domain.AgentRequest{}, fmt.Errorf("insert agent request: %w", err)
		}
		return getAgentRequest(ctx, tx, workspaceID, id, false)
	})
}

// AgentRequestFilter narrows a list. TokenID, when set, keeps only that token's requests.
type AgentRequestFilter struct {
	Status  string
	TokenID string
}

// maxListedAgentRequests bounds one list; pending requests come first.
const maxListedAgentRequests = 200

// ListAgentRequests returns the workspace's requests, pending first, then newest first.
func (s *Store) ListAgentRequests(ctx context.Context, workspaceID string, filter AgentRequestFilter) ([]domain.AgentRequest, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.AgentRequest, error) {
		if err := s.expireAgentRequests(ctx, tx, workspaceID); err != nil {
			return nil, err
		}
		query := agentRequestSelect + ` WHERE r.workspace_id=$1`
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
// approval (a double click, a second tab) finds it decided and runs nothing. A non-nil body replaces
// the stored call's body (an edited reply draft). A request of a revoked token is refused.
func (s *Store) ClaimAgentRequest(ctx context.Context, workspaceID, id, agentID string, body json.RawMessage) (domain.AgentRequest, error) {
	return s.decide(ctx, workspaceID, id, func(tx pgx.Tx, item domain.AgentRequest) error {
		if item.RequestedBy.TokenRevoked {
			return domain.ErrAgentRequestTokenRevoked
		}
		var replacement any
		if body != nil {
			replacement = string(body)
		}
		_, err := tx.Exec(ctx, `UPDATE agent_requests SET status='approved',decided_by_agent_id=$3,decided_at=$4,body=COALESCE($5::jsonb,body)
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
