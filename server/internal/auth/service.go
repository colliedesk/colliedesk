package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const linkLifetime = 15 * time.Minute

// handoffLifetime bounds a hand-off from APP_ORIGIN to a workspace's host: the browser follows it at once.
const handoffLifetime = time.Minute
const sessionLifetime = 30 * 24 * time.Hour

type Service struct {
	db     *database.DB
	origin string
	// domain is HELP_CENTER_DOMAIN: links for a workspace's agents name its own host below it.
	domain string
	now    func() time.Time
	// planLimits turns on the agent cap of the hosted cloud's plans; self-hosted has none.
	plansOnSale []string
	planLimits  bool
	// random is the entropy for generated slug suffixes; nil reads crypto/rand.
	random io.Reader
	// signupKey keys the hashes of the cloud sign-up records (SIGNUP_HASH_KEY).
	signupKey []byte
}

type Principal struct {
	Workspace domain.Workspace
	Agent     domain.Agent
	SessionID string
	ViaCookie bool
	// Scope is the tier of an agent token (read, draft, send or admin), custom for a token that holds
	// its own list of Permissions; empty for a full session. Ask Allows, not Scope, what a token may do.
	Scope string
	// Permissions is a custom token's list; nil for a preset token and a full session.
	Permissions []string
	// Access is the workspace's plan at the time of the request, derived inside the transaction that
	// authenticated the session so the lock check costs no transaction of its own. The zero value
	// (self-hosted, no plan limits) is never locked.
	Access domain.Access
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
		var agentID string
		var created bool
		var err error
		workspace, agentID, created, err = s.insertWorkspaceTx(ctx, tx, slug, name, email)
		if err != nil {
			return err
		}
		if !created {
			return fmt.Errorf("create workspace: the name %q is already used by another workspace", slug)
		}
		link, err = s.createLinkTx(ctx, tx, workspace.ID, agentID, strings.ToLower(email), "pairing", "admin", nil)
		return err
	})
	if err != nil {
		return domain.Workspace{}, "", err
	}
	return workspace, link.URL, nil
}

// insertWorkspaceTx creates a workspace on a fresh trial and its first admin inside tx. created is
// false, and nothing is written, when the slug is already used by another workspace.
func (s *Service) insertWorkspaceTx(ctx context.Context, tx pgx.Tx, slug, name, email string) (workspace domain.Workspace, agentID string, created bool, err error) {
	err = tx.QueryRow(ctx, `INSERT INTO workspaces(slug,name,trial_ends_at) VALUES($1,$2,$3) ON CONFLICT (slug) DO NOTHING
		RETURNING id::text,slug,name,timezone`, slug, name, s.now().Add(domain.TrialLength)).
		Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Workspace{}, "", false, nil
	}
	if err != nil {
		return domain.Workspace{}, "", false, fmt.Errorf("create workspace: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO agents(workspace_id,email,role) VALUES($1,lower($2),'admin') RETURNING id::text`, workspace.ID, email).Scan(&agentID); err != nil {
		return domain.Workspace{}, "", false, fmt.Errorf("create admin agent: %w", err)
	}
	return workspace, agentID, true, nil
}

func (s *Service) Pair(ctx context.Context, workspaceSlug, email string) (string, error) {
	workspaceID, err := s.db.WorkspaceIDBySlug(ctx, workspaceSlug)
	if err != nil {
		return "", fmt.Errorf("find agent: %w", err)
	}
	var link createdLink
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var agentID, role string
		if err := tx.QueryRow(ctx, `SELECT id::text,role FROM agents WHERE workspace_id=$1 AND email=lower($2) AND deactivated_at IS NULL`, workspaceID, email).
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

// redeemURL is the sign-in address of a one-time link, on the workspace's own host when it has one.
func (s *Service) redeemURL(token, slug string) string {
	return workspacehost.Origin(s.origin, s.domain, slug) + "/sign-in?token=" + url.QueryEscape(token)
}

// WithWorkspaceHosts returns a service whose links name each workspace's host below domain
// (HELP_CENTER_DOMAIN); empty keeps every link on the origin.
func (s *Service) WithWorkspaceHosts(domain string) *Service {
	copy := *s
	copy.domain = domain
	return &copy
}

// WithPlanLimits returns a service that refuses to add agents beyond the workspace plan's cap.
func (s *Service) WithPlanLimits(enabled bool) *Service {
	copy := *s
	copy.planLimits = enabled
	return &copy
}

// WithPlansOnSale returns a service whose seat refusals point to the next plan up among plans.
func (s *Service) WithPlansOnSale(plans []string) *Service {
	copy := *s
	copy.plansOnSale = plans
	return &copy
}

// WithClock returns a service that reads now from the given clock; tests move it instead of sleeping.
func (s *Service) WithClock(now func() time.Time) *Service {
	copy := *s
	copy.now = func() time.Time { return now().UTC() }
	return &copy
}

// WithRandom returns a service that draws generated slug suffixes from r; tests pin it.
func (s *Service) WithRandom(r io.Reader) *Service {
	copy := *s
	copy.random = r
	return &copy
}

// WithSignupKey returns a service that keys the cloud sign-up record hashes with key.
func (s *Service) WithSignupKey(key string) *Service {
	copy := *s
	copy.signupKey = []byte(key)
	return &copy
}
