package auth

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const linkLifetime = 15 * time.Minute
const sessionLifetime = 30 * 24 * time.Hour

type Service struct {
	db     *database.DB
	origin string
	now    func() time.Time
	// planLimits turns on the agent cap of the hosted cloud's plans; self-hosted has none.
	planLimits bool
}

type Principal struct {
	Workspace domain.Workspace
	Agent     domain.Agent
	SessionID string
	ViaCookie bool
}

func New(pool *pgxpool.Pool, origin string) *Service {
	return NewWithDB(database.New(pool), origin)
}

// NewWithDB uses db as is, for example one built with database.NewSplit.
func NewWithDB(db *database.DB, origin string) *Service {
	return &Service{db: db, origin: strings.TrimRight(origin, "/"), now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) CreateWorkspace(ctx context.Context, name, email string) (domain.Workspace, string, error) {
	slug := slugify(name)
	if slug == "" || !strings.Contains(email, "@") {
		return domain.Workspace{}, "", fmt.Errorf("name and valid admin email are required")
	}
	var workspace domain.Workspace
	var link createdLink
	err := s.db.Privileged(ctx, database.PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name) VALUES($1,$2) RETURNING id::text,slug,name,timezone`, slug, name).
			Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone); err != nil {
			return fmt.Errorf("create workspace: %w", err)
		}
		var agentID string
		if err := tx.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,lower($2),'admin') RETURNING id::text`, workspace.ID, email).Scan(&agentID); err != nil {
			return fmt.Errorf("create admin agent: %w", err)
		}
		var err error
		link, err = s.createLinkTx(ctx, tx, workspace.ID, agentID, strings.ToLower(email), "pairing", "admin", nil)
		return err
	})
	if err != nil {
		return domain.Workspace{}, "", err
	}
	return workspace, link.URL, nil
}

func (s *Service) Pair(ctx context.Context, workspaceSlug, email string) (string, error) {
	workspaceID, err := s.db.WorkspaceIDBySlug(ctx, workspaceSlug)
	if err != nil {
		return "", fmt.Errorf("find agent: %w", err)
	}
	var link createdLink
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var agentID, role string
		if err := tx.QueryRow(ctx, `SELECT id::text,role FROM agents WHERE workspace_id=$1 AND email=lower($2)`, workspaceID, email).
			Scan(&agentID, &role); err != nil {
			return fmt.Errorf("find agent: %w", err)
		}
		var err error
		link, err = s.createLinkTx(ctx, tx, workspaceID, agentID, strings.ToLower(email), "pairing", role, nil)
		return err
	})
	if err != nil {
		return "", err
	}
	return link.URL, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(value string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(value), "-"), "-")
}

func (s *Service) redeemURL(token string) string {
	return s.origin + "/redeem?token=" + url.QueryEscape(token)
}

// WithPlanLimits returns a service that refuses to add agents beyond the workspace plan's cap.
func (s *Service) WithPlanLimits(enabled bool) *Service {
	copy := *s
	copy.planLimits = enabled
	return &copy
}

// WithClock returns a service that reads now from the given clock; tests move it instead of sleeping.
func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = func() time.Time { return now().UTC() }
	return &copy
}
