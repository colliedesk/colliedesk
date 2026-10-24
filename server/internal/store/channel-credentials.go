package store

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5"
)

// ErrSealKeysMissing means the server has no MAILBOX_TOKEN_KEYS, so a channel secret cannot be kept.
var ErrSealKeysMissing = errors.New("this server has no MAILBOX_TOKEN_KEYS, so channel secrets cannot be stored")

// WithSealKeys returns a store that seals channel secrets with the keys (MAILBOX_TOKEN_KEYS).
func (s *Store) WithSealKeys(keys tokenseal.Keys) *Store {
	copy := *s
	copy.sealKeys = keys
	return &copy
}

func credentialContext(workspaceID, channelID, name string) string {
	return "channel:" + workspaceID + ":" + channelID + ":" + name
}

// credentialHint is the last four characters of a secret, the only part shown again.
func credentialHint(value string) string {
	if utf8.RuneCountInString(value) <= 4 {
		return ""
	}
	runes := []rune(value)
	return string(runes[len(runes)-4:])
}

// SetChannelCredential seals and stores a secret the channel's type declares, and records the change
// with its hint. It returns the hint.
func (s *Store) SetChannelCredential(ctx context.Context, workspaceID, channelID, name, value string, actor ChannelActor) (string, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (string, error) {
		channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
		if err != nil {
			return "", err
		}
		if !channel.Live() {
			return "", channels.ErrNotFound
		}
		if !s.sealKeys.Configured() {
			return "", ErrSealKeysMissing
		}
		adapter, err := s.adapterOf(channel.Type)
		if err != nil {
			return "", err
		}
		declared := false
		for _, spec := range adapter.Config().Credentials() {
			declared = declared || spec.Name == name
		}
		if !declared || value == "" {
			return "", &channels.FieldError{Fields: map[string]string{"name": "This channel keeps no secret by that name."}, Message: "Check the secret."}
		}
		keyID, sealed, err := s.sealKeys.Seal([]byte(value), credentialContext(workspaceID, channelID, name))
		if err != nil {
			return "", err
		}
		hint := credentialHint(value)
		if _, err := tx.Exec(ctx, `INSERT INTO channel_credentials(workspace_id,channel_id,name,key_id,sealed,hint,updated_by_agent_id)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,'')::uuid)
			ON CONFLICT(workspace_id,channel_id,name) DO UPDATE SET key_id=EXCLUDED.key_id,sealed=EXCLUDED.sealed,hint=EXCLUDED.hint,
				expires_at=NULL,version=channel_credentials.version+1,updated_by_agent_id=EXCLUDED.updated_by_agent_id,updated_at=now()`,
			workspaceID, channelID, name, keyID, sealed, hint, actor.AgentID); err != nil {
			return "", fmt.Errorf("store channel secret: %w", err)
		}
		return hint, insertChannelEvent(ctx, tx, channel, "credential_changed", actor, map[string]any{"name": name, "hint": hint})
	})
}

// OpenChannelCredential opens a channel's secret with its version; found is false when none is stored.
func (s *Store) OpenChannelCredential(ctx context.Context, workspaceID, channelID, name string) (string, int64, bool, error) {
	type sealed struct {
		keyID   string
		value   []byte
		version int64
		found   bool
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (sealed, error) {
		var row sealed
		err := tx.QueryRow(ctx, `SELECT key_id,sealed,version FROM channel_credentials WHERE workspace_id=$1 AND channel_id=$2 AND name=$3`,
			workspaceID, channelID, name).Scan(&row.keyID, &row.value, &row.version)
		if errors.Is(err, pgx.ErrNoRows) {
			return row, nil
		}
		row.found = err == nil
		return row, err
	})
	if err != nil || !got.found {
		return "", 0, false, err
	}
	value, err := s.sealKeys.Open(got.keyID, got.value, credentialContext(workspaceID, channelID, name))
	if err != nil {
		return "", 0, false, fmt.Errorf("open channel secret: %w", err)
	}
	return string(value), got.version, true, nil
}

// ReplaceChannelCredential stores a new value only while the stored version is still version, so of
// two senders refreshing a token that may be used once, one wins and the other reads the new token.
func (s *Store) ReplaceChannelCredential(ctx context.Context, workspaceID, channelID, name, value string, version int64, expiresAt *time.Time) (bool, error) {
	if !s.sealKeys.Configured() {
		return false, ErrSealKeysMissing
	}
	keyID, sealed, err := s.sealKeys.Seal([]byte(value), credentialContext(workspaceID, channelID, name))
	if err != nil {
		return false, err
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(ctx, `UPDATE channel_credentials SET key_id=$4,sealed=$5,hint=NULLIF($6,''),expires_at=$7,version=version+1,updated_at=now()
			WHERE workspace_id=$1 AND channel_id=$2 AND name=$3 AND version=$8`,
			workspaceID, channelID, name, keyID, sealed, credentialHint(value), expiresAt, version)
		return tag.RowsAffected() == 1, err
	})
}

// RefreshChannelCredential locks a secret and, when its version is still seen, replaces it with what
// refresh returns; otherwise another sender refreshed it, and its value is returned as is. The lock
// is held while refresh runs, so two senders never spend a token that may be used once.
func (s *Store) RefreshChannelCredential(ctx context.Context, workspaceID, channelID, name string, seen int64, refresh channels.Refresh) (string, int64, error) {
	if !s.sealKeys.Configured() {
		return "", 0, ErrSealKeysMissing
	}
	type state struct {
		value   string
		version int64
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (state, error) {
		var keyID string
		var sealed []byte
		var version int64
		if err := tx.QueryRow(ctx, `SELECT key_id,sealed,version FROM channel_credentials WHERE workspace_id=$1 AND channel_id=$2 AND name=$3 FOR UPDATE`,
			workspaceID, channelID, name).Scan(&keyID, &sealed, &version); err != nil {
			return state{}, fmt.Errorf("lock channel secret: %w", err)
		}
		current, err := s.sealKeys.Open(keyID, sealed, credentialContext(workspaceID, channelID, name))
		if err != nil {
			return state{}, fmt.Errorf("open channel secret: %w", err)
		}
		if version != seen {
			return state{string(current), version}, nil
		}
		next, expiresAt, err := refresh(ctx, string(current))
		if err != nil {
			return state{}, err
		}
		nextKey, nextSealed, err := s.sealKeys.Seal([]byte(next), credentialContext(workspaceID, channelID, name))
		if err != nil {
			return state{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_credentials SET key_id=$4,sealed=$5,hint=NULLIF($6,''),expires_at=$7,version=version+1,updated_at=now()
			WHERE workspace_id=$1 AND channel_id=$2 AND name=$3`, workspaceID, channelID, name, nextKey, nextSealed, credentialHint(next), expiresAt); err != nil {
			return state{}, fmt.Errorf("store refreshed secret: %w", err)
		}
		return state{next, version + 1}, nil
	})
	return got.value, got.version, err
}
