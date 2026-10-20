package freshdesk

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/importkit"
)

func acquireRunLock(ctx context.Context, db *database.DB, workspaceID string) (func(), error) {
	return importkit.AcquireRunLock(ctx, db, workspaceID)
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
