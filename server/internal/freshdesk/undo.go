package freshdesk

import (
	"context"
	"fmt"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UndoOptions struct {
	WorkspaceSlug string
	// Source is the ledger source, for example freshdesk:account:777 or helpscout:mailbox:501;
	// empty selects the only import in the workspace.
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
	// KBArticles, KBCollections and KBImages count the knowledge base rows removed. KeptKBArticles are
	// the Freshdesk ids of imported articles changed in Colliedesk since (an edit, a restore, a delete);
	// they stay, with the collections that hold them.
	KBArticles     int      `json:"kb_articles"`
	KBCollections  int      `json:"kb_collections"`
	KBImages       int      `json:"kb_images"`
	KeptKBArticles []string `json:"kept_kb_articles"`
	// KeptKBImages counts imported images no article shows any more that stay because a person kept
	// them in the media library.
	KeptKBImages int `json:"kept_kb_images"`
}

// targetExists tells whether the Colliedesk row a ledger row points at still exists.
const targetExists = `CASE i.entity
	WHEN 'ticket' THEN EXISTS(SELECT 1 FROM conversations x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'description' THEN EXISTS(SELECT 1 FROM messages x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'conversation' THEN EXISTS(SELECT 1 FROM messages x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'thread' THEN EXISTS(SELECT 1 FROM messages x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'attachment' THEN EXISTS(SELECT 1 FROM attachments x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'inline_image' THEN EXISTS(SELECT 1 FROM attachments x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'time_entry' THEN EXISTS(SELECT 1 FROM time_entries x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'contact' THEN EXISTS(SELECT 1 FROM contacts x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'identity' THEN EXISTS(SELECT 1 FROM contact_identities x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'tag' THEN EXISTS(SELECT 1 FROM tags x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'saved_reply' THEN EXISTS(SELECT 1 FROM saved_replies x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'kb_category' THEN EXISTS(SELECT 1 FROM kb_collections x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'kb_folder' THEN EXISTS(SELECT 1 FROM kb_collections x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'kb_article' THEN EXISTS(SELECT 1 FROM kb_articles x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	WHEN 'kb_translation' THEN EXISTS(SELECT 1 FROM kb_article_translations x WHERE x.workspace_id=i.workspace_id AND x.article_id=i.target_id
		AND x.locale=split_part(i.source_id,':',2))
	WHEN 'kb_image' THEN EXISTS(SELECT 1 FROM kb_media x WHERE x.workspace_id=i.workspace_id AND x.id=i.target_id)
	ELSE false END`

// Undo removes what one import (Freshdesk, Help Scout or a bundle: all write the same ledger entities, a
// Help Scout thread being "thread") created in a workspace: its conversations
// (with their messages, attachments and time entries), and the contacts and
// tags it created that nothing else uses. A conversation that gained a
// message after the import is kept with its ledger rows, so work done in
// Colliedesk is never lost and a later import does not duplicate it.
// Markdown is the undo as a person reads it: what was removed, and what stayed because the team
// changed it since the import.
func (u UndoResult) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Freshdesk import undone\n\n- Source: %s\n- Conversations removed: %d\n- Contacts removed: %d\n- Tags removed: %d\n- Saved replies removed: %d\n- Stored files removed: %d\n",
		u.Source, u.Conversations, u.Contacts, u.Tags, u.SavedReplies, u.Files)
	fmt.Fprintf(&b, "- Knowledge base removed: %d articles, %d collections, %d images\n", u.KBArticles, u.KBCollections, u.KBImages)
	kept := []struct {
		what string
		ids  []string
	}{
		{"Conversations kept because they have messages written after the import (Freshdesk ticket ids)", u.KeptConversationTickets},
		{"Saved replies kept because an agent edited them (Freshdesk canned response ids)", u.KeptSavedReplies},
		{"Articles kept because they were changed in Colliedesk (Freshdesk article ids)", u.KeptKBArticles},
	}
	for _, item := range kept {
		if len(item.ids) > 0 {
			fmt.Fprintf(&b, "- %s: %s\n", item.what, strings.Join(item.ids, ", "))
		}
	}
	if u.KeptKBImages > 0 {
		fmt.Fprintf(&b, "- Images kept because a person kept them in the media library: %d\n", u.KeptKBImages)
	}
	return b.String()
}

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
	if err := db.RefreshStatistics(ctx, "import_items", "conversations", "messages", "attachments", "contacts", "tags", "conversation_tags", "time_entries", "saved_replies",
		"kb_articles", "kb_article_versions", "kb_collections", "kb_media", "kb_article_media", "kb_article_translations"); err != nil {
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
	if err := results.MarkImportUndone(ctx, tx, workspaceID, source); err != nil {
		return UndoResult{}, err
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
	rows, err := tx.Query(ctx, `SELECT DISTINCT source FROM import_items WHERE workspace_id=$1 AND (source LIKE 'freshdesk:%' OR source LIKE 'helpscout:%' OR source LIKE 'bundle:%') ORDER BY 1`, workspaceID)
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
	result := UndoResult{Source: source, KeptConversationTickets: []string{}, KeptSavedReplies: []string{}, KeptKBArticles: []string{}}
	rows, err := tx.Query(ctx, `SELECT i.source_id FROM import_items i JOIN conversations c ON c.workspace_id=i.workspace_id AND c.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='ticket' AND EXISTS (
			SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND NOT EXISTS (
				SELECT 1 FROM import_items j WHERE j.workspace_id=$1 AND j.source=$2 AND j.entity IN ('description','conversation','thread') AND j.target_id=m.id))
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
		// A bundle's identities it added to a contact that stays (one known before, or kept) go too, unless
		// a conversation that stays came through them.
		{new(int), `DELETE FROM contact_identities x USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='identity'
			AND (i.meta->>'created')::boolean AND x.workspace_id=i.workspace_id AND x.id=i.target_id
			AND NOT EXISTS (SELECT 1 FROM conversations v WHERE v.workspace_id=x.workspace_id AND v.contact_id=x.contact_id AND v.channel_id=x.channel_id)`, []any{workspaceID, source}},
		{&result.Contacts, `DELETE FROM contacts c USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='contact'
			AND (i.meta->>'created')::boolean AND c.workspace_id=i.workspace_id AND c.id=i.target_id AND c.merged_into_id IS NULL
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
	kbKeys, err := undoKnowledgeBase(ctx, tx, workspaceID, source, &result)
	if err != nil {
		return result, nil, err
	}
	orphanCandidates = append(orphanCandidates, kbKeys...)
	if _, err := tx.Exec(ctx, `DELETE FROM import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND NOT (`+targetExists+`)`, workspaceID, source); err != nil {
		return result, nil, fmt.Errorf("clean import ledger: %w", err)
	}
	var orphans []string
	for _, key := range orphanCandidates {
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE storage_key=$1)
			OR EXISTS(SELECT 1 FROM kb_media WHERE storage_key=$1 OR thumb_storage_key=$1 OR poster_storage_key=$1)`, key).Scan(&used); err != nil {
			return result, nil, fmt.Errorf("check stored file: %w", err)
		}
		if !used {
			orphans = append(orphans, key)
		}
	}
	return result, orphans, nil
}

// undoKnowledgeBase removes the imported articles nobody changed in Colliedesk since (their newest
// version is the import; updated_at cannot tell, an article carries Freshdesk's timestamps), then the
// imported collections left with no article and no collection, then the images the import added that
// no article links and nobody kept in the library. It returns the storage keys of the images removed
// and of their thumbnails.
func undoKnowledgeBase(ctx context.Context, tx pgx.Tx, workspaceID, source string, result *UndoResult) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT i.source_id FROM import_items i JOIN kb_articles a ON a.workspace_id=i.workspace_id AND a.id=i.target_id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_article'
			AND (SELECT v.kind FROM kb_article_versions v WHERE v.workspace_id=a.workspace_id AND v.article_id=a.id ORDER BY v.version DESC LIMIT 1) <> 'import'
		ORDER BY length(i.source_id),i.source_id`, workspaceID, source)
	if err != nil {
		return nil, fmt.Errorf("find knowledge base articles to keep: %w", err)
	}
	if result.KeptKBArticles, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return nil, fmt.Errorf("find knowledge base articles to keep: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM kb_articles a USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_article'
		AND a.workspace_id=i.workspace_id AND a.id=i.target_id AND i.source_id <> ALL($3)`, workspaceID, source, result.KeptKBArticles)
	if err != nil {
		return nil, fmt.Errorf("undo knowledge base articles: %w", err)
	}
	result.KBArticles = int(tag.RowsAffected())
	// Deepest first: a collection goes once the collections in it went.
	for range domain.MaxKBDepth {
		tag, err := tx.Exec(ctx, `DELETE FROM kb_collections c USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity IN ('kb_category','kb_folder')
			AND c.workspace_id=i.workspace_id AND c.id=i.target_id
			AND NOT EXISTS (SELECT 1 FROM kb_articles a WHERE a.workspace_id=c.workspace_id AND a.collection_id=c.id)
			AND NOT EXISTS (SELECT 1 FROM kb_collections k WHERE k.workspace_id=c.workspace_id AND k.parent_id=c.id)`, workspaceID, source)
		if err != nil {
			return nil, fmt.Errorf("undo knowledge base collections: %w", err)
		}
		result.KBCollections += int(tag.RowsAffected())
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_media k JOIN import_items i ON i.workspace_id=k.workspace_id AND i.target_id=k.id
		WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_image' AND (i.meta->>'created')::boolean AND k.kept
		AND NOT EXISTS (SELECT 1 FROM kb_article_media l WHERE l.workspace_id=k.workspace_id AND l.media_id=k.id)`, workspaceID, source).Scan(&result.KeptKBImages); err != nil {
		return nil, fmt.Errorf("count kept knowledge base images: %w", err)
	}
	rows, err = tx.Query(ctx, `DELETE FROM kb_media k USING import_items i WHERE i.workspace_id=$1 AND i.source=$2 AND i.entity='kb_image'
		AND (i.meta->>'created')::boolean AND k.workspace_id=i.workspace_id AND k.id=i.target_id AND NOT k.kept
		AND NOT EXISTS (SELECT 1 FROM kb_article_media l WHERE l.workspace_id=k.workspace_id AND l.media_id=k.id)
		RETURNING k.storage_key, COALESCE(k.thumb_storage_key,'')`, workspaceID, source)
	if err != nil {
		return nil, fmt.Errorf("undo knowledge base images: %w", err)
	}
	var keys []string
	var key, thumb string
	if _, err := pgx.ForEachRow(rows, []any{&key, &thumb}, func() error {
		result.KBImages++
		keys = append(keys, key)
		if thumb != "" {
			keys = append(keys, thumb)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("undo knowledge base images: %w", err)
	}
	if result.KBArticles+result.KBCollections+result.KBImages > 0 {
		// A help center page cached under the old content version is stale now.
		if _, err := tx.Exec(ctx, `UPDATE help_centers SET content_version=content_version+1, updated_at=clock_timestamp() WHERE workspace_id=$1`, workspaceID); err != nil {
			return nil, fmt.Errorf("bump knowledge base version: %w", err)
		}
	}
	return keys, nil
}
