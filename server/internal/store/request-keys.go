package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

// ErrRequestInProgress means the first request under a key is still running after a repeat waited for it.
var ErrRequestInProgress = errors.New("a request with this idempotency key is still running")

const (
	// requestKeyWait bounds how long a repeat waits for the first request under its key to finish.
	requestKeyWait = 10 * time.Second
	// requestKeyAbandoned is the age after which a key whose request never finished (the server stopped
	// mid-way) is taken over by the next request.
	requestKeyAbandoned = 2 * time.Minute
)

// CreateOnce runs create at most once per workspace, operation and key (the client's Idempotency-Key)
// within a day, and returns the JSON of what it made. A repeat returns the first answer with replayed
// true and runs nothing; a key whose fingerprint differs is ErrRequestKeyReused. The key is claimed in a
// short transaction of its own before create runs, so no connection is held while create takes others: a
// repeat that arrives while the first still runs polls for its answer, without a connection, for up to 10
// seconds (then ErrRequestInProgress). An empty key runs create without remembering anything. When create
// fails the claim is released, so a retry runs it again. The answer is recorded even when the client went
// away meanwhile, so its retry finds what was made.
func (s *Store) CreateOnce(ctx context.Context, workspaceID, operation, key, fingerprint string, create func(context.Context) (any, error)) (json.RawMessage, bool, error) {
	if key == "" {
		made, err := create(ctx)
		if err != nil {
			return nil, false, err
		}
		answer, err := json.Marshal(made)
		return answer, false, err
	}
	deadline := time.Now().Add(requestKeyWait)
	for {
		answer, claimed, err := s.claimRequestKey(ctx, workspaceID, operation, key, fingerprint)
		switch {
		case err != nil:
			return nil, false, err
		case answer != nil:
			return answer, true, nil
		case claimed:
			return s.runClaimed(ctx, workspaceID, operation, key, create)
		case time.Now().After(deadline):
			return nil, false, ErrRequestInProgress
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// claimRequestKey reads the key and, when nobody holds it, claims it for this request. It answers the
// stored answer of a finished request, claimed true when this request may run, or neither while another
// request under the key is still running.
func (s *Store) claimRequestKey(ctx context.Context, workspaceID, operation, key, fingerprint string) (json.RawMessage, bool, error) {
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, false, fmt.Errorf("begin create key: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "request-key:"+workspaceID+":"+operation+":"+key); err != nil {
		return nil, false, fmt.Errorf("lock create key: %w", err)
	}
	var stored string
	var answer []byte
	var abandoned bool
	err = tx.QueryRow(ctx, `SELECT fingerprint, response, response IS NULL AND created_at < now() - make_interval(secs => $4)
		FROM request_idempotency_keys WHERE workspace_id=$1 AND operation=$2 AND key=$3 AND created_at > now() - interval '1 day'`,
		workspaceID, operation, key, requestKeyAbandoned.Seconds()).Scan(&stored, &answer, &abandoned)
	switch {
	case err == nil && stored != fingerprint:
		return nil, false, ErrRequestKeyReused
	case err == nil && answer != nil:
		return answer, false, nil
	case err == nil && !abandoned:
		return nil, false, nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, false, fmt.Errorf("read create key: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM request_idempotency_keys WHERE workspace_id=$1 AND created_at <= now() - interval '1 day'`, workspaceID); err != nil {
		return nil, false, fmt.Errorf("drop old create keys: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO request_idempotency_keys(workspace_id,operation,key,fingerprint) VALUES($1,$2,$3,$4)
		ON CONFLICT (workspace_id,operation,key) DO UPDATE SET fingerprint=EXCLUDED.fingerprint, response=NULL, created_at=now()`,
		workspaceID, operation, key, fingerprint); err != nil {
		return nil, false, fmt.Errorf("claim create key: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit create key: %w", err)
	}
	return nil, true, nil
}

// runClaimed runs create under a claimed key, then records its answer, or releases the key when it failed.
func (s *Store) runClaimed(ctx context.Context, workspaceID, operation, key string, create func(context.Context) (any, error)) (json.RawMessage, bool, error) {
	// What was made stays made when the client goes away: the key must still name it.
	finish := context.WithoutCancel(ctx)
	// A release that fails leaves the claim, which the next request takes over once it is abandoned.
	release := func() {
		_ = s.db.InWorkspace(finish, workspaceID, func(tx pgx.Tx) error {
			_, err := tx.Exec(finish, `DELETE FROM request_idempotency_keys WHERE workspace_id=$1 AND operation=$2 AND key=$3 AND response IS NULL`, workspaceID, operation, key)
			return err
		})
	}
	made, err := create(ctx)
	if err != nil {
		release()
		return nil, false, err
	}
	answer, err := json.Marshal(made)
	if err != nil {
		release()
		return nil, false, fmt.Errorf("encode created answer: %w", err)
	}
	if err := s.db.InWorkspace(finish, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(finish, `UPDATE request_idempotency_keys SET response=$4 WHERE workspace_id=$1 AND operation=$2 AND key=$3`, workspaceID, operation, key, answer)
		return err
	}); err != nil {
		return nil, false, fmt.Errorf("record created answer: %w", err)
	}
	return answer, false, nil
}
