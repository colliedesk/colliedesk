package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5"
)

// ScopeOperator is the tier of an operator token. It is not in ScopeNames: those tiers are ordered inside a
// workspace (read < draft < send < admin), and no workspace token ever reaches an operator route.
const ScopeOperator = "operator"

// PermissionOperator marks a route of the operator console: only an operator's session or token reaches it.
const PermissionOperator = "operator"

// What an operator token may do. Reading is always granted; the rest become requests a person approves.
const (
	PermOperatorRead           = "operator:read"
	PermOperatorRequestsCancel = "operator_requests:cancel"
	PermOperatorConfigWrite    = "operator_config:write"
	PermOperatorManage         = "operator:manage"
)

// OperatorPermissions lists every permission an operator token can hold.
var OperatorPermissions = []string{PermOperatorRead, PermOperatorRequestsCancel, PermOperatorConfigWrite, PermOperatorManage}

const (
	operatorLinkLifetime    = 15 * time.Minute
	operatorTicketLifetime  = 10 * time.Minute
	operatorCookieLifetime  = 12 * time.Hour
	operatorTokenLifetime   = 30 * 24 * time.Hour
	operatorReauthLifetime  = 10 * time.Minute
	operatorFreshSession    = 15 * time.Minute
	operatorWrongCodesLimit = 10
	operatorLockout         = 15 * time.Minute
	operatorTokenPrefix     = "cdop_"
	operatorTokenHashDomain = "colliedesk-operator-token\x00"
	operatorTOTPIssuer      = "Colliedesk"
)

// Errors of the operator sign-in; none carries a value.
var (
	ErrOperatorLink       = errors.New("operator link is not valid")
	ErrOperatorLinkExpire = errors.New("operator link expired")
	ErrOperatorLinkUsed   = errors.New("operator link was used")
	ErrOperatorCode       = errors.New("authenticator code is wrong")
	ErrOperatorLocked     = errors.New("operator sign-in is locked")
	ErrOperatorReauth     = errors.New("this change needs your authenticator code")
	ErrOperatorReauthCLI  = errors.New("this change needs a session started in the last 15 minutes")
	ErrOperatorNoRing     = errors.New("authenticator keys can be stored only with MAILBOX_TOKEN_KEYS set")
)

// Operator is a person who runs the server.
type Operator struct {
	ID            string     `json:"id"`
	Email         string     `json:"email"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     string     `json:"created_by"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	TOTPEnabledAt *time.Time `json:"totp_enabled_at,omitempty"`
	LinkUnused    bool       `json:"link_unused"`
}

// OperatorSession is a session (a browser, 12 hours) or a token (30 days) of an operator.
type OperatorSession struct {
	ID          string     `json:"id"`
	OperatorID  string     `json:"operator_id"`
	Email       string     `json:"email"`
	Kind        string     `json:"kind"`
	Label       string     `json:"label"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// OperatorPrincipal is who an operator request comes from.
type OperatorPrincipal struct {
	Operator    Operator
	SessionID   string
	Kind        string // cookie or token
	Permissions []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ReauthAt    *time.Time
	ViaCookie   bool
}

// IsToken reports whether the principal is an operator token (an AI agent), not a person's session.
func (p OperatorPrincipal) IsToken() bool { return p.Kind == "token" }

// Allows reports whether the principal may do what permission allows: a person's session everything, a
// token what it was given (reading always).
func (p OperatorPrincipal) Allows(permission string) bool {
	if !p.IsToken() || permission == PermOperatorRead {
		return true
	}
	return slices.Contains(p.Permissions, permission)
}

// OperatorService signs operators in and keeps their sessions.
type OperatorService struct {
	db *database.DB
	// keys seals authenticator keys (MAILBOX_TOKEN_KEYS).
	keys tokenseal.Keys
	// consoleBase is where links point: OPERATOR_ORIGIN, or APP_ORIGIN/operator.
	consoleBase string
	now         func() time.Time
	// requireTOTP reads operator.require_totp at each sign-in.
	requireTOTP func() bool
}

// NewOperatorService makes the service; consoleBase is the console's address without a trailing slash.
func NewOperatorService(db *database.DB, keys tokenseal.Keys, consoleBase string, requireTOTP func() bool) *OperatorService {
	if requireTOTP == nil {
		requireTOTP = func() bool { return false }
	}
	return &OperatorService{db: db, keys: keys, consoleBase: strings.TrimRight(consoleBase, "/"), now: func() time.Time { return time.Now().UTC() }, requireTOTP: requireTOTP}
}

// WithClock replaces the clock, for tests.
func (s *OperatorService) WithClock(now func() time.Time) *OperatorService {
	copied := *s
	copied.now = now
	return &copied
}

// ConsoleBase is where the console's links point.
func (s *OperatorService) ConsoleBase() string { return s.consoleBase }

func (s *OperatorService) privileged(ctx context.Context, fn func(pgx.Tx) error) error {
	return s.db.Privileged(ctx, database.PrivilegeOperator, fn)
}

func operatorTokenHash(raw string) []byte {
	if strings.HasPrefix(raw, operatorTokenPrefix) {
		hash := sha256.Sum256([]byte(operatorTokenHashDomain + raw))
		return hash[:]
	}
	return tokenHash(raw)
}

func operatorAudit(ctx context.Context, tx pgx.Tx, actorKind, actor, action string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO operator_audit(actor_kind, actor, action, detail) VALUES($1,$2,$3,$4)`, actorKind, actor, action, encoded)
	return err
}

