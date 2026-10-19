package helpscout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Ledger entities in import_items. "ticket", "contact", "tag", "attachment" and "inline_image" mean
// what they mean for the Freshdesk importer, so `collie import undo` handles both; a Help Scout
// thread that became a message is "thread".
const (
	entityContact     = "contact"
	entityTicket      = "ticket"
	entityThread      = "thread"
	entityAttachment  = "attachment"
	entityInlineImage = "inline_image"
	entityTag         = "tag"

	ledgerDone    = "done"
	ledgerFailed  = "failed"
	ledgerSkipped = "skipped"
)

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// itemMeta records what Help Scout held for a conversation when it was imported, so a re-run that
// skips it can still report source counts.
type itemMeta struct {
	// Created marks contacts and tags the import created (undo removes only those).
	Created bool `json:"created,omitempty"`
	Threads int  `json:"threads,omitempty"`
	// ThreadsRead is how many threads the threads endpoint returned; fewer than Threads (the count
	// on the conversation) means Help Scout did not hand over every thread.
	ThreadsRead  int      `json:"threads_read,omitempty"`
	Messages     int      `json:"messages"`
	Attachments  int      `json:"attachments"`
	InlineImages int      `json:"inline_images,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Contact      string   `json:"contact,omitempty"`
	LineItems    int      `json:"line_items,omitempty"`
	Drafts       []string `json:"drafts,omitempty"`
	Virus        []string `json:"virus,omitempty"`
}

type ledgerEntry struct {
	Status          string
	TargetID        *string
	SourceUpdatedAt *time.Time
	Meta            itemMeta
}

type ledger struct {
	db          *database.DB
	workspaceID string
	source      string
}

func (l ledger) get(ctx context.Context, entity, sourceID string) (ledgerEntry, bool, error) {
	var entry ledgerEntry
	var meta []byte
	err := l.db.InWorkspace(ctx, l.workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status,target_id::text,source_updated_at,meta FROM import_items
			WHERE workspace_id=$1 AND source=$2 AND entity=$3 AND source_id=$4`, l.workspaceID, l.source, entity, sourceID).
			Scan(&entry.Status, &entry.TargetID, &entry.SourceUpdatedAt, &meta)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ledgerEntry{}, false, nil
	}
	if err != nil {
		return ledgerEntry{}, false, fmt.Errorf("read import ledger: %w", err)
	}
	if err := json.Unmarshal(meta, &entry.Meta); err != nil {
		return ledgerEntry{}, false, fmt.Errorf("read import ledger meta: %w", err)
	}
	return entry, true, nil
}

func (l ledger) done(ctx context.Context, entity, sourceID string) (string, bool, error) {
	entry, found, err := l.get(ctx, entity, sourceID)
	if err != nil || !found || entry.Status != ledgerDone || entry.TargetID == nil {
		return "", false, err
	}
	return *entry.TargetID, true, nil
}

type ledgerMark struct {
	entity, sourceID, parentID string
	targetID                   *string
	status, errText            string
	sourceUpdatedAt            *time.Time
	meta                       *itemMeta
}

// mark upserts a ledger row in q's transaction, or in its own when q is nil. A later mark never
// clears a known target id.
func (l ledger) mark(ctx context.Context, q execer, m ledgerMark) error {
	if q == nil {
		return l.db.InWorkspace(ctx, l.workspaceID, func(tx pgx.Tx) error { return l.mark(ctx, tx, m) })
	}
	meta := []byte(`{}`)
	if m.meta != nil {
		encoded, err := json.Marshal(m.meta)
		if err != nil {
			return fmt.Errorf("encode ledger meta: %w", err)
		}
		meta = encoded
	}
	var parent any
	if m.parentID != "" {
		parent = m.parentID
	}
	errText := m.errText
	if len(errText) > 500 {
		errText = errText[:500]
	}
	_, err := q.Exec(ctx, `INSERT INTO import_items(workspace_id,source,entity,source_id,parent_source_id,target_id,status,error,source_updated_at,meta)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (workspace_id,source,entity,source_id) DO UPDATE SET
		parent_source_id=COALESCE(EXCLUDED.parent_source_id,import_items.parent_source_id),
		target_id=COALESCE(EXCLUDED.target_id,import_items.target_id),
		status=EXCLUDED.status,error=EXCLUDED.error,
		source_updated_at=COALESCE(EXCLUDED.source_updated_at,import_items.source_updated_at),
		meta=CASE WHEN EXCLUDED.meta='{}'::jsonb THEN import_items.meta ELSE EXCLUDED.meta END,
		updated_at=now()`,
		l.workspaceID, l.source, m.entity, m.sourceID, parent, m.targetID, m.status, errText, m.sourceUpdatedAt, meta)
	if err != nil {
		return fmt.Errorf("write import ledger: %w", err)
	}
	return nil
}
