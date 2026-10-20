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
	// Upgrade is the plan with more storage the server sells (StorageRoom.Upgrade), "" when none.
	Upgrade string
}

func (e *StorageCapError) Error() string {
	remedy := "Contact us for more storage"
	if e.Upgrade != "" {
		remedy = "Upgrade to the " + domain.PlanName(e.Upgrade) + " plan (Billing > Manage billing) for a higher cap"
	}
	name := domain.PlanName(e.Plan)
	return fmt.Sprintf("this import needs %s of attachments, but only %s is free under the %s attachment cap of the %s plan. "+
		"Nothing was imported. %s, or import a smaller slice with --since.",
		domain.FormatBytes(e.Needed), domain.FormatBytes(max(e.Limit-e.Used, 0)), domain.FormatBytes(e.Limit), name, remedy)
}

// CappedRoom returns the storage room of the workspace, or ok false when no cap that counts imported
// files applies (self-hosted, trial). paidStorageBytes replaces the cap of every plan that counts
// imports when above zero; tests use a small one. plansOnSale picks the upgrade the room names.
func CappedRoom(ctx context.Context, db *database.DB, workspaceID string, cloud bool, paidStorageBytes int64, plansOnSale []string) (store.StorageRoom, bool, error) {
	if !cloud {
		return store.StorageRoom{}, false, nil
	}
	room, err := store.NewWithDB(db).WithPlanLimits(true).WithPaidStorageBytes(paidStorageBytes).WithPlansOnSale(plansOnSale).GetStorageRoom(ctx, workspaceID)
	if err != nil {
		return store.StorageRoom{}, false, err
	}
	return room, room.Capped && domain.StorageCountsImports(room.Plan), nil
}