// Pair makes a one-time sign-in link for email, creating the operator or opening a revoked one again. With
// resetTOTP the link replaces the operator's authenticator: the shell is the root of trust.
func (s *OperatorService) Pair(ctx context.Context, email string, resetTOTP bool, by string) (string, error) {
	return s.pair(ctx, email, resetTOTP, "cli", by)
}

func (s *OperatorService) pair(ctx context.Context, email string, resetTOTP bool, kind, by string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") || len(email) < 3 {
		return "", errors.New("an operator needs an email address")
	}
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	purpose := "sign-in"
	if resetTOTP {
		purpose = "reset-totp"
	}
	err = s.privileged(ctx, func(tx pgx.Tx) error {
		var id string
		var revoked *time.Time
		err := tx.QueryRow(ctx, `SELECT id, revoked_at FROM operators WHERE email=$1 FOR UPDATE`, email).Scan(&id, &revoked)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `INSERT INTO operators(email, created_by) VALUES($1,$2) RETURNING id`, email, by).Scan(&id); err != nil {
				return fmt.Errorf("add operator: %w", err)
			}
		case err != nil:
			return err
		case revoked != nil:
			if _, err := tx.Exec(ctx, `UPDATE operators SET revoked_at=NULL, failed_codes=0, locked_until=NULL WHERE id=$1`, id); err != nil {
				return err
			}
			if err := operatorAudit(ctx, tx, kind, by, "operator.restore", map[string]any{"operator": email}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO operator_links(operator_id, token_hash, purpose, expires_at, created_by) VALUES($1,$2,$3,$4,$5)`,
			id, hash, purpose, s.now().Add(operatorLinkLifetime), by); err != nil {
			return fmt.Errorf("make operator link: %w", err)
		}
		return operatorAudit(ctx, tx, kind, by, "operator.pair", map[string]any{"operator": email, "reset_totp": resetTOTP})
	})
	if err != nil {
		return "", err
	}
	return s.consoleBase + "/sign-in?token=" + raw, nil
}

// SignInStep is where a sign-in stands after a link or a code.
type SignInStep struct {
	// Step is "code" (enter the authenticator code), "enroll" (add the key, then a code) or "done".
	Step        string
	Ticket      string
	Email       string
	Secret      []byte
	OTPAuthURL  string
	Session     string
	LockedUntil *time.Time
}

// Redeem uses a one-time link. Without an authenticator required or set, it signs the operator in at once;
// otherwise it returns a ticket for the code (and the key to add, the first time).
func (s *OperatorService) Redeem(ctx context.Context, raw string) (SignInStep, error) {
	var step SignInStep
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var linkID, operatorID, purpose, email string
		var expires time.Time
		var used, revoked, locked, enabled *time.Time
		err := tx.QueryRow(ctx, `SELECT l.id, l.operator_id, l.purpose, l.expires_at, l.used_at, o.email, o.revoked_at, o.locked_until, o.totp_enabled_at
			FROM operator_links l JOIN operators o ON o.id = l.operator_id
			WHERE l.token_hash=$1 AND l.purpose IN ('sign-in','reset-totp') FOR UPDATE OF l`, tokenHash(raw)).
			Scan(&linkID, &operatorID, &purpose, &expires, &used, &email, &revoked, &locked, &enabled)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrOperatorLink
		case err != nil:
			return err
		case revoked != nil:
			return ErrOperatorLink
		case used != nil:
			return ErrOperatorLinkUsed
		case !s.now().Before(expires):
			return ErrOperatorLinkExpire
		}
		step.Email = email
		if locked != nil && s.now().Before(*locked) {
			// The link stays usable: it works again once the lock ends, within its 15 minutes.
			step.LockedUntil = locked
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE operator_links SET used_at=$2 WHERE id=$1`, linkID, s.now()); err != nil {
			return err
		}
		reset := purpose == "reset-totp"
		if !reset && enabled == nil && !s.requireTOTP() {
			step.Step = "done"
			step.Session, err = s.startSession(ctx, tx, operatorID, email, "")
			return err
		}
		ticket, ticketHash, err := newToken()
		if err != nil {
			return err
		}
		var sealed []byte
		var keyID *string
		step.Step, step.Ticket = "code", ticket
		if reset || enabled == nil {
			if !s.keys.Configured() {
				return ErrOperatorNoRing
			}
			secret, err := newTOTPSecret()
			if err != nil {
				return err
			}
			id, box, err := s.keys.Seal(secret, "operator-totp:"+operatorID)
			if err != nil {
				return err
			}
			sealed, keyID = box, &id
			step.Step, step.Secret, step.OTPAuthURL = "enroll", secret, otpauthURL(operatorTOTPIssuer, email, secret)
		}
		_, err = tx.Exec(ctx, `INSERT INTO operator_links(operator_id, token_hash, purpose, expires_at, created_by, enroll_sealed, enroll_key_id)
			VALUES($1,$2,'totp-ticket',$3,'sign-in',$4,$5)`, operatorID, ticketHash, s.now().Add(operatorTicketLifetime), sealed, keyID)
		return err
	})
	if err == nil && step.LockedUntil != nil {
		return step, ErrOperatorLocked
	}
	return step, err
}

