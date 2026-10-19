package importkit

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// StorageCapError means a workspace cannot take this import: its attachments would pass the plan's
// storage cap, which counts imported files. Nothing was written.
type StorageCapError struct {
	Plan                string
	Needed, Used, Limit int64
}

func (e *StorageCapError) Error() string {
	remedy := "Contact us for more storage"
	if e.Plan == domain.PlanFounding {
		remedy = "Upgrade to the Team plan (Billing > Manage billing) for a higher cap"
	}
	name := domain.PlanName(e.Plan)
	return fmt.Sprintf("this import needs %s of attachments, but only %s is free under the %s attachment cap of the %s plan. "+
		"Nothing was imported. %s, or import a smaller slice with --since.",
		domain.FormatBytes(e.Needed), domain.FormatBytes(max(e.Limit-e.Used, 0)), domain.FormatBytes(e.Limit), name, remedy)
}

// CappedRoom returns the storage room of the workspace, or ok false when no cap that counts imported
// files applies (self-hosted, trial). paidStorageBytes replaces the cap of every plan that counts
// imports when above zero; tests use a small one.
func CappedRoom(ctx context.Context, db *database.DB, workspaceID string, cloud bool, paidStorageBytes int64) (store.StorageRoom, bool, error) {
	if !cloud {
		return store.StorageRoom{}, false, nil
	}
	room, err := store.NewWithDB(db).WithPlanLimits(true).WithPaidStorageBytes(paidStorageBytes).GetStorageRoom(ctx, workspaceID)
	if err != nil {
		return store.StorageRoom{}, false, err
	}
	return room, room.Capped && domain.StorageCountsImports(room.Plan), nil
}
