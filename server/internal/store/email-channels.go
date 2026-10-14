package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

const channelColumns = `support_address,secret_key_id IS NOT NULL,secret_rotated_at,created_at,updated_at`

func scanChannel(row pgx.Row) (domain.EmailChannel, error) {
	var channel domain.EmailChannel
	if err := row.Scan(&channel.SupportAddress, &channel.SecretSet, &channel.SecretRotatedAt, &channel.CreatedAt, &channel.UpdatedAt); err != nil {
		return domain.EmailChannel{}, err
	}
	channel.SecretRotatedAt = domain.UTCOptional(channel.SecretRotatedAt)
	channel.CreatedAt, channel.UpdatedAt = domain.UTC(channel.CreatedAt), domain.UTC(channel.UpdatedAt)
	return channel, nil
}

// GetEmailChannel returns the workspace channel, or nil when email is not connected.
func (s *Store) GetEmailChannel(ctx context.Context, workspaceID string) (*domain.EmailChannel, error) {
	channel, err := scanChannel(s.pool.QueryRow(ctx, `SELECT `+channelColumns+` FROM email_channels WHERE workspace_id=$1`, workspaceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get email channel: %w", err)
	}
	return &channel, nil
}

// UpsertEmailChannel connects email or changes the support address; an existing secret is kept.
func (s *Store) UpsertEmailChannel(ctx context.Context, workspaceID, supportAddress string) (domain.EmailChannel, error) {
	channel, err := scanChannel(s.pool.QueryRow(ctx, `INSERT INTO email_channels(workspace_id,support_address) VALUES($1,lower($2))
		ON CONFLICT(workspace_id) DO UPDATE SET support_address=EXCLUDED.support_address,updated_at=now()
		RETURNING `+channelColumns, workspaceID, supportAddress))
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("save email channel: %w", err)
	}
	return channel, nil
}

// DeleteEmailChannel disconnects email. Conversations, messages and delivery history stay.
func (s *Store) DeleteEmailChannel(ctx context.Context, workspaceID string) (bool, error) {
	result, err := s.pool.Exec(ctx, `DELETE FROM email_channels WHERE workspace_id=$1`, workspaceID)
	if err != nil {
		return false, fmt.Errorf("delete email channel: %w", err)
	}
	return result.RowsAffected() == 1, nil
}

// RotateEmailSecret replaces the inbound secret and returns the raw value once. The secret has the
// form "<key id>.<random>": the key id finds the channel, the hash of the whole value proves it.
func (s *Store) RotateEmailSecret(ctx context.Context, workspaceID string, now time.Time) (string, error) {
	keyID, err := randomURLToken(9)
	if err != nil {
		return "", err
	}
	random, err := randomURLToken(32)
	if err != nil {
		return "", err
	}
	secret := keyID + "." + random
	hash := sha256.Sum256([]byte(secret))
	result, err := s.pool.Exec(ctx, `UPDATE email_channels SET secret_key_id=$2,secret_hash=$3,secret_rotated_at=$4,updated_at=$4
		WHERE workspace_id=$1`, workspaceID, keyID, hash[:], now)
	if err != nil {
		return "", fmt.Errorf("rotate inbound secret: %w", err)
	}
	if result.RowsAffected() != 1 {
		return "", fmt.Errorf("rotate inbound secret: email channel not found")
	}
	return secret, nil
}

// WorkspaceForInboundSecret resolves the workspace that owns an inbound secret.
// The digest comparison is constant-time; an unknown key id and a wrong secret look the same.
func (s *Store) WorkspaceForInboundSecret(ctx context.Context, secret string) (string, bool, error) {
	keyID, _, found := strings.Cut(secret, ".")
	if !found || keyID == "" || len(secret) > 256 {
		return "", false, nil
	}
	var workspaceID string
	var stored []byte
	err := s.pool.QueryRow(ctx, `SELECT workspace_id::text,secret_hash FROM email_channels WHERE secret_key_id=$1`, keyID).Scan(&workspaceID, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		stored = make([]byte, sha256.Size)
	} else if err != nil {
		return "", false, fmt.Errorf("find inbound secret: %w", err)
	}
	hash := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(hash[:], stored) != 1 || workspaceID == "" {
		return "", false, nil
	}
	return workspaceID, true, nil
}

func randomURLToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
