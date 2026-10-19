package helpscout_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/helpscout"
	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// collie import undo runs freshdesk.Undo for every importer; it must find and remove a Help Scout
// import exactly, and keep a conversation an agent answered after the import.
func TestUndoRemovesExactlyTheHelpScoutImport(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	if _, err := store.New(env.pool).CreateTag(ctx, env.workspaceID, "billing", "#64748b"); err != nil {
		t.Fatal(err)
	}
	env.mustRun(t)
	var alice string
	if err := env.pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, env.workspaceID).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(env.pool).CreateMessage(ctx, env.workspaceID, env.target(t, "ticket", "103"), store.Actor{AgentID: alice}, "note", "Follow up tomorrow"); err != nil {
		t.Fatal(err)
	}
	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "helpscout:mailbox:501" || result.Conversations != 2 || result.Tags != 2 || len(result.KeptConversationTickets) != 1 || result.KeptConversationTickets[0] != "103" {
		t.Fatalf("undo = %+v", result)
	}
	// Conversation 103 (customer dana) stays with its 2 imported messages and the note; erin goes;
	// the billing tag existed before the import and stays.
	if got := env.tableCounts(t); got != "contacts=1 conversations=1 messages=3 attachments=0 tags=1 conversation_tags=0" {
		t.Fatalf("after undo: %s", got)
	}
	var channel, lastKind string
	if err := env.pool.QueryRow(ctx, `SELECT channel,last_public_kind FROM conversations WHERE workspace_id=$1`, env.workspaceID).Scan(&channel, &lastKind); err != nil ||
		channel != "imported" || lastKind != "incoming" {
		t.Fatalf("kept conversation channel=%q last_public_kind=%q (%v), want imported/incoming", channel, lastKind, err)
	}
	env.assertLastPublic(t)
}

func TestImportRefusesWhileAnotherImportHoldsTheWorkspace(t *testing.T) {
	env := newImportEnv(t)
	release, err := importkit.AcquireRunLock(context.Background(), database.New(env.pool), env.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := env.run(t, nil); !errors.Is(err, importkit.ErrImportRunning) {
		t.Fatalf("err = %v, want ErrImportRunning", err)
	}
}

func TestPaidPlanImportOverTheStorageCapWritesNothing(t *testing.T) {
	env := newImportEnv(t)
	if _, err := env.pool.Exec(context.Background(), `INSERT INTO workspace_billing(workspace_id,polar_subscription_id,plan,polar_customer_id,status,current_period_end,source_modified_at)
		VALUES($1::uuid,'sub_'||$1::text,'team','cus_test','active','2099-01-01T00:00:00Z','2026-10-01T09:00:00Z')`, env.workspaceID); err != nil {
		t.Fatal(err)
	}
	_, err := env.run(t, func(o *helpscout.Options) { o.Cloud, o.PaidStorageBytes = true, 30 })
	var capErr *importkit.StorageCapError
	if !errors.As(err, &capErr) || capErr.Needed != 39 {
		t.Fatalf("err = %v, want a StorageCapError needing 39 bytes (11 + 12 + 16)", err)
	}
	if got := env.tableCounts(t); got != "contacts=0 conversations=0 messages=0 attachments=0 tags=0 conversation_tags=0" {
		t.Fatalf("over-cap import wrote: %s", got)
	}
}
