package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
)

// ErrObjectMissing means a stored file that was about to be referenced is gone. Writing the request
// again stores it afresh.
var ErrObjectMissing = errors.New("a stored file is missing; try again")

// WithObjects returns a store that verifies attachment objects against the given storage before a
// row references them.
func (s *Store) WithObjects(storage attachments.Storage) *Store {
	copy := *s
	copy.objects = storage
	return &copy
}

// DiscardObject deletes the stored object when no attachment row references it.
func (s *Store) DiscardObject(ctx context.Context, workspaceID, key string) error {
	return DiscardObject(ctx, s.db, s.objects, workspaceID, key)
}

// lockObjects serializes deleting an object against referencing it: every path that deletes or
// references an object holds this transaction-scoped lock for its key. Keys are locked in sorted order
// so two transactions with several keys cannot deadlock.
func lockObjects(ctx context.Context, tx pgx.Tx, keys []string) error {
	sorted := slices.Clone(keys)
	slices.Sort(sorted)
	for _, key := range slices.Compact(sorted) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
			return fmt.Errorf("lock attachment object: %w", err)
		}
	}
	return nil
}

// DiscardObject deletes the object when no attachment of the workspace references it. Objects are
// content-addressed, so one object can back several attachments. The reference check and the delete
// happen under the object's lock, so a request that stored the same bytes and is about to commit its
// row is waited for, never undercut.
func DiscardObject(ctx context.Context, db *database.DB, storage attachments.Storage, workspaceID, key string) error {
	tx, err := db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("begin discard: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := lockObjects(ctx, tx, []string{key}); err != nil {
		return err
	}
	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE workspace_id=$1 AND storage_key=$2)`, workspaceID, key).Scan(&used); err != nil {
		return fmt.Errorf("check attachment key: %w", err)
	}
	if !used {
		if err := storage.Delete(ctx, key); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// VerifyObjects takes the lock of every object, then checks it exists, so a row never points at bytes
// a concurrent cleanup removed. Call it in the transaction that inserts the rows, before inserting.
// A nil storage only locks.
func VerifyObjects(ctx context.Context, tx pgx.Tx, storage attachments.Storage, keys ...string) error {
	if err := lockObjects(ctx, tx, keys); err != nil {
		return err
	}
	if storage == nil {
		return nil
	}
	for _, key := range keys {
		if err := attachments.Exists(ctx, storage, key); errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrObjectMissing, key)
		} else if err != nil {
			return fmt.Errorf("check stored file: %w", err)
		}
	}
	return nil
}

// CheckAttachmentAdmission refuses early what CreateMessage would refuse after the bytes were
// written: no room left under the plan's storage cap, and a reply the trial does not allow. The checks
// inside CreateMessage stay authoritative.
func (s *Store) CheckAttachmentAdmission(ctx context.Context, workspaceID, conversationID, kind string) error {
	_, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if err := s.CheckStorage(ctx, tx, workspaceID, 1, s.now()); err != nil {
			return struct{}{}, err
		}
		if kind != "outgoing" {
			return struct{}{}, nil
		}
		var verified bool
		err := tx.QueryRow(ctx, `SELECT domain_verified_at IS NOT NULL FROM email_channels WHERE workspace_id=$1`, workspaceID).Scan(&verified)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, nil
		}
		if err != nil {
			return struct{}{}, fmt.Errorf("load email channel: %w", err)
		}
		return struct{}{}, s.checkTrialOutbound(ctx, tx, workspaceID, conversationID, verified)
	})
	return err
}