// ConfirmTOTP checks the code of a ticket and signs the operator in. A wrong code counts toward the lock
// (10 wrong codes lock sign-in for 15 minutes) and keeps the ticket for another try.
func (s *OperatorService) ConfirmTOTP(ctx context.Context, ticket, code string) (SignInStep, int, error) {
	var step SignInStep
	left := 0
	var wrong bool
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var linkID, operatorID, email string
		var expires time.Time
		var used, locked *time.Time
		var enrollSealed, totpSealed []byte
		var enrollKeyID, totpKeyID *string
		var lastStep int64
		var failed int
		err := tx.QueryRow(ctx, `SELECT l.id, o.id, o.email, l.expires_at, l.used_at, o.locked_until, l.enroll_sealed, l.enroll_key_id, o.totp_sealed, o.totp_key_id, o.totp_last_step, o.failed_codes
			FROM operator_links l JOIN operators o ON o.id = l.operator_id
			WHERE l.token_hash=$1 AND l.purpose='totp-ticket' AND o.revoked_at IS NULL FOR UPDATE OF l, o`, tokenHash(ticket)).
			Scan(&linkID, &operatorID, &email, &expires, &used, &locked, &enrollSealed, &enrollKeyID, &totpSealed, &totpKeyID, &lastStep, &failed)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return ErrOperatorLink
		case err != nil:
			return err
		case used != nil:
			return ErrOperatorLinkUsed
		case !s.now().Before(expires):
			return ErrOperatorLinkExpire
		}
		step.Email = email
		if locked != nil && s.now().Before(*locked) {
			step.LockedUntil = locked
			return nil
		}
		sealed, keyID := totpSealed, totpKeyID
		if enrollSealed != nil {
			sealed, keyID, lastStep = enrollSealed, enrollKeyID, 0
		}
		if sealed == nil || keyID == nil {
			return ErrOperatorLink
		}
		secret, err := s.keys.Open(*keyID, sealed, "operator-totp:"+operatorID)
		if err != nil {
			return fmt.Errorf("open the authenticator key: %w", err)
		}
		matched, ok := matchTOTP(secret, code, s.now(), lastStep)
		if !ok {
			wrong = true
			failed++
			var lockUntil *time.Time
			if failed >= operatorWrongCodesLimit {
				until := s.now().Add(operatorLockout)
				lockUntil, failed = &until, 0
				step.LockedUntil = lockUntil
				if err := operatorAudit(ctx, tx, "operator", email, "operator.locked", nil); err != nil {
					return err
				}
			}
			left = operatorWrongCodesLimit - failed
			_, err := tx.Exec(ctx, `UPDATE operators SET failed_codes=$2, locked_until=coalesce($3, locked_until) WHERE id=$1`, operatorID, failed, lockUntil)
			return err
		}
		if enrollSealed != nil {
			if _, err := tx.Exec(ctx, `UPDATE operators SET totp_sealed=$2, totp_key_id=$3, totp_enabled_at=$4 WHERE id=$1`, operatorID, enrollSealed, *enrollKeyID, s.now()); err != nil {
				return err
			}
			if err := operatorAudit(ctx, tx, "operator", email, "operator.totp_enabled", nil); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE operators SET totp_last_step=$2, failed_codes=0, locked_until=NULL WHERE id=$1`, operatorID, matched); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE operator_links SET used_at=$2 WHERE id=$1`, linkID, s.now()); err != nil {
			return err
		}
		step.Step = "done"
		step.Session, err = s.startSession(ctx, tx, operatorID, email, "reauth")
		return err
	})
	switch {
	case err != nil:
		return step, left, err
	case step.LockedUntil != nil:
		return step, 0, ErrOperatorLocked
	case wrong:
		return step, left, ErrOperatorCode
	}
	return step, left, nil
}

