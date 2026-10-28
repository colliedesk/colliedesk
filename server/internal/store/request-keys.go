package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrRequestKeyReused means a create key already names a request of the workspace with other input: the
// client reused a key for something new.
var ErrRequestKeyReused = errors.New("the idempotency key already names another request")

// RequestFingerprint hashes what makes two create requests the same one: their author and input.
func RequestFingerprint(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		// The length keeps ("ab","c") and ("a","bc") apart.
		fmt.Fprintf(hash, "%d:%s|", len(part), part)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// CreateOnce runs create at most once per workspace, operation and key (the client's Idempotency-Key)
// within a day, and returns the JSON of what it made. A repeat returns the first answer with replayed
// true and runs nothing; a key whose fingerprint differs is ErrRequestKeyReused. A lock on the key makes
// requests that arrive at once wait for the first, so they find its answer instead of creating their own.
// An empty key runs create without remembering anything. When create fails nothing is remembered, so a
// retry runs it again.
func (s *Store) CreateOnce(ctx context.Context, workspaceID, operation, key, fingerprint string, create func(context.Context) (any, error)) (json.RawMessage, bool, error) {
	if key == "" {
		made, err := create(ctx)
		if err != nil {
			return nil, false, err
		}
		answer, err := json.Marshal(made)
		return answer, false, err
	}
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, false, fmt.Errorf("begin create key: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "request-key:"+workspaceID+":"+operation+":"+key); err != nil {
		return nil, false, fmt.Errorf("lock create key: %w", err)
	}
	var stored string
	var answer json.RawMessage
	err = tx.QueryRow(ctx, `SELECT fingerprint, response FROM request_idempotency_keys
		WHERE workspace_id=$1 AND operation=$2 AND key=$3 AND created_at > now() - interval '1 day'`,
		workspaceID, operation, key).Scan(&stored, &answer)
	switch {
	case err == nil && stored != fingerprint:
		return nil, false, ErrRequestKeyReused
	case err == nil:
		return answer, true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, false, fmt.Errorf("read create key: %w", err)
	}
	made, err := create(ctx)
	if err != nil {
		return nil, false, err
	}
	if answer, err = json.Marshal(made); err != nil {
		return nil, false, fmt.Errorf("encode created answer: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM request_idempotency_keys WHERE workspace_id=$1 AND created_at <= now() - interval '1 day'`, workspaceID); err != nil {
		return nil, false, fmt.Errorf("drop old create keys: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO request_idempotency_keys(workspace_id,operation,key,fingerprint,response) VALUES($1,$2,$3,$4,$5)`,
		workspaceID, operation, key, fingerprint, answer); err != nil {
		return nil, false, fmt.Errorf("remember create key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit create key: %w", err)
	}
	return answer, false, nil
}
