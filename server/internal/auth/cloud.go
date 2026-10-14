package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrCloudAgentNotFound = errors.New("cloud identity is not an existing agent")

type WorkspaceChoice struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type cloudAgent struct {
	WorkspaceID   string
	WorkspaceSlug string
	WorkspaceName string
	AgentID       string
	Email         string
	Role          string
}

func (s *Service) ExchangeCloud(ctx context.Context, email, workspaceSlug, label string) (Redeemed, []WorkspaceChoice, error) {
	agents, err := s.cloudAgents(ctx, email)
	if err != nil {
		return Redeemed{}, nil, err
	}
	if len(agents) == 0 {
		return Redeemed{}, nil, ErrCloudAgentNotFound
	}
	if workspaceSlug == "" && len(agents) > 1 {
		choices := make([]WorkspaceChoice, 0, len(agents))
		for _, agent := range agents {
			choices = append(choices, WorkspaceChoice{Slug: agent.WorkspaceSlug, Name: agent.WorkspaceName})
		}
		return Redeemed{}, choices, nil
	}
	selected := agents[0]
	if workspaceSlug != "" {
		found := false
		for _, agent := range agents {
			if agent.WorkspaceSlug == workspaceSlug {
				selected, found = agent, true
				break
			}
		}
		if !found {
			return Redeemed{}, nil, ErrCloudAgentNotFound
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Redeemed{}, nil, fmt.Errorf("begin cloud pairing: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	link, err := s.createLinkTx(ctx, tx, selected.WorkspaceID, selected.AgentID, selected.Email, "pairing", selected.Role, nil)
	if err != nil {
		return Redeemed{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Redeemed{}, nil, fmt.Errorf("commit cloud pairing: %w", err)
	}
	redeemed, err := s.Redeem(ctx, link.Token, "", label)
	return redeemed, nil, err
}

func (s *Service) cloudAgents(ctx context.Context, email string) ([]cloudAgent, error) {
	rows, err := s.pool.Query(ctx, `SELECT w.id::text,w.slug,w.name,a.id::text,a.email,a.role
		FROM agents a JOIN workspaces w ON w.id=a.workspace_id
		WHERE lower(a.email)=lower($1) ORDER BY w.slug`, strings.TrimSpace(email))
	if err != nil {
		return nil, fmt.Errorf("find cloud agents: %w", err)
	}
	defer rows.Close()
	agents := make([]cloudAgent, 0)
	for rows.Next() {
		var agent cloudAgent
		if err := rows.Scan(&agent.WorkspaceID, &agent.WorkspaceSlug, &agent.WorkspaceName, &agent.AgentID, &agent.Email, &agent.Role); err != nil {
			return nil, fmt.Errorf("scan cloud agent: %w", err)
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud agents: %w", err)
	}
	return agents, nil
}