// startSession opens a 12-hour browser session; reauth starts it as freshly re-authenticated.
func (s *OperatorService) startSession(ctx context.Context, tx pgx.Tx, operatorID, email, reauth string) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	var reauthAt *time.Time
	if reauth != "" {
		now := s.now()
		reauthAt = &now
	}
	if _, err := tx.Exec(ctx, `INSERT INTO operator_sessions(operator_id, token_hash, kind, label, expires_at, reauth_at) VALUES($1,$2,'cookie','Browser',$3,$4)`,
		operatorID, hash, s.now().Add(operatorCookieLifetime), reauthAt); err != nil {
		return "", fmt.Errorf("start operator session: %w", err)
	}
	return raw, operatorAudit(ctx, tx, "operator", email, "operator.sign_in", nil)
}

// Authenticate finds the operator of a session or token; every refusal wraps ErrInvalidCredential.
func (s *OperatorService) Authenticate(ctx context.Context, raw string, viaCookie bool) (OperatorPrincipal, error) {
	if raw == "" {
		return OperatorPrincipal{}, fmt.Errorf("%w: missing credential", ErrInvalidCredential)
	}
	if viaCookie && strings.HasPrefix(raw, operatorTokenPrefix) {
		return OperatorPrincipal{}, fmt.Errorf("%w: a token is not a cookie", ErrInvalidCredential)
	}
	var principal OperatorPrincipal
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `UPDATE operator_sessions s SET last_seen_at=$2
			FROM operators o
			WHERE s.token_hash=$1 AND o.id = s.operator_id AND s.revoked_at IS NULL AND o.revoked_at IS NULL AND s.expires_at > $2
			RETURNING s.id, s.kind, s.permissions, s.created_at, s.expires_at, s.reauth_at, o.id, o.email, o.created_at, o.created_by, o.totp_enabled_at`,
			operatorTokenHash(raw), s.now()).
			Scan(&principal.SessionID, &principal.Kind, &principal.Permissions, &principal.CreatedAt, &principal.ExpiresAt, &principal.ReauthAt,
				&principal.Operator.ID, &principal.Operator.Email, &principal.Operator.CreatedAt, &principal.Operator.CreatedBy, &principal.Operator.TOTPEnabledAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: unknown, revoked or expired", ErrInvalidCredential)
		}
		return err
	})
	if err != nil {
		return OperatorPrincipal{}, err
	}
	if principal.Kind == "cookie" && !viaCookie || principal.Kind == "token" && viaCookie {
		return OperatorPrincipal{}, fmt.Errorf("%w: wrong kind", ErrInvalidCredential)
	}
	principal.ViaCookie = viaCookie
	return principal, nil
}

// Reauth confirms it is still the operator before a dangerous change: the authenticator code, which
// marks the session for 10 minutes. An operator without an authenticator passes only within the first
// 15 minutes of a session (a fresh link from the shell). A wrong code counts toward the lock.
func (s *OperatorService) Reauth(ctx context.Context, principal OperatorPrincipal, code string) error {
	if principal.IsToken() {
		return ErrOperatorReauth
	}
	if principal.Operator.TOTPEnabledAt == nil {
		if s.now().Sub(principal.CreatedAt) <= operatorFreshSession {
			return nil
		}
		return ErrOperatorReauthCLI
	}
	if strings.TrimSpace(code) == "" {
		if principal.ReauthAt != nil && s.now().Sub(*principal.ReauthAt) <= operatorReauthLifetime {
			return nil
		}
		return ErrOperatorReauth
	}
	var outcome error
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var sealed []byte
		var keyID string
		var lastStep int64
		var failed int
		var locked *time.Time
		if err := tx.QueryRow(ctx, `SELECT totp_sealed, totp_key_id, totp_last_step, failed_codes, locked_until FROM operators WHERE id=$1 FOR UPDATE`, principal.Operator.ID).
			Scan(&sealed, &keyID, &lastStep, &failed, &locked); err != nil {
			return err
		}
		if locked != nil && s.now().Before(*locked) {
			outcome = ErrOperatorLocked
			return nil
		}
		secret, err := s.keys.Open(keyID, sealed, "operator-totp:"+principal.Operator.ID)
		if err != nil {
			return fmt.Errorf("open the authenticator key: %w", err)
		}
		matched, ok := matchTOTP(secret, code, s.now(), lastStep)
		if !ok {
			failed++
			var lockUntil *time.Time
			if failed >= operatorWrongCodesLimit {
				until := s.now().Add(operatorLockout)
				lockUntil, failed = &until, 0
			}
			outcome = ErrOperatorCode
			_, err := tx.Exec(ctx, `UPDATE operators SET failed_codes=$2, locked_until=coalesce($3, locked_until) WHERE id=$1`, principal.Operator.ID, failed, lockUntil)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE operators SET totp_last_step=$2, failed_codes=0 WHERE id=$1`, principal.Operator.ID, matched); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE operator_sessions SET reauth_at=$2 WHERE id=$1`, principal.SessionID, s.now())
		return err
	})
	if err != nil {
		return err
	}
	return outcome
}

