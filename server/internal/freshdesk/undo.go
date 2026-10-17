package freshdesk

import (
	"context"
	"fmt"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UndoOptions struct {
	WorkspaceSlug string
	// Source is the ledger source, for example freshdesk:account:777; empty
	// selects the only Freshdesk import in the workspace.
	Source string
	DryRun bool
}

type UndoResult struct {
	Source        string `json:"source"`
	Conversations int    `json:"conversations"`
	Contacts      int    `json:"contacts"`
	Tags          int    `json:"tags"`
	SavedReplies  int    `json:"saved_replies"`
	// KeptSavedReplies are the Freshdesk ids of imported saved replies an agent edited since; they stay.
	KeptSavedReplies        []string `json:"kept_saved_replies"`
	Files                   int      `json:"files"`
	KeptConversationTickets []string `json:"kept_conversation_tickets"`
}

// targetExists tells whether the Colliedesk row a ledger row points at still exists.
const targetExists = `CASE i.entity
	WHEN 'ticket' THEN EXISTS(SELECT 1 FROM conversations x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'description' THEN EXISTS(SELECT 1 FROM messages x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'conversation' THEN EXISTS(SELECT 1 FROM messages x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'attachment' THEN EXISTS(SELECT 1 FROM attachments x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'time_entry' THEN EXISTS(SELECT 1 FROM time_entries x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'contact' THEN EXISTS(SELECT 1 FROM contacts x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'tag' THEN EXISTS(SELECT 1 FROM tags x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'saved_reply' THEN EXISTS(SELECT 1 FROM saved_replies x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	ELSE false END`

// Undo removes what one import created in a workspace: its conversations
// (with their messages, attachments and time entries), and the contacts and
// tags it created that nothing else uses. A conversation that gained a
// message after the import is kept with its ledger rows, so work done in
// Colliedesk is never lost and a later import does not duplicate it.
func Undo(ctx context.Context, pool *pgxpool.Pool, storage attachments.Storage, options UndoOptions) (UndoResult, error) {
	db := database.New(pool)
	workspaceID, err := db.WorkspaceIDBySlug(ctx, options.WorkspaceSlug)
	if err != nil {
		return UndoResult{}, err
	}
	release, err := acquireRunLock(ctx, db, workspaceID)
	if err != nil {
		return UndoResult{}, err
	}
	defer release()
	// Undo usually follows an import within minutes, before autovacuum has analyzed the rows it
	// wrote; with stale statistics the keep query below took 37 s for a 300-ticket import.
	if err := db.RefreshStatistics(ctx, "import_items", "conversations", "messages", "attachments", "contacts", "tags", "conversation_tags", "time_entries", "saved_replies"); err != nil {
		return UndoResult{}, err
	}
	tx, err := db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return UndoResult{}, fmt.Errorf("begin undo: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	source, err := undoSource(ctx, tx, workspaceID, options.Source)
	if err != nil {
		return UndoResult{}, err
	}
	result, orphans, err := undoRows(ctx, tx, workspaceID, source)
	if err != nil {
		return UndoResult{}, err
	}
	result.Files = len(orphans)
	if options.DryRun {
		return result, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return UndoResult{}, fmt.Errorf("commit undo: %w", err)
	}
	for _, key := range orphans {
		if err := store.DiscardObject(ctx, db, storage, workspaceID, key); err != nil {
			return result, fmt.Errorf("undo committed, but a stored file could not be removed: %w", err)
		}
	}
	return result, nil
}

func undoSource(ctx context.Context, tx pgx.Tx, workspaceID, requested string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT source FROM import_items WHERE workspace_id=$1 AND source LIKE 'freshdesk:%' ORDER BY 1`, workspaceID)
	if err != nil {
		return "", fmt.Errorf("list imports: %w", err)
	}
	sources, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", fmt.Errorf("list imports: %w", err)
	}
	switch {
	case requested != "":
		for _, source := range sources {
			if source == requested {
				return source, nil
			}
		}
		return "", fmt.Errorf("%w from %s", ErrNoImport, requested)
	case len(sources) == 0:
		return "", ErrNoImport
	case len(sources) > 1:
		return "", fmt.Errorf("the workspace holds several imports (%s); choose one with --source", strings.Join(sources, ", "))
	}
	return sources[0], nil
}

func undoRows(ctx context.Context, tx pgx.Tx, workspaceID, source string) (UndoResult, []string, error) {
	result := UndoResult{Source: source, KeptConversationTickets: []string{}, KeptSavedReplies: []string{}}
	rows, err := tx.Query(ctx, `SELECT i.source_id FROM import_items i JOIN conversations c ON c.workspace_id=i.workspace_id AND c.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='ticket' AND EXISTS (
			SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND NOT EXISTS (
				SELECT 1 FROM import_items j WHERE j.workspace_id=$1 AND j.source=$2 AND j.entity IN ('description','conversation') AND j.target_id=m.id))
		ORDER BY length(i.source_id),i.source_id`, workspaceID, source)
	if err != nil {
		return result, nil, fmt.Errorf("find conversations to keep: %w", err)
	}
	if result.KeptConversationTickets, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return result, nil, fmt.Errorf("find conversations to keep: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT i.source_id FROM import_items i JOIN saved_replies s ON s.workspace_id=i.workspace_id AND s.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='saved_reply' AND s.updated_at > s.created_at
		ORDER BY length(i.source_id),i.source_id`, workspaceID, source)
	if err != nil {
		return result, nil, fmt.Errorf("find saved replies to keep: %w", err)
	}
	if result.KeptSavedReplies, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return result, nil, fmt.Errorf("find saved replies to keep: %w", err)
	}
	steps := []struct {
		count *int
		sql   string
		args  []any
	}{
		{&result.Conversations, `DELETE FROM conversations c USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='ticket'
			AND c.workspace_id=i.workspace_id AND c.id=i.target_id AND i.source_id <> ALL($3)`, []any{workspaceID, source, result.KeptConversationTickets}},
		{&result.Contacts, `DELETE FROM contacts c USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='contact'
			AND (i.meta->>'created')::boolean AND c.workspace_id=i.workspace_id AND c.id=i.target_id
			AND NOT EXISTS (SELECT 1 FROM conversations v WHERE v.workspace_id=c.workspace_id AND v.contact_id=c.id)`, []any{workspaceID, source}},
		{&result.SavedReplies, `DELETE FROM saved_replies s USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='saved_reply'
			AND s.workspace_id=i.workspace_id AND s.id=i.target_id AND i.source_id <> ALL($3)`, []any{workspaceID, source, result.KeptSavedReplies}},
		{&result.Tags, `DELETE FROM tags t USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='tag'
			AND (i.meta->>'created')::boolean AND t.workspace_id=i.workspace_id AND t.id=i.target_id
			AND NOT EXISTS (SELECT 1 FROM conversation_tags ct WHERE ct.workspace_id=t.workspace_id AND ct.tag_id=t.id)`, []any{workspaceID, source}},
	}
	var orphanCandidates []string
	keys, err := tx.Query(ctx, `SELECT DISTINCT a.storage_key FROM attachments a JOIN messages m ON m.workspace_id=a.workspace_id AND m.id=a.message_id
		JOIN import_items i ON i.workspace_id=m.workspace_id AND i.target_id=m.conversation_id AND i.entity='ticket'
		WHERE a.workspace_id=$1 AND i.source=$2`, workspaceID, source)
	if err != nil {
		return result, nil, fmt.Errorf("list stored files: %w", err)
	}
	if orphanCandidates, err = pgx.CollectRows(keys, pgx.RowTo[string]); err != nil {
		return result, nil, fmt.Errorf("list stored files: %w", err)
	}
	for _, step := range steps {
		tag, err := tx.Exec(ctx, step.sql, step.args...)
		if err != nil {
			return result, nil, fmt.Errorf("undo import: %w", err)
		}
		*step.count = int(tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `DELETE FROM import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND NOT (`+targetExists+`)`, workspaceID, source); err != nil {
		return result, nil, fmt.Errorf("clean import ledger: %w", err)
	}
	var orphans []string
	for _, key := range orphanCandidates {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE storage_key=$1)`, key).Scan(&used); err != nil {
			return result, nil, fmt.Errorf("check stored file: %w", err)
		}
		if !used {
			orphans = append(orphans, key)
		}
	}
	return result, orphans, nil
}
