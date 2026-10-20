package freshdesk_test

import (
	"context"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func TestUndoKeepsAContactTheImportCreatedOnceItIsMergedIntoAnother(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	data := store.New(env.pool)
	kept, err := data.CreateContact(ctx, env.workspaceID, "Kept by the team", "kept@team.example")
	if err != nil {
		t.Fatal(err)
	}
	env.mustRun(t)
	var imported string
	if err := env.pool.QueryRow(ctx, `SELECT i.target_id::text FROM import_items i JOIN conversations c ON c.workspace_id=i.workspace_id AND c.contact_id=i.target_id
		WHERE i.workspace_id=$1 AND i.entity='contact' AND (i.meta->>'created')::boolean LIMIT 1`, env.workspaceID).Scan(&imported); err != nil {
		t.Fatal(err)
	}
	merge, err := data.MergeContacts(ctx, env.workspaceID, kept.ID, imported, store.Actor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"}); err != nil {
		t.Fatal(err)
	}
	// The imported tickets are gone, but the merged contact stays: its address keeps routing mail to the
	// kept contact, and the merge stays on record.
	detail, err := data.GetContact(ctx, env.workspaceID, imported)
	if err != nil || detail.MergedIntoID == nil || *detail.MergedIntoID != kept.ID {
		t.Fatalf("merged contact after undo = %+v, %v; want it kept and merged into %s", detail.Contact, err, kept.ID)
	}
	keptDetail, err := data.GetContact(ctx, env.workspaceID, kept.ID)
	if err != nil || len(keptDetail.Merges) != 1 || keptDetail.Merges[0].ID != merge.ID {
		t.Fatalf("kept contact after undo = %+v, %v; want the merge listed", keptDetail.Merges, err)
	}
}
