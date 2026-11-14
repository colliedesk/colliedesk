package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// An AI app connected with OAuth holds a grant: a sessions row with scope 'oauth' that acts as the person
// who allowed it, with everything they hold now (Principal.setAccess). Its id never changes, so the notes
// it writes keep naming it; its credentials rotate in oauth_tokens. docs/internals/auth.md, "OAuth grants".
const (
	// oauthCodeLifetime: an app trades its code at once; RFC 6749 allows at most ten minutes.
	oauthCodeLifetime = 5 * time.Minute
	// oauthAccessLifetime: a stolen access token works for at most an hour; the app refreshes.
	oauthAccessLifetime = time.Hour
	// oauthRefreshGrace is how long a refresh token that was just used still works: a client that lost
	// the answer to its refresh (a network error) retries with it, and two processes of one app sharing
	// a credential may refresh at once. After it, using it again is a replay and ends the grant.
	oauthRefreshGrace = 2 * time.Minute
	// oauthReplayMemory is how long a used refresh token is kept to tell a replay; later it is unknown.
	oauthReplayMemory = 24 * time.Hour
	// oauthPendingLifetime: a grant whose code nobody exchanged is cleared after it.
	oauthPendingLifetime = time.Hour
	maxOAuthLabelRunes   = 80
)

// ErrInvalidGrant is a code or refresh token that is unknown, used, expired, revoked, or for another app
// or redirect: the token endpoint answers invalid_grant and the app signs in again.
var ErrInvalidGrant = errors.New("invalid grant")

// ErrInvalidTarget is a token request for a resource other than the one the grant was allowed for.
var ErrInvalidTarget = errors.New("invalid target")

// OAuthGrantRequest is what the person allowed on the consent page, already checked against the app's
// registration (client id, redirect) and the protocol (S256 challenge).
type OAuthGrantRequest struct {
	ClientID      string
	ClientName    string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	UserAgent     string
}

