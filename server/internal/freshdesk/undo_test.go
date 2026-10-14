package freshdesk_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func storedFiles(t *testing.T, root string) int {
	t.Helper()
	files := 0
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files++
		}
		return err
	})
	return files
}

func TestUndoRemovesWhatTheImportCreatedAndKeepsWorkSinceThen(t *testing.T) {
	env := newImportEnv(t)
	ctx := context.Background()
	data := store.New(env.pool)
	if _, err := data.CreateContact(ctx, env.workspaceID, "Carol (existing)", "carol@globex.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateTag(ctx, env.workspaceID, "billing", "#64748b"); err != nil {
		t.Fatal(err)
	}
	env.mustRun(t)
	var alice string
	if err := env.pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, env.workspaceID).Scan(&alice); err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateMessage(ctx, env.workspaceID, env.target(t, "ticket", "2"), alice, "outgoing", "Written after the migration"); err != nil {
		t.Fatal(err)
	}

	preview, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support", DryRun: true})
	if err != nil || preview.Conversations != 4 || env.count(t, "conversations") != 5 {
		t.Fatalf("dry run = %+v, %v; conversations now %d", preview, err, env.count(t, "conversations"))
	}
	result, err := freshdesk.Undo(ctx, env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "freshdesk:account:777" || result.Conversations != 4 || strings.Join(result.KeptConversationTickets, ",") != "2" {
		t.Fatalf("undo = %+v", result)
	}
	// Kept: ticket 2 with its requester Dan, the pre-existing Carol contact and billing tag.
	if got := env.tableCounts(t); got != "contacts=2 conversations=1 messages=3 attachments=1 time_entries=0 tags=1 conversation_tags=0" {
		t.Fatalf("rows after undo = %s", got)
	}
	if files := storedFiles(t, env.storageRoot); files != 1 {
		t.Fatalf("stored files after undo = %d, want only the kept ticket's attachment", files)
	}

	again := env.mustRun(t)
	if again.Status != freshdesk.StatusOK || env.count(t, "conversations") != 5 || env.count(t, "messages") != 12 {
		t.Fatalf("re-import after undo: status=%s rows=%s", again.Status, env.tableCounts(t))
	}
}

func TestUndoWithoutAnImportFails(t *testing.T) {
	env := newImportEnv(t)
	_, err := freshdesk.Undo(context.Background(), env.pool, env.storage, freshdesk.UndoOptions{WorkspaceSlug: "acme-support"})
	if !errors.Is(err, freshdesk.ErrNoImport) {
		t.Fatalf("error = %v, want ErrNoImport", err)
	}
}
