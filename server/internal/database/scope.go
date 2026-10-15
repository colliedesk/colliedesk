package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoWorkspace means a workspace-scoped transaction was requested without a valid workspace id.
var ErrNoWorkspace = errors.New("workspace-scoped query needs a workspace id")

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// DB runs every query that reads or writes rows in a transaction that is either scoped to one
// workspace (InWorkspace) or declared privileged with a reviewed reason (Privileged). The owner pool
// also runs maintenance that returns no rows: advisory locks (TryAdvisoryLock) and table statistics
// (RefreshStatistics). Nothing else touches the owner pool outside these methods.
type DB struct {
	pool    *pgxpool.Pool // owner: migrations, privileged lookups, locks, ANALYZE
	request *pgxpool.Pool // workspace-scoped transactions

	mu      sync.Mutex
	appRole string
}

// New wraps one pool that connects as the schema owner (single-login mode). Scoping happens per
// transaction, so existing DATABASE_URL settings keep working. SQL that runs RESET ROLE inside a
// scoped transaction returns to the owner; use NewSplit to close that.
func New(pool *pgxpool.Pool) *DB {
	return NewSplit(pool, pool)
}

// NewSplit runs workspace-scoped transactions on request, a pool connected as the request login
// (see EnsureRequestLogin), and everything else on owner. The request login can only switch to the
// app role, so leaving the role inside a transaction leaves it with no rights at all.
func NewSplit(owner, request *pgxpool.Pool) *DB {
	return &DB{pool: owner, request: request}
}

// AppRole returns the role every workspace-scoped transaction switches to. The migration creates it
// per database (colliedesk_app_<database>) and binds the workspace_isolation policies to it; the
// name is read back from the policy on workspaces, so the migration stays the single source.
// The role owns nothing, cannot log in and has no BYPASSRLS.
func (d *DB) AppRole(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.appRole != "" {
		return d.appRole, nil
	}
	var role string
	err := d.pool.QueryRow(ctx, `SELECT roles[1]::text FROM pg_policies
		WHERE schemaname=current_schema() AND tablename='workspaces' AND policyname='workspace_isolation'`).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("row-level security is not set up in this schema; run `colliedesk migrate` first")
	}
	if err != nil {
		return "", fmt.Errorf("read the app role: %w", err)
	}
	d.appRole = role
	return role, nil
}

// BeginWorkspace starts a transaction as the app role with app.workspace_id set for this transaction
// only. Row-level security then limits every statement to that workspace. The caller commits or
// rolls back; both end the scope, so a pooled connection never carries it to the next user.
func (d *DB) BeginWorkspace(ctx context.Context, workspaceID string) (pgx.Tx, error) {
	return d.BeginWorkspaceTx(ctx, workspaceID, pgx.TxOptions{})
}

// BeginWorkspaceTx is BeginWorkspace with transaction options, for example a read-only
// REPEATABLE READ snapshot. Isolation and access mode must be fixed before the first statement,
// so they cannot be set after BeginWorkspace returns.
func (d *DB) BeginWorkspaceTx(ctx context.Context, workspaceID string, options pgx.TxOptions) (pgx.Tx, error) {
	if !uuidPattern.MatchString(workspaceID) {
		return nil, ErrNoWorkspace
	}
	role, err := d.AppRole(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := d.request.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin workspace transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true),set_config('app.workspace_id',$2,true)`, role, workspaceID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("scope transaction to workspace: %w", err)
	}
	return tx, nil
}

// InWorkspace runs fn in a workspace-scoped transaction and commits when fn returns nil.
func (d *DB) InWorkspace(ctx context.Context, workspaceID string, fn func(pgx.Tx) error) error {
	tx, err := d.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	return finish(ctx, tx, fn)
}

// Privileged runs fn as the connecting owner role with app.privileged set for this transaction, so
// it sees every workspace. Use it only for a reason listed in privileges.go; a guard test fails
// when a new call site appears.
func (d *DB) Privileged(ctx context.Context, reason Privilege, fn func(pgx.Tx) error) error {
	if reason == "" {
		return errors.New("privileged transaction needs a reason")
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin privileged transaction (%s): %w", reason, err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.privileged','on',true)`); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("mark privileged transaction (%s): %w", reason, err)
	}
	return finish(ctx, tx, fn)
}