// OAuthTokens is the answer of the token endpoint.
type OAuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// AllowOAuth makes a grant for the app the person allowed, in the workspace of their session, and returns
// its one-time code. Only a person's own session allows (the route is person-only); a grant from a session
// limited to a role is limited to it as well.
func (s *Service) AllowOAuth(ctx context.Context, principal Principal, request OAuthGrantRequest) (string, error) {
	if principal.IsToken() {
		return "", errors.New("only a person's session can allow an app")
	}
	label := strings.TrimSpace(request.ClientName)
	if label == "" {
		label = "AI app"
	}
	if runes := []rune(label); len(runes) > maxOAuthLabelRunes {
		label = string(runes[:maxOAuthLabelRunes])
	}
	userAgent := request.UserAgent
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, grantHash, err := newOAuthCredential(oauthGrantPrefix)
	if err != nil {
		return "", err
	}
	code, codeHash, err := newOAuthCredential(oauthCodePrefix)
	if err != nil {
		return "", err
	}
	// role_ceiling is kept for a rollback (which revokes the grant anyway).
	ceiling := "agent"
	if legacyAdminTier(principal.Effective()) {
		ceiling = "admin"
	}
	var roleID string
	if principal.LimitedTo != nil {
		roleID = principal.LimitedTo.ID
	}
	now := s.now()
	err = s.db.InWorkspace(ctx, principal.Workspace.ID, func(tx pgx.Tx) error {
		// Grants whose code nobody exchanged wrote nothing, so they go without a trace.
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE workspace_id=$1 AND agent_id=$2 AND scope='oauth' AND oauth_connected_at IS NULL AND created_at<$3`,
			principal.Workspace.ID, principal.Agent.ID, now.Add(-oauthPendingLifetime)); err != nil {
			return fmt.Errorf("clear unused grants: %w", err)
		}
		var grantID string
		err := tx.QueryRow(ctx, `INSERT INTO sessions(workspace_id,agent_id,token_hash,role_ceiling,scope,role_id,label,created_at,last_seen_at,expires_at,
				oauth_client_id,oauth_redirect_uri,oauth_resource,user_agent)
			VALUES($1,$2,$3,$4,'oauth',NULLIF($5,'')::uuid,$6,$7,$7,$8,$9,$10,$11,NULLIF($12,'')) RETURNING id::text`,
			principal.Workspace.ID, principal.Agent.ID, grantHash, ceiling, roleID, label, now, now.Add(oauthPendingLifetime),
			request.ClientID, request.RedirectURI, request.Resource, userAgent).Scan(&grantID)
		if err != nil {
			return fmt.Errorf("create grant: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO oauth_tokens(workspace_id,session_id,kind,hash,code_challenge,created_at,expires_at) VALUES($1,$2,'code',$3,$4,$5,$6)`,
			principal.Workspace.ID, grantID, codeHash, request.CodeChallenge, now, now.Add(oauthCodeLifetime))
		if err != nil {
			return fmt.Errorf("store code: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("allow app: %w", err)
	}
	return code, nil
}

// OAuthCodeExchange is the authorization_code request of the token endpoint.
type OAuthCodeExchange struct {
	Code         string
	CodeVerifier string
	ClientID     string
	RedirectURI  string
	Resource     string
}

// OAuthRefresh is the refresh_token request of the token endpoint.
type OAuthRefresh struct {
	RefreshToken string
	ClientID     string
	Resource     string
}

// oauthCredential is a row of oauth_tokens with its grant, as the token endpoint reads it.
type oauthCredential struct {
	sessionID, kind, challenge, clientID, redirectURI, resource string
	expiresAt                                                   time.Time
	replacedAt, revokedAt                                       *time.Time
	grantExpiresAt                                              time.Time
	deactivated                                                 bool
}

// ExchangeOAuthCode trades a code for the grant's first access and refresh tokens. A code works once; a
// second use ends the grant (RFC 6749 §4.1.2: the first use may have been a thief's).
func (s *Service) ExchangeOAuthCode(ctx context.Context, request OAuthCodeExchange) (OAuthTokens, error) {
	if !strings.HasPrefix(request.Code, oauthCodePrefix) {
		return OAuthTokens{}, ErrInvalidGrant
	}
	return s.useOAuthCredential(ctx, request.Code, func(tx pgx.Tx, workspaceID string, row oauthCredential, now time.Time) (bool, error) {
		if row.kind != "code" {
			return false, ErrInvalidGrant
		}
		if row.replacedAt != nil {
			return true, ErrInvalidGrant
		}
		if !now.Before(row.expiresAt) || row.clientID != request.ClientID || row.redirectURI != request.RedirectURI ||
			!pkceMatches(request.CodeVerifier, row.challenge) {
			return false, ErrInvalidGrant
		}
		if request.Resource != "" && request.Resource != row.resource {
			return false, ErrInvalidTarget
		}
		if _, err := tx.Exec(ctx, `UPDATE oauth_tokens SET replaced_at=$3 WHERE workspace_id=$1 AND hash=$2`, workspaceID, credentialHash(request.Code), now); err != nil {
			return false, fmt.Errorf("use code: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE sessions SET oauth_connected_at=$3 WHERE workspace_id=$1 AND id=$2`, workspaceID, row.sessionID, now); err != nil {
			return false, fmt.Errorf("connect grant: %w", err)
		}
		return false, nil
	})
}

// RefreshOAuth trades a refresh token for a new pair; the old one is replaced. Within oauthRefreshGrace of
// that, the old one gives another new pair and leaves the others working (a retry, or two processes of
// one app refreshing at once); after it, using it ends the grant.
func (s *Service) RefreshOAuth(ctx context.Context, request OAuthRefresh) (OAuthTokens, error) {
	if !strings.HasPrefix(request.RefreshToken, oauthRefreshPrefix) {
		return OAuthTokens{}, ErrInvalidGrant
	}
	return s.useOAuthCredential(ctx, request.RefreshToken, func(tx pgx.Tx, workspaceID string, row oauthCredential, now time.Time) (bool, error) {
		if row.kind != "refresh" || !now.Before(row.expiresAt) || (request.ClientID != "" && request.ClientID != row.clientID) {
			return false, ErrInvalidGrant
		}
		if request.Resource != "" && request.Resource != row.resource {
			return false, ErrInvalidTarget
		}
		if row.replacedAt != nil {
			if now.Sub(*row.replacedAt) > oauthRefreshGrace {
				return true, ErrInvalidGrant
			}
		} else if _, err := tx.Exec(ctx, `UPDATE oauth_tokens SET replaced_at=$3 WHERE workspace_id=$1 AND hash=$2`, workspaceID, credentialHash(request.RefreshToken), now); err != nil {
			return false, fmt.Errorf("use refresh token: %w", err)
		}
		return false, nil
	})
}

// useOAuthCredential finds the grant of raw, lets check decide (revoke: the use is a replay that ends the
// grant), then issues a new access and refresh token and slides the grant's expiry like a session's.
func (s *Service) useOAuthCredential(ctx context.Context, raw string, check func(pgx.Tx, string, oauthCredential, time.Time) (bool, error)) (OAuthTokens, error) {
	hash := credentialHash(raw)
	// Only the credential-to-workspace lookup crosses workspaces; the grant is read and changed scoped.
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeSessionToken, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM oauth_tokens WHERE hash=$1`, hash).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthTokens{}, ErrInvalidGrant
	}
	if err != nil {
		return OAuthTokens{}, fmt.Errorf("find grant: %w", err)
	}
	now := s.now()
	var tokens OAuthTokens
	var refused error
	err = s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var row oauthCredential
		err := tx.QueryRow(ctx, `SELECT t.session_id::text,t.kind,COALESCE(t.code_challenge,''),t.expires_at,t.replaced_at,
				s.oauth_client_id,s.oauth_redirect_uri,s.oauth_resource,s.revoked_at,s.expires_at,a.deactivated_at IS NOT NULL
			FROM oauth_tokens t JOIN sessions s ON s.workspace_id=t.workspace_id AND s.id=t.session_id
			JOIN agents a ON a.workspace_id=s.workspace_id AND a.id=s.agent_id
			WHERE t.workspace_id=$1 AND t.hash=$2 AND s.scope='oauth' FOR UPDATE OF t, s`, workspaceID, hash).
			Scan(&row.sessionID, &row.kind, &row.challenge, &row.expiresAt, &row.replacedAt,
				&row.clientID, &row.redirectURI, &row.resource, &row.revokedAt, &row.grantExpiresAt, &row.deactivated)
		if errors.Is(err, pgx.ErrNoRows) {
			refused = ErrInvalidGrant
			return nil
		}
		if err != nil {
			return fmt.Errorf("read grant: %w", err)
		}
		if row.revokedAt != nil || row.deactivated || !now.Before(row.grantExpiresAt) {
			refused = ErrInvalidGrant
			return nil
		}
		revoke, err := check(tx, workspaceID, row, now)
		if revoke {
			// Committed with the refusal: a replayed code or refresh token ends the grant for everyone.
			_, revokeErr := tx.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,$3) WHERE workspace_id=$1 AND id=$2`, workspaceID, row.sessionID, now)
			if revokeErr != nil {
				return fmt.Errorf("end replayed grant: %w", revokeErr)
			}
		}
		if errors.Is(err, ErrInvalidGrant) || errors.Is(err, ErrInvalidTarget) {
			refused = err
			return nil
		}
		if err != nil {
			return err
		}
		tokens, err = s.issueOAuthTokens(ctx, tx, workspaceID, row.sessionID, now)
		return err
	})
	if err != nil {
		return OAuthTokens{}, fmt.Errorf("oauth token: %w", err)
	}
	if refused != nil {
		return OAuthTokens{}, refused
	}
	return tokens, nil
}