// Logout ends the principal's own session or token.
func (s *OperatorService) Logout(ctx context.Context, principal OperatorPrincipal) error {
	return s.privileged(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE operator_sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, principal.SessionID, s.now()); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, actorKind(principal), principal.Operator.Email, "session.sign_out", map[string]any{"session": principal.SessionID})
	})
}

func actorKind(principal OperatorPrincipal) string {
	if principal.IsToken() {
		return "token"
	}
	return "operator"
}

// CreateToken makes an operator token: read always, plus the permissions given, for 30 days. The raw token
// is returned once.
func (s *OperatorService) CreateToken(ctx context.Context, principal OperatorPrincipal, label string, permissions []string) (string, OperatorSession, error) {
	label = strings.TrimSpace(label)
	if label == "" || len(label) > 100 {
		return "", OperatorSession{}, &InvalidTokenError{"A token needs a name of at most 100 characters."}
	}
	clean := []string{PermOperatorRead}
	for _, permission := range permissions {
		if !slices.Contains(OperatorPermissions, permission) {
			return "", OperatorSession{}, &InvalidTokenError{"Unknown operator permission " + quote(permission) + "."}
		}
		if !slices.Contains(clean, permission) {
			clean = append(clean, permission)
		}
	}
	slices.Sort(clean)
	random, err := randomToken()
	if err != nil {
		return "", OperatorSession{}, err
	}
	raw := operatorTokenPrefix + random
	session := OperatorSession{OperatorID: principal.Operator.ID, Email: principal.Operator.Email, Kind: "token", Label: label, Permissions: clean,
		CreatedAt: s.now(), LastSeenAt: s.now(), ExpiresAt: s.now().Add(operatorTokenLifetime)}
	err = s.privileged(ctx, func(tx pgx.Tx) error {
		var creator *string
		if principal.SessionID != "" {
			creator = &principal.SessionID
		}
		if err := tx.QueryRow(ctx, `INSERT INTO operator_sessions(operator_id, token_hash, kind, label, permissions, expires_at, created_by_session)
			VALUES($1,$2,'token',$3,$4,$5,$6) RETURNING id`, principal.Operator.ID, operatorTokenHash(raw), label, clean, session.ExpiresAt, creator).Scan(&session.ID); err != nil {
			return fmt.Errorf("create operator token: %w", err)
		}
		return operatorAudit(ctx, tx, actorKind(principal), principal.Operator.Email, "token.create", map[string]any{"session": session.ID, "label": label, "permissions": clean})
	})
	return raw, session, err
}

// CLIPrincipal is the shell acting as an operator: the CLI is the root of trust (collie operator token create).
func (s *OperatorService) CLIPrincipal(ctx context.Context, email string) (OperatorPrincipal, error) {
	var principal OperatorPrincipal
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id, email, created_at, created_by, totp_enabled_at FROM operators WHERE email=$1 AND revoked_at IS NULL`,
			strings.ToLower(strings.TrimSpace(email))).Scan(&principal.Operator.ID, &principal.Operator.Email, &principal.Operator.CreatedAt, &principal.Operator.CreatedBy, &principal.Operator.TOTPEnabledAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return principal, fmt.Errorf("no operator %s: run collie operator pair --email %s first", email, email)
	}
	principal.Kind = "cookie"
	return principal, err
}

// Operators lists every operator, revoked ones last.
func (s *OperatorService) Operators(ctx context.Context) ([]Operator, error) {
	var operators []Operator
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT o.id, o.email, o.created_at, o.created_by, o.revoked_at, o.totp_enabled_at,
			NOT EXISTS (SELECT 1 FROM operator_sessions s WHERE s.operator_id = o.id) AND EXISTS (SELECT 1 FROM operator_links l WHERE l.operator_id = o.id AND l.used_at IS NULL AND l.expires_at > $1 AND l.purpose <> 'totp-ticket')
			FROM operators o ORDER BY o.revoked_at IS NOT NULL, o.created_at`, s.now())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var operator Operator
			if err := rows.Scan(&operator.ID, &operator.Email, &operator.CreatedAt, &operator.CreatedBy, &operator.RevokedAt, &operator.TOTPEnabledAt, &operator.LinkUnused); err != nil {
				return err
			}
			operators = append(operators, operator)
		}
		return rows.Err()
	})
	return operators, err
}

