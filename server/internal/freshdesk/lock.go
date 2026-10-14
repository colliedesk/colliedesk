package freshdesk

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// acquireRunLock holds a PostgreSQL advisory lock on the workspace for a whole
// import or undo. It is per workspace, not per source: imports of different
// accounts share content-addressed files, so an undo deleting unreferenced
// files must never overlap another import into the same workspace.
func acquireRunLock(ctx context.Context, pool *pgxpool.Pool, workspaceID string) (func(), error) {
	key := "colliedesk-import|" + workspaceID
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("take import lock: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked); err != nil {
		conn.Release()
		return nil, fmt.Errorf("take import lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, fmt.Errorf("%w; wait for it to finish, then run again", ErrImportRunning)
	}
	return func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key)
		conn.Release()
	}, nil
}

// accountSource reads the Freshdesk account id. The ledger is keyed by it, so
// renaming the Freshdesk subdomain never imports the account a second time.
func (r *run) accountSource(ctx context.Context) (string, error) {
	var account struct {
		AccountID *int64 `json:"account_id"`
	}
	if _, err := r.client.getJSON(ctx, r.endpoint("/api/v2/account", nil), &account); err != nil {
		return "", fmt.Errorf("read the Freshdesk account (an admin API key is required): %w", err)
	}
	if account.AccountID == nil || *account.AccountID <= 0 {
		return "", fmt.Errorf("the Freshdesk account response has no account_id")
	}
	return fmt.Sprintf("freshdesk:account:%d", *account.AccountID), nil
}
