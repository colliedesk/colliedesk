package auth

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

const linkLifetime = 15 * time.Minute
const sessionLifetime = 30 * 24 * time.Hour

type Service struct {
	pool   *pgxpool.Pool
	origin string
	now    func() time.Time
}

type Principal struct {
	Workspace domain.Workspace
	Agent     domain.Agent
	SessionID string
	ViaCookie bool
}

func New(pool *pgxpool.Pool, origin string) *Service {
	return &Service{pool: pool, origin: strings.TrimRight(origin, "/"), now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) CreateWorkspace(ctx context.Context, name, email string) (domain.Workspace, string, error) {
	slug := slugify(name)
	if slug == "" || !strings.Contains(email, "@") {
		return domain.Workspace{}, "", fmt.Errorf("name and valid admin email are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Workspace{}, "", fmt.Errorf("begin workspace creation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var workspace domain.Workspace
	if err := tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$2) RETURNING id::text,slug,name,timezone`, slug, name).
		Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone); err != nil {
		return domain.Workspace{}, "", fmt.Errorf("create workspace: %w", err)
	}
	var agentID string
	if err := tx.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,lower($2),'admin') RETURNING id::text`, workspace.ID, email).Scan(&agentID); err != nil {
		return domain.Workspace{}, "", fmt.Errorf("create admin agent: %w", err)
	}
	link, _, err := s.createLinkTx(ctx, tx, workspace.ID, agentID, strings.ToLower(email), "pairing", "admin", nil)
	if err != nil {
		return domain.Workspace{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Workspace{}, "", fmt.Errorf("commit workspace creation: %w", err)
	}
	return workspace, link, nil
}

func (s *Service) Pair(ctx context.Context, workspaceSlug, email string) (string, error) {
	var workspaceID, agentID, role string
	err := s.pool.QueryRow(ctx, `SELECT w.id::text,a.id::text,a.role FROM workspaces w JOIN agents a ON a.workspace_id=w.id WHERE w.slug=$1 AND a.email=lower($2)`, workspaceSlug, email).
		Scan(&workspaceID, &agentID, &role)
	if err != nil {
		return "", fmt.Errorf("find agent: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin pairing: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	link, _, err := s.createLinkTx(ctx, tx, workspaceID, agentID, strings.ToLower(email), "pairing", role, nil)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit pairing: %w", err)
	}
	return link, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(value), "-"), "-")
}

func (s *Service) redeemURL(token string) string {
	return s.origin + "/redeem?token=" + url.QueryEscape(token)
}