// issueOAuthTokens stores a new access and refresh token for the grant, slides its expiry, records its
// use, and forgets credentials that can no longer be used or told apart.
func (s *Service) issueOAuthTokens(ctx context.Context, tx pgx.Tx, workspaceID, grantID string, now time.Time) (OAuthTokens, error) {
	access, accessHash, err := newOAuthCredential(oauthAccessPrefix)
	if err != nil {
		return OAuthTokens{}, err
	}
	refresh, refreshHash, err := newOAuthCredential(oauthRefreshPrefix)
	if err != nil {
		return OAuthTokens{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_tokens WHERE workspace_id=$1 AND session_id=$2
			AND (expires_at<$3 OR (kind='refresh' AND replaced_at<$4) OR (kind='code' AND replaced_at IS NOT NULL AND replaced_at<$4))`,
		workspaceID, grantID, now, now.Add(-oauthReplayMemory)); err != nil {
		return OAuthTokens{}, fmt.Errorf("forget old credentials: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_tokens(workspace_id,session_id,kind,hash,created_at,expires_at)
			VALUES($1,$2,'access',$3,$5,$6),($1,$2,'refresh',$4,$5,$7)`,
		workspaceID, grantID, accessHash, refreshHash, now, now.Add(oauthAccessLifetime), now.Add(sessionLifetime)); err != nil {
		return OAuthTokens{}, fmt.Errorf("store tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET last_seen_at=$3,expires_at=$4 WHERE workspace_id=$1 AND id=$2`,
		workspaceID, grantID, now, now.Add(sessionLifetime)); err != nil {
		return OAuthTokens{}, fmt.Errorf("slide grant: %w", err)
	}
	return OAuthTokens{AccessToken: access, RefreshToken: refresh, ExpiresIn: int(oauthAccessLifetime.Seconds())}, nil
}

// pkceMatches checks an S256 code verifier (RFC 7636: 43 to 128 unreserved characters) against the
// challenge stored with the code. Plain challenges are never accepted.
func pkceMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || challenge == "" {
		return false
	}
	for _, r := range verifier {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '_' || r == '~') {
			return false
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}
