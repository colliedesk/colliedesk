package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// AgentToken is a created token. Token is the raw bearer value; it exists only in this result.
type AgentToken struct {
	Token   string         `json:"token"`
	Session domain.Session `json:"session"`
}

// CreateAgentToken gives the signed-in agent a token of the given tier. A read, draft or send token
// carries the agent role ceiling whatever the creator's role is; an admin token carries the admin
// ceiling and only an admin (as this session acts now) may create one.
func (s *Service) CreateAgentToken(ctx context.Context, principal Principal, label, scope string) (AgentToken, error) {
	label, err := checkTokenRequest(label, scope)
	if err != nil {
		return AgentToken{}, err
	}
	if scope == ScopeAdmin && principal.Agent.Role != "admin" {
		return AgentToken{}, ErrAdminTokenNeedsAdmin
	}
	var created AgentToken
	err = s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		var err error
		created, err = s.insertAgentToken(ctx, tx, principal.Workspace.ID, principal.Agent.ID, label, scope)
		return err
	})
	return created, err
}

// IssueAgentToken is the operator path of CreateAgentToken: it names the workspace by slug and the
// agent by email, for installs where nobody can sign in yet.
func (s *Service) IssueAgentToken(ctx context.Context, workspaceSlug, email, label, scope string) (AgentToken, error) {
	label, err := checkTokenRequest(label, scope)
	if err != nil {
		return AgentToken{}, err
	}
	workspaceID, err := s.db.WorkspaceIDBySlug(ctx, workspaceSlug)
	if err != nil {
		return AgentToken{}, err
	}
	var created AgentToken
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var agentID, role string
		err := tx.QueryRow(ctx, `SELECT id::text,role FROM agents WHERE workspace_id=$1 AND email=lower($2)`, workspaceID, strings.TrimSpace(email)).Scan(&agentID, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no agent %q in workspace %q", email, workspaceSlug)
		}
		if err != nil {
			return fmt.Errorf("find agent: %w", err)
		}
		if scope == ScopeAdmin && role != "admin" {
			return fmt.Errorf("%w: %s has the %s role", ErrAdminTokenNeedsAdmin, email, role)
		}
		created, err = s.insertAgentToken(ctx, tx, workspaceID, agentID, label, scope)
		return err
	})
	return created, err
}

func (s *Service) insertAgentToken(ctx context.Context, tx pgx.Tx, workspaceID, agentID, label, scope string) (AgentToken, error) {
	raw, hash, err := newAgentToken()
	if err != nil {
		return AgentToken{}, err
	}
	// created_at comes from the service clock: the one-year limit is counted from it.
	now := s.now()
	session, err := scanSession(tx.QueryRow(ctx, `WITH s AS (
			INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,label,created_at,last_seen_at,expires_at)
			VALUES($1,$2,$3,$8,$4,$5,$6,$6,$7) RETURNING *)
		SELECT `+sessionColumns+` FROM s JOIN agents a ON a.id=s.agent_id AND a.workspace_id=s.workspace_id`,
		workspaceID, agentID, hash, scope, label, now, now.Add(sessionLifetime), tokenRoleCeiling(scope)))
	if err != nil {
		return AgentToken{}, fmt.Errorf("create agent token: %w", err)
	}
	return AgentToken{Token: raw, Session: session}, nil
}
