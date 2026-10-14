package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppRole is the role every workspace-scoped transaction switches to. It owns nothing, cannot log
// in and has no BYPASSRLS, so the workspace_isolation policies always apply to it.
const AppRole = "colliedesk_app"

// ErrNoWorkspace means a workspace-scoped transaction was requested without a valid workspace id.
var ErrNoWorkspace = errors.New("workspace-scoped query needs a workspace id")

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// DB runs every query in a transaction that is either scoped to one workspace (InWorkspace) or
// declared privileged with a reviewed reason (Privileged). There is no third way to query.
type DB struct {
	pool *pgxpool.Pool
}

// New wraps the connection pool. The pool connects as the schema owner; scoping happens per
// transaction, so existing DATABASE_URL settings keep working.
func New(pool *pgxpool.Pool) *DB {
	return &DB{pool: pool}
}

// BeginWorkspace starts a transaction as AppRole with app.workspace_id set for this transaction
// only. Row-level security then limits every statement to that workspace. The caller commits or
// rolls back; both end the scope, so a pooled connection never carries it to the next user.
func (d *DB) BeginWorkspace(ctx context.Context, workspaceID string) (pgx.Tx, error) {
	if !uuidPattern.MatchString(workspaceID) {
		return nil, ErrNoWorkspace
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin workspace transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true),set_config('app.workspace_id',$2,true)`, AppRole, workspaceID); err != nil {
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
// not be a superuser or BYPASSRLS, and the connecting role must be able to switch to it.
func (d *DB) Verify(ctx context.Context) error {
	var super, bypass, canSet bool
	err := d.pool.QueryRow(ctx, `SELECT rolsuper,rolbypassrls,pg_has_role(current_user,oid,'SET') FROM pg_roles WHERE rolname=$1`, AppRole).
		Scan(&super, &bypass, &canSet)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("database role %s is missing; run `colliedesk migrate` first", AppRole)
	}
	if err != nil {
		return fmt.Errorf("check database role %s: %w", AppRole, err)
	}
	if super || bypass {
		return fmt.Errorf("database role %s must not be SUPERUSER or BYPASSRLS; row-level security would be skipped", AppRole)
	}
	if !canSet {
		return fmt.Errorf("the DATABASE_URL role cannot SET ROLE %s; run `GRANT %s TO <that role>` as a superuser", AppRole, AppRole)
	}
	return nil
}