// Sessions lists the sessions and tokens still valid, never their token.
func (s *OperatorService) Sessions(ctx context.Context) ([]OperatorSession, error) {
	var sessions []OperatorSession
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT s.id, s.operator_id, o.email, s.kind, s.label, s.permissions, s.created_at, s.last_seen_at, s.expires_at, s.revoked_at
			FROM operator_sessions s JOIN operators o ON o.id = s.operator_id
			WHERE s.revoked_at IS NULL AND s.expires_at > $1 AND o.revoked_at IS NULL ORDER BY s.created_at`, s.now())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var session OperatorSession
			if err := rows.Scan(&session.ID, &session.OperatorID, &session.Email, &session.Kind, &session.Label, &session.Permissions, &session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt, &session.RevokedAt); err != nil {
				return err
			}
			sessions = append(sessions, session)
		}
		return rows.Err()
	})
	return sessions, err
}

// RevokeOperator ends an operator's access: every session and token stops at once. Pair opens it again.
func (s *OperatorService) RevokeOperator(ctx context.Context, idOrEmail, actorKind, actor string) (bool, error) {
	var found bool
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var id, email string
		err := tx.QueryRow(ctx, `UPDATE operators SET revoked_at=$2 WHERE (id::text=$1 OR email=lower($1)) AND revoked_at IS NULL RETURNING id, email`, idOrEmail, s.now()).Scan(&id, &email)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		if _, err := tx.Exec(ctx, `UPDATE operator_sessions SET revoked_at=$2 WHERE operator_id=$1 AND revoked_at IS NULL`, id, s.now()); err != nil {
			return err
		}
		return operatorAudit(ctx, tx, actorKind, actor, "operator.revoke", map[string]any{"operator": email})
	})
	return found, err
}

// RevokeSession ends one session or token.
func (s *OperatorService) RevokeSession(ctx context.Context, sessionID, actorKind, actor string) (bool, error) {
	var found bool
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE operator_sessions SET revoked_at=$2 WHERE id::text=$1 AND revoked_at IS NULL`, sessionID, s.now())
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		found = true
		return operatorAudit(ctx, tx, actorKind, actor, "session.revoke", map[string]any{"session": sessionID})
	})
	return found, err
}

// Errors of an operator's Idempotency-Key.
var (
	ErrOperatorKeyReused   = errors.New("this Idempotency-Key was used for another request")
	ErrOperatorKeyInFlight = errors.New("a request with this Idempotency-Key is still running")
)

// operatorIdempotencyLifetime is how long a key answers its first result.
const operatorIdempotencyLifetime = 24 * time.Hour

// Once runs create once per operator, operation and key: a repeat with the same input answers the stored
// answer (replayed true), another input with the key is refused. create's answer is what a repeat sees, so
// it must not hold anything shown once.
func (s *OperatorService) Once(ctx context.Context, operatorID, operation, key string, fingerprint []byte, create func(context.Context) (any, error)) (json.RawMessage, bool, error) {
	var stored json.RawMessage
	var claimed bool
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM operator_idempotency WHERE created_at < $1`, s.now().Add(-operatorIdempotencyLifetime)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO operator_idempotency(operator_id, operation, key, fingerprint) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
			operatorID, operation, key, fingerprint)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			claimed = true
			return nil
		}
		var seen []byte
		if err := tx.QueryRow(ctx, `SELECT fingerprint, answer FROM operator_idempotency WHERE operator_id=$1 AND operation=$2 AND key=$3`, operatorID, operation, key).Scan(&seen, &stored); err != nil {
			return err
		}
		switch {
		case string(seen) != string(fingerprint):
			return ErrOperatorKeyReused
		case stored == nil:
			return ErrOperatorKeyInFlight
		}
		return nil
	})
	if err != nil || !claimed {
		return stored, !claimed && err == nil, err
	}
	answer, err := create(ctx)
	// The outcome is recorded even when the client left meanwhile, or its key would stay "in flight" and the
	// answer (a token shown once) would never reach a retry.
	detached := context.WithoutCancel(ctx)
	if err != nil {
		_ = s.privileged(detached, func(tx pgx.Tx) error {
			_, err := tx.Exec(detached, `DELETE FROM operator_idempotency WHERE operator_id=$1 AND operation=$2 AND key=$3`, operatorID, operation, key)
			return err
		})
		return nil, false, err
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		return nil, false, err
	}
	err = s.privileged(detached, func(tx pgx.Tx) error {
		_, err := tx.Exec(detached, `UPDATE operator_idempotency SET answer=$4 WHERE operator_id=$1 AND operation=$2 AND key=$3`, operatorID, operation, key, encoded)
		return err
	})
	return nil, false, err
}

// OperatorRequest is a change an operator token asked for, waiting for a person.
type OperatorRequest struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	TokenLabel   string          `json:"token_label"`
	Email        string          `json:"email"`
	Method       string          `json:"method"`
	Route        string          `json:"route"`
	Path         string          `json:"path"`
	Body         json.RawMessage `json:"body,omitempty"`
	Summary      string          `json:"summary"`
	Note         string          `json:"note"`
	NeedsSecrets []string        `json:"needs_secrets"`
	Status       string          `json:"status"`
	DecidedBy    string          `json:"decided_by,omitempty"`
	DecidedAt    *time.Time      `json:"decided_at,omitempty"`
	ResultStatus *int            `json:"result_status,omitempty"`
	Result       json.RawMessage `json:"result,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	ExpiresAt    time.Time       `json:"expires_at"`
}

