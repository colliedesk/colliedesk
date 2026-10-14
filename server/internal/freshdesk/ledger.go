package freshdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Ledger entities. A ledger row maps one Freshdesk id to one CollieDesk row
// and makes the import idempotent: a re-run skips every row marked done.
const (
	entityContact     = "contact"
	entityTicket      = "ticket"
	entityDescription = "description"
	entityMessage     = "conversation"
	entityAttachment  = "attachment"
	entityTimeEntry   = "time_entry"
	entityTag         = "tag"

	ledgerDone    = "done"
	ledgerFailed  = "failed"
	ledgerSkipped = "skipped"
)

// Reasons stored in the error column of skipped tickets.
const (
	skipDeleted = "deleted_at_source"
	skipTrashed = "trashed"
	skipSpam    = "spam"
	skipAbsent  = "absent"
)

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ticketMeta records what Freshdesk held for a ticket when it was imported,
// so a re-run that skips the ticket can still report source counts.
type ticketMeta struct {
	// Created marks contacts and tags the import created (undo removes only those).
	Created         bool  `json:"created,omitempty"`
	Archived        bool  `json:"archived"`
	Messages        int   `json:"messages"`
	Attachments     int   `json:"attachments"`
	AttachmentBytes int64 `json:"attachment_bytes"`
	TimeEntries     int   `json:"time_entries"`
}

type ledgerEntry struct {
	Status          string
	TargetID        *string
	Error           string
	SourceUpdatedAt *time.Time
	Meta            ticketMeta
}

type ledger struct {
	pool        *pgxpool.Pool
	workspaceID string
	source      string
}

func (l ledger) get(ctx context.Context, entity, sourceID string) (ledgerEntry, bool, error) {
	var entry ledgerEntry
	var meta []byte
	err := l.pool.QueryRow(ctx, `SELECT status,target_id::text,error,source_updated_at,meta FROM import_items
		WHERE workspace_id=$1 AND source=$2 AND entity=$3 AND source_id=$4`, l.workspaceID, l.source, entity, sourceID).
		Scan(&entry.Status, &entry.TargetID, &entry.Error, &entry.SourceUpdatedAt, &meta)
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
	meta                       *ticketMeta
}

// mark upserts a ledger row. A later mark never clears a known target id.
func (l ledger) mark(ctx context.Context, q execer, m ledgerMark) error {
	if q == nil {
		q = l.pool
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
	_, err := q.Exec(ctx, `INSERT INTO import_items(workspace_id,source,entity,source_id,parent_source_id,target_id,status,error,source_updated_at,meta)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (workspace_id,source,entity,source_id) DO UPDATE SET
		parent_source_id=COALESCE(EXCLUDED.parent_source_id,import_items.parent_source_id),
		target_id=COALESCE(EXCLUDED.target_id,import_items.target_id),
		status=EXCLUDED.status,error=EXCLUDED.error,
		source_updated_at=COALESCE(EXCLUDED.source_updated_at,import_items.source_updated_at),
		meta=CASE WHEN EXCLUDED.meta='{}'::jsonb THEN import_items.meta ELSE EXCLUDED.meta END,
		updated_at=now()`,
		l.workspaceID, l.source, m.entity, m.sourceID, parent, m.targetID, m.status, truncate(m.errText, 500), m.sourceUpdatedAt, meta)
	if err != nil {
		return fmt.Errorf("write import ledger: %w", err)
	}
	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