// finish runs fn and commits. The deferred rollback (a no-op after a commit) also runs when fn
// panics, so the connection returns to the pool and its row and advisory locks are released
// before the panic continues.
func finish(ctx context.Context, tx pgx.Tx, fn func(pgx.Tx) error) error {
	defer tx.Rollback(context.WithoutCancel(ctx)) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// WorkspaceIDBySlug resolves a workspace slug for CLI commands that name a workspace.
func (d *DB) WorkspaceIDBySlug(ctx context.Context, slug string) (string, error) {
	var workspaceID string
	err := d.Privileged(ctx, PrivilegeWorkspaceBySlug, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id::text FROM workspaces WHERE slug=$1`, slug).Scan(&workspaceID)
	})
	if err != nil {
		return "", fmt.Errorf("find workspace %q: %w", slug, err)
	}
	return workspaceID, nil
}

// ErrLockHeld means another session holds the advisory lock.
var ErrLockHeld = errors.New("advisory lock is held by another session")

// TryAdvisoryLock takes a session-level advisory lock on its own pooled connection and returns the
// release function. It reads no table, so it needs no workspace scope.
func (d *DB) TryAdvisoryLock(ctx context.Context, key string) (func(), error) {
	conn, err := d.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("take advisory lock: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked); err != nil {
		conn.Release()
		return nil, fmt.Errorf("take advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, ErrLockHeld
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
		conn.Release()
	}, nil
}

// Verify refuses to serve when row-level security could be skipped: the app role must exist, must
// not be a superuser or BYPASSRLS, and the request connection must be able to switch to it. With a
// separate request login, that login must not be a superuser, BYPASSRLS or a member of the owner.
func (d *DB) Verify(ctx context.Context) error {
	role, err := d.AppRole(ctx)
	if err != nil {
		return err
	}
	var super, bypass bool
	err = d.pool.QueryRow(ctx, `SELECT rolsuper,rolbypassrls FROM pg_roles WHERE rolname=$1`, role).Scan(&super, &bypass)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("database role %s is missing; run `colliedesk migrate` first", role)
	}
	if err != nil {
		return fmt.Errorf("check database role %s: %w", role, err)
	}
	if super || bypass {
		return fmt.Errorf("database role %s must not be SUPERUSER or BYPASSRLS; row-level security would be skipped", role)
	}
	var owner string
	if err := d.pool.QueryRow(ctx, `SELECT current_user`).Scan(&owner); err != nil {
		return fmt.Errorf("read the owner role: %w", err)
	}
	var user string
	var canSet, loginSuper, loginBypass, isOwner bool
	err = d.request.QueryRow(ctx, `SELECT session_user, pg_has_role(session_user,$1,'SET'), r.rolsuper, r.rolbypassrls,
		pg_has_role(session_user,$2,'MEMBER') FROM pg_roles r WHERE r.rolname=session_user`, role, owner).
		Scan(&user, &canSet, &loginSuper, &loginBypass, &isOwner)
	if err != nil {
		return fmt.Errorf("check the request login: %w", err)
	}
	if !canSet {
		return fmt.Errorf("database role %s cannot SET ROLE %s; run `GRANT %s TO %s` as a superuser", user, role, role, user)
	}
	if d.request != d.pool && (loginSuper || loginBypass || isOwner) {
		return fmt.Errorf("the request login %s must not be a superuser, BYPASSRLS or a member of the owner %s", user, owner)
	}
	return nil
}

// EnsureRequestLogin creates the login role named in requestURL, with the password in it, when the
// role does not exist, and lets it switch to the app role without inheriting its rights. The owner
// pool needs CREATEROLE (or superuser) to create it. Only a SCRAM-SHA-256 verifier of the password
// is sent, so a logged or failed CREATE ROLE never shows the plaintext.
func EnsureRequestLogin(ctx context.Context, owner *pgxpool.Pool, requestURL string) error {
	config, err := pgx.ParseConfig(requestURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_REQUEST_URL: %w", err)
	}
	user := config.User
	var ownerName string
	if err := owner.QueryRow(ctx, `SELECT current_user`).Scan(&ownerName); err != nil {
		return fmt.Errorf("read the owner role: %w", err)
	}
	if user == "" || user == ownerName {
		return fmt.Errorf("DATABASE_REQUEST_URL must name its own user, not the DATABASE_URL owner %q", ownerName)
	}
	role, err := New(owner).AppRole(ctx)
	if err != nil {
		return err
	}
	return New(owner).Privileged(ctx, PrivilegeRequestLogin, func(tx pgx.Tx) error {
		var exists, member bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1),
			EXISTS(SELECT 1 FROM pg_auth_members m JOIN pg_roles g ON g.oid=m.roleid JOIN pg_roles u ON u.oid=m.member
				WHERE g.rolname=$2 AND u.rolname=$1)`, user, role).Scan(&exists, &member); err != nil {
			return fmt.Errorf("check request login %s: %w", user, err)
		}
		if !exists {
			verifier, err := newSCRAMVerifier(config.Password)
			if err != nil {
				return fmt.Errorf("DATABASE_REQUEST_URL password: %w", err)
			}
			var statement string
			if err := tx.QueryRow(ctx, `SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE PASSWORD %L',
				$1::text, $2::text)`, user, verifier).Scan(&statement); err != nil {
				return fmt.Errorf("prepare request login %s: %w", user, err)
			}
			if _, err := tx.Exec(ctx, statement); err != nil {
				return fmt.Errorf("create request login %s (the DATABASE_URL role needs CREATEROLE): %w", user, err)
			}
		}
		if !member {
			grant := fmt.Sprintf(`GRANT %s TO %s WITH INHERIT FALSE, SET TRUE`, pgx.Identifier{role}.Sanitize(), pgx.Identifier{user}.Sanitize())
			if _, err := tx.Exec(ctx, grant); err != nil {
				return fmt.Errorf("let %s switch to %s (as a superuser run: %s): %w", user, role, grant, err)
			}
		}
		return nil
	})
}