// operatorRequestLifetime is how long a request waits for a person.
const operatorRequestLifetime = 7 * 24 * time.Hour

// FileRequest stores a token's call for a person to approve. The caller has refused a body with a secret.
func (s *OperatorService) FileRequest(ctx context.Context, principal OperatorPrincipal, request OperatorRequest) (OperatorRequest, error) {
	request.Status = "pending"
	request.CreatedAt = s.now()
	request.ExpiresAt = s.now().Add(operatorRequestLifetime)
	request.SessionID = principal.SessionID
	if request.NeedsSecrets == nil {
		request.NeedsSecrets = []string{}
	}
	if len(request.Note) > 500 {
		request.Note = request.Note[:500]
	}
	var body any
	if len(request.Body) > 0 {
		body = request.Body
	}
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO operator_requests(session_id, method, route, path, body, summary, note, needs_secrets, expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, principal.SessionID, request.Method, request.Route, request.Path, body,
			request.Summary, request.Note, request.NeedsSecrets, request.ExpiresAt).Scan(&request.ID); err != nil {
			return fmt.Errorf("file operator request: %w", err)
		}
		return operatorAudit(ctx, tx, "token", principal.Operator.Email, "request.file", map[string]any{"request": request.ID, "route": request.Route})
	})
	return request, err
}

// ErrOperatorRequestDecided means the request is no longer pending.
var ErrOperatorRequestDecided = errors.New("the request was decided, cancelled or expired")

const operatorRequestColumns = `r.id, r.session_id, s.label, o.email, r.method, r.route, r.path, r.body, r.summary, r.note, r.needs_secrets,
	CASE WHEN r.status = 'pending' AND r.expires_at <= $1 THEN 'expired' ELSE r.status END,
	coalesce(d.email, ''), r.decided_at, r.result_status, r.result, r.created_at, r.expires_at`

const operatorRequestJoins = `FROM operator_requests r JOIN operator_sessions s ON s.id = r.session_id JOIN operators o ON o.id = s.operator_id
	LEFT JOIN operators d ON d.id = r.decided_by`

func scanOperatorRequest(row pgx.Row) (OperatorRequest, error) {
	var request OperatorRequest
	var body, result []byte
	err := row.Scan(&request.ID, &request.SessionID, &request.TokenLabel, &request.Email, &request.Method, &request.Route, &request.Path, &body,
		&request.Summary, &request.Note, &request.NeedsSecrets, &request.Status, &request.DecidedBy, &request.DecidedAt, &request.ResultStatus, &result,
		&request.CreatedAt, &request.ExpiresAt)
	request.Body, request.Result = body, result
	if request.NeedsSecrets == nil {
		request.NeedsSecrets = []string{}
	}
	return request, err
}

// Requests lists operator requests, newest first: a token sees its own, an operator's session all.
func (s *OperatorService) Requests(ctx context.Context, principal OperatorPrincipal) ([]OperatorRequest, error) {
	var requests []OperatorRequest
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+operatorRequestColumns+` `+operatorRequestJoins+`
			WHERE ($2 = '' OR r.session_id::text = $2) ORDER BY r.created_at DESC LIMIT 200`, s.now(), tokenFilter(principal))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			request, err := scanOperatorRequest(rows)
			if err != nil {
				return err
			}
			requests = append(requests, request)
		}
		return rows.Err()
	})
	return requests, err
}

