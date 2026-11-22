package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// KBBulkEntry is what undo needs for one article a bulk action changed: how to put it back (Undo),
// the fields to put back (Before), the article's token just after the action (AfterToken; undo
// leaves the article when it differs) and, for a review, its id (Ref).
type KBBulkEntry struct {
	ArticleID  string
	Before     json.RawMessage
	AfterToken *time.Time
	Undo       string
	Ref        string
}

// RecordKBBulkOperation keeps a bulk action and the entries undo can put back, in their order, and
// returns its id and when it ran.
func (s *Store) RecordKBBulkOperation(ctx context.Context, workspaceID string, actor Actor, action string, params json.RawMessage, entries []KBBulkEntry) (string, time.Time, error) {
	type recorded struct {
		id  string
		ran time.Time
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	made, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (recorded, error) {
		var made recorded
		if err := tx.QueryRow(ctx, `INSERT INTO kb_bulk_operations(workspace_id,action,params,created_by_agent_id,created_by_token_id)
			SELECT $1,$2,$3,$4,t.id FROM (SELECT 1) one LEFT JOIN sessions t ON t.workspace_id=$1 AND t.id=$5
			RETURNING id::text, created_at`, workspaceID, action, params, optionalID(actor.AgentID), optionalID(actor.TokenID)).Scan(&made.id, &made.ran); err != nil {
			return made, fmt.Errorf("insert knowledge base bulk action: %w", err)
		}
		// COPY is refused on a table with row-level security, so the entries go in as arrays.
		ids, befores, tokens, undos, refs := make([]string, len(entries)), make([]string, len(entries)),
			make([]*time.Time, len(entries)), make([]string, len(entries)), make([]*string, len(entries))
		for i, entry := range entries {
			ids[i], befores[i], tokens[i], undos[i], refs[i] = entry.ArticleID, `{}`, entry.AfterToken, entry.Undo, optionalID(entry.Ref)
			if len(entry.Before) > 0 {
				befores[i] = string(entry.Before)
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO kb_bulk_operation_articles(workspace_id,operation_id,article_id,ordinal,before,after_token,undo,ref)
			SELECT $1,$2,e.article_id,e.ordinal-1,e.before,e.after_token,e.undo,e.ref
			FROM unnest($3::uuid[],$4::jsonb[],$5::timestamptz[],$6::text[],$7::text[])
				WITH ORDINALITY AS e(article_id,before,after_token,undo,ref,ordinal)`,
			workspaceID, made.id, ids, befores, tokens, undos, refs)
		return made, err
	})
	return made.id, made.ran, err
}

// KBBulkOperation is a bulk action claimed for undo, with the entries to put back in their order.
type KBBulkOperation struct {
	ID       string
	Action   string
	UndoneAt time.Time
	Entries  []KBBulkEntry
}

// ClaimKBBulkUndo marks the bulk action id undone by actor and returns what to put back. It refuses an
// id this workspace has no action for, an action undone already and one older than the undo window.
// The claim comes first, so two undos at once never both put the articles back.
func (s *Store) ClaimKBBulkUndo(ctx context.Context, workspaceID string, actor Actor, id string) (KBBulkOperation, error) {
	return s.kbBulkUndo(ctx, workspaceID, id, func(tx pgx.Tx, operation *KBBulkOperation) error {
		return tx.QueryRow(ctx, `UPDATE kb_bulk_operations SET undone_at=clock_timestamp(), undone_by_agent_id=$3,
			undone_by_token_id=(SELECT t.id FROM sessions t WHERE t.workspace_id=$1 AND t.id=$4)
			WHERE workspace_id=$1 AND id=$2 RETURNING undone_at`, workspaceID, id, optionalID(actor.AgentID), optionalID(actor.TokenID)).Scan(&operation.UndoneAt)
	})
}

// KBBulkUndoable returns what undoing the bulk action id would put back without claiming it, so a caller
// who may not do all of it is refused while the action stays undoable. It refuses what ClaimKBBulkUndo does.
func (s *Store) KBBulkUndoable(ctx context.Context, workspaceID, id string) (KBBulkOperation, error) {
	return s.kbBulkUndo(ctx, workspaceID, id, nil)
}

// kbBulkUndo reads the bulk action id and its entries, refusing one that is missing, undone or too old.
// claim, when given, runs after that check with the action's row locked.
func (s *Store) kbBulkUndo(ctx context.Context, workspaceID, id string, claim func(pgx.Tx, *KBBulkOperation) error) (KBBulkOperation, error) {
	if !savedReplyID.MatchString(id) {
		return KBBulkOperation{}, domain.ErrKBBulkOperationNotFound
	}
	lock := ""
	if claim != nil {
		lock = " FOR UPDATE"
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (KBBulkOperation, error) {
		operation := KBBulkOperation{ID: id}
		var undone *time.Time
		var expired bool
		err := tx.QueryRow(ctx, `SELECT action, undone_at, created_at <= clock_timestamp() - make_interval(secs => $3)
			FROM kb_bulk_operations WHERE workspace_id=$1 AND id=$2`+lock, workspaceID, id, domain.KBBulkUndoWindow.Seconds()).Scan(&operation.Action, &undone, &expired)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return operation, domain.ErrKBBulkOperationNotFound
		case err != nil:
			return operation, err
		case undone != nil:
			return operation, domain.ErrKBBulkUndone
		case expired:
			return operation, domain.ErrKBBulkExpired
		}
		if claim != nil {
			if err := claim(tx, &operation); err != nil {
				return operation, err
			}
		}
		rows, err := tx.Query(ctx, `SELECT article_id::text, before, after_token, undo, COALESCE(ref,'') FROM kb_bulk_operation_articles
			WHERE workspace_id=$1 AND operation_id=$2 ORDER BY ordinal`, workspaceID, id)
		if err != nil {
			return operation, err
		}
		operation.Entries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (KBBulkEntry, error) {
			var entry KBBulkEntry
			err := row.Scan(&entry.ArticleID, &entry.Before, &entry.AfterToken, &entry.Undo, &entry.Ref)
			return entry, err
		})
		return operation, err
	})
}

// KBTrashedArticleToken is the token of an article in the trash, which restoring it checks.
func (s *Store) KBTrashedArticleToken(ctx context.Context, workspaceID, id string) (time.Time, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (time.Time, error) {
		var token time.Time
		err := tx.QueryRow(ctx, `SELECT `+kbArticleToken+` FROM kb_articles a WHERE a.workspace_id=$1 AND a.id=$2 AND a.deleted_at IS NOT NULL`, workspaceID, id).Scan(&token)
		if errors.Is(err, pgx.ErrNoRows) {
			return token, domain.ErrKBArticleNotFound
		}
		return token, err
	})
}
