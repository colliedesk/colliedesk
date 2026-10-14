package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

const channelColumns = `support_address,secret_key_id IS NOT NULL,secret_rotated_at,domain_verification_token,domain_verified_at,created_at,updated_at`

// DomainRecordPrefix names the TXT record that proves control of the support address domain.
const DomainRecordPrefix = "_colliedesk."

func scanChannel(row pgx.Row) (domain.EmailChannel, error) {
	var channel domain.EmailChannel
	var token string
	if err := row.Scan(&channel.SupportAddress, &channel.SecretSet, &channel.SecretRotatedAt, &token, &channel.DomainVerifiedAt,
		&channel.CreatedAt, &channel.UpdatedAt); err != nil {
		return domain.EmailChannel{}, err
	}
	channel.Domain = email.Domain(channel.SupportAddress)
	channel.DNSRecordName = DomainRecordPrefix + channel.Domain
	channel.DNSRecordValue = "colliedesk-verification=" + token
	channel.DomainVerifiedAt = domain.UTCOptional(channel.DomainVerifiedAt)
	channel.SecretRotatedAt = domain.UTCOptional(channel.SecretRotatedAt)
	channel.CreatedAt, channel.UpdatedAt = domain.UTC(channel.CreatedAt), domain.UTC(channel.UpdatedAt)
	return channel, nil
}

// GetEmailChannel returns the workspace channel, or nil when email is not connected.
func (s *Store) GetEmailChannel(ctx context.Context, workspaceID string) (*domain.EmailChannel, error) {
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		return scanChannel(tx.QueryRow(ctx, `SELECT `+channelColumns+` FROM email_channels WHERE workspace_id=$1`, workspaceID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get email channel: %w", err)
	}
	return &channel, nil
}

// UpsertEmailChannel connects email or changes the support address; an existing secret is kept.
// A changed address loses its domain verification and gets a new verification token.
func (s *Store) UpsertEmailChannel(ctx context.Context, workspaceID, supportAddress string) (domain.EmailChannel, error) {
	token, err := email.NewToken()
	if err != nil {
		return domain.EmailChannel{}, err
	}
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		return scanChannel(tx.QueryRow(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token)
			VALUES($1,lower($2),$3)
			ON CONFLICT(workspace_id) DO UPDATE SET
				domain_verification_token=CASE WHEN email_channels.support_address=EXCLUDED.support_address
					THEN email_channels.domain_verification_token ELSE EXCLUDED.domain_verification_token END,
				domain_verified_at=CASE WHEN email_channels.support_address=EXCLUDED.support_address
					THEN email_channels.domain_verified_at END,
				support_address=EXCLUDED.support_address,updated_at=now()
			RETURNING `+channelColumns, workspaceID, supportAddress, token))
	})
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("save email channel: %w", err)
	}
	return channel, nil
}

// ErrDomainRecordMissing means the verification TXT record was not found with the expected value.
var ErrDomainRecordMissing = errors.New("domain verification record not found")

// VerifyEmailDomain looks up the channel's TXT record and records the verification when it matches.
func (s *Store) VerifyEmailDomain(ctx context.Context, workspaceID string, lookupTXT func(context.Context, string) ([]string, error), now time.Time) (domain.EmailChannel, error) {
	channel, err := s.GetEmailChannel(ctx, workspaceID)
	if err != nil {
		return domain.EmailChannel{}, err
	}
	if channel == nil {
		return domain.EmailChannel{}, fmt.Errorf("verify email domain: email channel not found")
	}
	values, err := lookupTXT(ctx, channel.DNSRecordName)
	var dnsErr *net.DNSError
	if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
		return domain.EmailChannel{}, fmt.Errorf("look up %s: %w", channel.DNSRecordName, err)
	}
	if !slices.Contains(values, channel.DNSRecordValue) {
		return *channel, ErrDomainRecordMissing
	}
	verified, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		return scanChannel(tx.QueryRow(ctx, `UPDATE email_channels SET domain_verified_at=$3,updated_at=$3
			WHERE workspace_id=$1 AND support_address=$2 RETURNING `+channelColumns, workspaceID, channel.SupportAddress, now))
	})
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("record domain verification: %w", err)
	}
	return verified, nil
}

// DeleteEmailChannel disconnects email. Conversations, messages and delivery history stay.
func (s *Store) DeleteEmailChannel(ctx context.Context, workspaceID string) (bool, error) {
	deleted, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `DELETE FROM email_channels WHERE workspace_id=$1`, workspaceID)
		return result.RowsAffected() == 1, err
	})
	if err != nil {
		return false, fmt.Errorf("delete email channel: %w", err)
	}
	return deleted, nil
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
	rotated, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		result, err := tx.Exec(ctx, `UPDATE email_channels SET secret_key_id=$2,secret_hash=$3,secret_rotated_at=$4,updated_at=$4
			WHERE workspace_id=$1`, workspaceID, keyID, hash[:], now)
		return result.RowsAffected() == 1, err
	})
	if err != nil {
		return "", fmt.Errorf("rotate inbound secret: %w", err)
	}
	if !rotated {
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
	err := s.db.Privileged(ctx, database.PrivilegeInboundSecret, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text,secret_hash FROM email_channels WHERE secret_key_id=$1`, keyID).Scan(&workspaceID, &stored)
	})
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
