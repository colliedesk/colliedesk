package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
	"github.com/jackc/pgx/v5"
)

type Redeemed struct {
	Token     string           `json:"token,omitempty"`
	Agent     domain.Agent     `json:"agent"`
	Workspace domain.Workspace `json:"workspace"`
	// ContinueURL is where a browser that signed in on APP_ORIGIN continues: its workspace's own host,
	// signed in there by a one-time token in the address's fragment. Absent everywhere else.
	ContinueURL *string `json:"continue_url,omitempty"`
}

// ErrLinkOfAnotherWorkspace refuses a one-time link on the host of a workspace it does not belong to.
var ErrLinkOfAnotherWorkspace = errors.New("the link belongs to another workspace")

// ErrHandoffOffItsHost refuses a hand-off redeemed anywhere but as a cookie session on its workspace's
// own host: it carries a browser's sign-in across, never a token a client keeps.
var ErrHandoffOffItsHost = errors.New("a hand-off signs in only as a cookie session on its workspace's host")

// expiredHandoffsPerSweep bounds how many expired hand-offs one new hand-off deletes, so a page load
// never waits on a large delete; every hand-off sweeps again.
const expiredHandoffsPerSweep = 100

// Handoff makes a one-time link that signs the principal's agent in on the workspace's own host with
// the same role, valid for a minute. It returns the token only: the caller puts it in the address's
// fragment, which no server receives or logs. It also deletes some of the workspace's expired
// hand-offs: they are worth nothing after their minute, and every page load can make one.
func (s *Service) Handoff(ctx context.Context, principal Principal) (string, error) {
	var link createdLink
	err := s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM pairing_links WHERE id IN (SELECT id FROM pairing_links
			WHERE workspace_id=$1 AND kind='handoff' AND expires_at<=$2 LIMIT $3)`, principal.Workspace.ID, s.now(), expiredHandoffsPerSweep); err != nil {
			return fmt.Errorf("delete expired hand-offs: %w", err)
		}
		var err error
		link, err = s.createLinkFor(ctx, tx, principal.Workspace.ID, principal.Agent.ID, principal.Agent.Email, "handoff", principal.Agent.Role, nil, handoffLifetime)
		return err
	})
	if err != nil {
		return "", err
	}
	return link.Token, nil
}

type createdLink struct {
	Token string
	URL   string
	ID    string
}

func (s *Service) createLinkTx(ctx context.Context, tx pgx.Tx, workspaceID, agentID, email, kind, ceiling string, creatorID *string) (createdLink, error) {
	return s.createLinkFor(ctx, tx, workspaceID, agentID, email, kind, ceiling, creatorID, linkLifetime)
}

func (s *Service) createLinkFor(ctx context.Context, tx pgx.Tx, workspaceID, agentID, email, kind, ceiling string, creatorID *string, lifetime time.Duration) (createdLink, error) {
	token, hash, err := newToken()
	if err != nil {
		return createdLink{}, err
	}
	var nullableAgent any
	if agentID != "" {
		nullableAgent = agentID
	}
	var linkID, slug string
	err = tx.QueryRow(ctx, `INSERT INTO pairing_links(workspace_id,agent_id,email,kind,role_ceiling,token_hash,created_by_agent_id,expires_at)
		VALUES($1,$2,lower($3),$4,$5,$6,$7,$8) RETURNING id::text, (SELECT slug FROM workspaces WHERE id=$1)`,
		workspaceID, nullableAgent, email, kind, ceiling, hash, creatorID, s.now().Add(lifetime)).Scan(&linkID, &slug)
	if err != nil {
		return createdLink{}, fmt.Errorf("create %s link: %w", kind, err)
	}
	return createdLink{Token: token, URL: s.redeemURL(token, slug), ID: linkID}, nil
}

// Redeem spends a one-time link for a new session; cookie tells whether the session will be a browser's
// cookie (true) or a bearer token the client keeps.
func (s *Service) Redeem(ctx context.Context, token, requestedRole, label string, cookie bool) (Redeemed, error) {
	// The link's workspace is unknown until the token hash is looked up; the redemption itself,
	// with its checks and the row lock, runs scoped to that workspace.
	var linkWorkspace string
	err := s.db.Privileged(ctx, database.PrivilegeLinkToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM pairing_links WHERE token_hash=$1`, tokenHash(token)).Scan(&linkWorkspace)
	})
	if err != nil {
		return Redeemed{}, fmt.Errorf("redeem link: %w", err)
	}
	tx, err := s.db.BeginWorkspace(ctx, linkWorkspace)
	if err != nil {
		return Redeemed{}, fmt.Errorf("begin redeem: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	now := s.now()
	var linkID, workspaceID, email, kind, ceiling, slug string
	var agentID *string
	err = tx.QueryRow(ctx, `SELECT p.id::text,p.workspace_id::text,p.agent_id::text,p.email,p.kind,p.role_ceiling,w.slug FROM pairing_links p
		JOIN workspaces w ON w.id=p.workspace_id
		WHERE p.token_hash=$1 AND p.used_at IS NULL AND p.revoked_at IS NULL AND p.expires_at>$2 FOR UPDATE OF p`, tokenHash(token), now).
		Scan(&linkID, &workspaceID, &agentID, &email, &kind, &ceiling, &slug)
	if err != nil {
		return Redeemed{}, fmt.Errorf("redeem link: %w", err)
	}
	// On a workspace's own host only that workspace's links sign in; the link stays unused.
	hostSlug, onWorkspaceHost := workspacehost.Slug(ctx)
	if onWorkspaceHost && hostSlug != slug {
		return Redeemed{}, fmt.Errorf("redeem link: %w", ErrLinkOfAnotherWorkspace)
	}
	if kind == "handoff" && (!onWorkspaceHost || !cookie) {
		return Redeemed{}, fmt.Errorf("redeem link: %w", ErrHandoffOffItsHost)
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
	if err := requireActiveAgent(ctx, tx, workspaceID, agentID, email); err != nil {
		return Redeemed{}, err
	}
	if kind == "invite" {
		if s.planLimits {
			if err := store.CheckAgentSeat(ctx, tx, workspaceID, now, email, linkID, s.plansOnSale); err != nil {
				return Redeemed{}, err
			}
		}
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

// requireActiveAgent returns domain.ErrAgentDeactivated when the link belongs to a deactivated agent:
// the agent a pairing link names, or the agent that already has the invited address.
func requireActiveAgent(ctx context.Context, tx pgx.Tx, workspaceID string, agentID *string, email string) error {
	// FOR SHARE makes this check and the session insert one step against a deactivation: that holds the
	// row exclusively until it commits, so a redeem that started first finishes first and its session is
	// revoked afterwards, and one that starts later sees the flag.
	var deactivated bool
	err := tx.QueryRow(ctx, `SELECT deactivated_at IS NOT NULL FROM agents WHERE workspace_id=$1
		AND (id::text=$2 OR ($2='' AND email=lower($3))) FOR SHARE`, workspaceID, agentRef(agentID), email).Scan(&deactivated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check agent state: %w", err)
	}
	if deactivated {
		return domain.ErrAgentDeactivated
	}
	return nil
}

func agentRef(agentID *string) string {
	if agentID == nil {
		return ""
	}
	return *agentID
}
