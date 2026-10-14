package auth

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

type Redeemed struct {
	Token     string           `json:"token,omitempty"`
	Agent     domain.Agent     `json:"agent"`
	Workspace domain.Workspace `json:"workspace"`
}

type createdLink struct {
	Token string
	URL   string
	ID    string
}

func (s *Service) createLinkTx(ctx context.Context, tx pgx.Tx, workspaceID, agentID, email, kind, ceiling string, creatorID *string) (createdLink, error) {
	token, hash, err := newToken()
	if err != nil {
		return createdLink{}, err
	}
	var nullableAgent any
	if agentID != "" {
		nullableAgent = agentID
	}
	var linkID string
	err = tx.QueryRow(ctx, `INSERT INTO pairing_links(workspace_id,agent_id,email,kind,role_ceiling,token_hash,created_by_agent_id,expires_at)
		VALUES($1,$2,lower($3),$4,$5,$6,$7,$8) RETURNING id::text`, workspaceID, nullableAgent, email, kind, ceiling, hash, creatorID, s.now().Add(linkLifetime)).Scan(&linkID)
	if err != nil {
		return createdLink{}, fmt.Errorf("create %s link: %w", kind, err)
	}
	return createdLink{Token: token, URL: s.redeemURL(token), ID: linkID}, nil
}

func (s *Service) Redeem(ctx context.Context, token, requestedRole, label string) (Redeemed, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Redeemed{}, fmt.Errorf("begin redeem: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	now := s.now()
	var linkID, workspaceID, email, kind, ceiling string
	var agentID *string
	err = tx.QueryRow(ctx, `SELECT id::text,workspace_id::text,agent_id::text,email,kind,role_ceiling FROM pairing_links
		WHERE token_hash=$1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at>$2 FOR UPDATE`, tokenHash(token), now).
		Scan(&linkID, &workspaceID, &agentID, &email, &kind, &ceiling)
	if err != nil {
		return Redeemed{}, fmt.Errorf("redeem link: %w", err)
	}
	role := ceiling
	if requestedRole != "" {
		if requestedRole != "agent" && requestedRole != "admin" {
			return Redeemed{}, fmt.Errorf("invalid requested role")
		}
		if ceiling == "agent" && requestedRole == "admin" {
			return Redeemed{}, fmt.Errorf("requested role exceeds invite ceiling")
		}
		role = requestedRole
	}
	if kind == "invite" {
		var created string
		if err := tx.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,lower($2),$3)
			ON CONFLICT(workspace_id,email) DO UPDATE SET role=CASE WHEN agents.role='admin' THEN agents.role ELSE EXCLUDED.role END
			RETURNING id::text`, workspaceID, email, role).Scan(&created); err != nil {
			return Redeemed{}, fmt.Errorf("create invited agent: %w", err)
		}
		agentID = &created
	}
	if agentID == nil {
		return Redeemed{}, fmt.Errorf("pairing link has no agent")
	}
	if _, err := tx.Exec(ctx, `UPDATE pairing_links SET used_at=$2 WHERE id=$1`, linkID, now); err != nil {
		return Redeemed{}, fmt.Errorf("consume link: %w", err)
	}
	sessionToken, sessionHash, err := newToken()
	if err != nil {
		return Redeemed{}, err
	}
	if label == "" {
		label = "Browser session"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,label,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, workspaceID, *agentID, sessionHash, role, label, now.Add(sessionLifetime)); err != nil {
		return Redeemed{}, fmt.Errorf("create session: %w", err)
	}
	result, err := scanRedeemed(ctx, tx, workspaceID, *agentID)
	if err != nil {
		return Redeemed{}, err
	}
	result.Agent.Role = effectiveRole(result.Agent.Role, role)
	result.Token = sessionToken
	if err := tx.Commit(ctx); err != nil {
		return Redeemed{}, fmt.Errorf("commit redeem: %w", err)
	}
	return result, nil
}

func scanRedeemed(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) (Redeemed, error) {
	var result Redeemed
	err := tx.QueryRow(ctx, `SELECT w.id::text,w.slug,w.name,w.timezone,a.id::text,a.email,a.display_name,a.role,a.timezone_override
		FROM workspaces w JOIN agents a ON a.workspace_id=w.id WHERE w.id=$1 AND a.id=$2`, workspaceID, agentID).
		Scan(&result.Workspace.ID, &result.Workspace.Slug, &result.Workspace.Name, &result.Workspace.Timezone,
			&result.Agent.ID, &result.Agent.Email, &result.Agent.DisplayName, &result.Agent.Role, &result.Agent.TimezoneOverride)
	if err != nil {
		return Redeemed{}, fmt.Errorf("load redeemed identity: %w", err)
	}
	return result, nil
}