func tokenFilter(principal OperatorPrincipal) string {
	if principal.IsToken() {
		return principal.SessionID
	}
	return ""
}

// Request finds one request the principal may see; pgx.ErrNoRows when none.
func (s *OperatorService) Request(ctx context.Context, principal OperatorPrincipal, id string) (OperatorRequest, error) {
	var request OperatorRequest
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var err error
		request, err = scanOperatorRequest(tx.QueryRow(ctx, `SELECT `+operatorRequestColumns+` `+operatorRequestJoins+`
			WHERE r.id::text = $3 AND ($2 = '' OR r.session_id::text = $2)`, s.now(), tokenFilter(principal), id))
		return err
	})
	return request, err
}

// PendingRequests counts the requests waiting for a person.
func (s *OperatorService) PendingRequests(ctx context.Context) (int, error) {
	var count int
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM operator_requests WHERE status='pending' AND expires_at > $1`, s.now()).Scan(&count)
	})
	return count, err
}

// CancelRequest withdraws a pending request: its own token, or an operator's session.
func (s *OperatorService) CancelRequest(ctx context.Context, principal OperatorPrincipal, id string) (OperatorRequest, error) {
	return s.decide(ctx, principal, id, "cancelled", "request.cancel")
}

// RejectRequest refuses a pending request; nothing runs.
func (s *OperatorService) RejectRequest(ctx context.Context, principal OperatorPrincipal, id string) (OperatorRequest, error) {
	return s.decide(ctx, principal, id, "rejected", "request.reject")
}

// ClaimRequest marks a pending request approved before its call runs, so two approvals run it once.
func (s *OperatorService) ClaimRequest(ctx context.Context, principal OperatorPrincipal, id string) (OperatorRequest, error) {
	return s.decide(ctx, principal, id, "approved", "request.approve")
}

func (s *OperatorService) decide(ctx context.Context, principal OperatorPrincipal, id, status, action string) (OperatorRequest, error) {
	var request OperatorRequest
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var current string
		var expires time.Time
		err := tx.QueryRow(ctx, `SELECT status, expires_at FROM operator_requests WHERE id::text=$1 AND ($2 = '' OR session_id::text = $2) FOR UPDATE`,
			id, tokenFilter(principal)).Scan(&current, &expires)
		if err != nil {
			return err
		}
		if current != "pending" || !s.now().Before(expires) {
			return ErrOperatorRequestDecided
		}
		var decider *string
		if !principal.IsToken() {
			decider = &principal.Operator.ID
		}
		if _, err := tx.Exec(ctx, `UPDATE operator_requests SET status=$2, decided_by=$3, decided_at=$4 WHERE id::text=$1`, id, status, decider, s.now()); err != nil {
			return err
		}
		if err := operatorAudit(ctx, tx, actorKind(principal), principal.Operator.Email, action, map[string]any{"request": id}); err != nil {
			return err
		}
		request, err = scanOperatorRequest(tx.QueryRow(ctx, `SELECT `+operatorRequestColumns+` `+operatorRequestJoins+` WHERE r.id::text = $2`, s.now(), id))
		return err
	})
	return request, err
}

// FinishRequest records the outcome of an approved request's call; result is nil when it holds a credential.
func (s *OperatorService) FinishRequest(ctx context.Context, id string, status int, result json.RawMessage) (OperatorRequest, error) {
	var request OperatorRequest
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		outcome := "approved"
		if status < 200 || status > 299 {
			outcome = "failed"
		}
		var stored any
		if len(result) > 0 {
			stored = result
		}
		if _, err := tx.Exec(ctx, `UPDATE operator_requests SET status=$2, result_status=$3, result=$4 WHERE id::text=$1`, id, outcome, status, stored); err != nil {
			return err
		}
		var err error
		request, err = scanOperatorRequest(tx.QueryRow(ctx, `SELECT `+operatorRequestColumns+` `+operatorRequestJoins+` WHERE r.id::text = $2`, s.now(), id))
		return err
	})
	return request, err
}

// MarkReauthenticated records a fresh code on the principal, for a call replayed after the approver's code.
func (p OperatorPrincipal) MarkReauthenticated(at time.Time) OperatorPrincipal {
	p.ReauthAt = &at
	return p
}

// Now is the service's clock.
func (s *OperatorService) Now() time.Time { return s.now() }

// Invite makes a sign-in link for an operator another operator adds (or lets a revoked one in again).
func (s *OperatorService) Invite(ctx context.Context, principal OperatorPrincipal, email string) (string, time.Time, error) {
	link, err := s.pair(ctx, email, false, actorKind(principal), principal.Operator.Email)
	return link, s.now().Add(operatorLinkLifetime), err
}
