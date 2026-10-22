package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
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
	"github.com/jackc/pgx/v5/pgconn"
)

const channelColumns = `support_address,sender_name,secret_key_id IS NOT NULL,secret_rotated_at,domain_verification_token,domain_verified_at,COALESCE(provider_domain_id,''),provider_domain_records,provider_domain_expired_at,created_at,updated_at`

// DomainRecordPrefix names the TXT record that proves control of the support address domain.
const DomainRecordPrefix = "_colliedesk."

func scanChannel(row pgx.Row) (domain.EmailChannel, error) {
	var channel domain.EmailChannel
	var token string
	var records []byte
	if err := row.Scan(&channel.SupportAddress, &channel.SenderName, &channel.SecretSet, &channel.SecretRotatedAt, &token, &channel.DomainVerifiedAt,
		&channel.ProviderDomainID, &records, &channel.DNSRecordsExpiredAt, &channel.CreatedAt, &channel.UpdatedAt); err != nil {
		return domain.EmailChannel{}, err
	}
	channel.DNSRecords = []domain.DNSRecord{}
	if err := json.Unmarshal(records, &channel.DNSRecords); err != nil {
		return domain.EmailChannel{}, fmt.Errorf("read provider domain records: %w", err)
	}
	channel.Domain = email.Domain(channel.SupportAddress)
	channel.DNSRecordName = DomainRecordPrefix + channel.Domain
	channel.DNSRecordValue = "colliedesk-verification=" + token
	channel.DomainVerifiedAt = domain.UTCOptional(channel.DomainVerifiedAt)
	channel.DNSRecordsExpiredAt = domain.UTCOptional(channel.DNSRecordsExpiredAt)
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
// A changed address loses its domain verification and gets a new verification token. A changed domain
// also loses the provider domain and its records; the same domain keeps them. senderName, when given,
// replaces the name customers see; a new channel without one starts with the workspace name.
func (s *Store) UpsertEmailChannel(ctx context.Context, workspaceID, supportAddress string, senderName *string) (domain.EmailChannel, error) {
	token, err := email.NewToken()
	if err != nil {
		return domain.EmailChannel{}, err
	}
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		sender := ""
		if senderName != nil {
			sender = *senderName
		} else {
			var name, slug string
			if err := tx.QueryRow(ctx, `SELECT name,slug FROM workspaces WHERE id=$1`, workspaceID).Scan(&name, &slug); err != nil {
				return domain.EmailChannel{}, err
			}
			sender = domain.DefaultSenderName(name, slug)
		}
		// The provider domain of the old address is no longer used when the domain changes.
		if _, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id)
			SELECT workspace_id,provider_domain_id FROM email_channels
			WHERE workspace_id=$1 AND provider_domain_id IS NOT NULL AND split_part(support_address,'@',2)<>split_part(lower($2),'@',2)
			ON CONFLICT DO NOTHING`, workspaceID, supportAddress); err != nil {
			return domain.EmailChannel{}, err
		}
		return scanChannel(tx.QueryRow(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,sender_name)
			VALUES($1,lower($2),$3,$4)
			ON CONFLICT(workspace_id) DO UPDATE SET
				sender_name=CASE WHEN $5 THEN EXCLUDED.sender_name ELSE email_channels.sender_name END,
				domain_verification_token=CASE WHEN email_channels.support_address=EXCLUDED.support_address
					THEN email_channels.domain_verification_token ELSE EXCLUDED.domain_verification_token END,
				domain_verified_at=CASE WHEN email_channels.support_address=EXCLUDED.support_address
					THEN email_channels.domain_verified_at END,
				provider_domain_id=CASE WHEN split_part(email_channels.support_address,'@',2)=split_part(EXCLUDED.support_address,'@',2)
					THEN email_channels.provider_domain_id END,
				provider_domain_records=CASE WHEN split_part(email_channels.support_address,'@',2)=split_part(EXCLUDED.support_address,'@',2)
					THEN email_channels.provider_domain_records ELSE '[]'::jsonb END,
				provider_domain_created_at=CASE WHEN split_part(email_channels.support_address,'@',2)=split_part(EXCLUDED.support_address,'@',2)
					THEN email_channels.provider_domain_created_at END,
				provider_domain_expired_at=CASE WHEN split_part(email_channels.support_address,'@',2)=split_part(EXCLUDED.support_address,'@',2)
					THEN email_channels.provider_domain_expired_at END,
				provider_domain_first_verified_at=CASE WHEN split_part(email_channels.support_address,'@',2)=split_part(EXCLUDED.support_address,'@',2)
					THEN email_channels.provider_domain_first_verified_at END,
				support_address=EXCLUDED.support_address,updated_at=now()
			RETURNING `+channelColumns, workspaceID, supportAddress, token, sender, senderName != nil))
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

// ProviderDomain is what the email provider reported for the channel's domain.
type ProviderDomain struct {
	ID       string
	Records  []domain.DNSRecord
	Verified bool
}

// ErrProviderDomainHeld means another workspace already has this domain name verified through the provider.
var ErrProviderDomainHeld = errors.New("domain is verified by another workspace")

// AttachProviderDomain stores a provider domain just created for the channel with this support address,
// but only while the channel still holds expectedID (empty for none). It reports false when the channel
// changed meanwhile, for example another request attached its own domain first; the caller then queues
// the domain it created for deletion. An id it replaces is queued here, in the same transaction. No
// provider call is made while the transaction is open.
func (s *Store) AttachProviderDomain(ctx context.Context, workspaceID, supportAddress, expectedID string, provider ProviderDomain, now time.Time) (domain.EmailChannel, bool, error) {
	records, err := json.Marshal(provider.Records)
	if err != nil {
		return domain.EmailChannel{}, false, fmt.Errorf("encode provider domain records: %w", err)
	}
	type outcome struct {
		channel  domain.EmailChannel
		attached bool
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (outcome, error) {
		var current string
		err := tx.QueryRow(ctx, `SELECT COALESCE(provider_domain_id,'') FROM email_channels WHERE workspace_id=$1 AND support_address=$2 FOR UPDATE`,
			workspaceID, supportAddress).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && current != expectedID) {
			return outcome{}, nil
		}
		if err != nil {
			return outcome{}, err
		}
		if current != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, workspaceID, current); err != nil {
				return outcome{}, err
			}
		}
		channel, err := scanChannel(tx.QueryRow(ctx, `UPDATE email_channels SET provider_domain_id=$3,provider_domain_records=$4,domain_verified_at=NULL,provider_domain_first_verified_at=NULL,provider_domain_created_at=$5,provider_domain_expired_at=NULL,updated_at=$5
			WHERE workspace_id=$1 AND support_address=$2 RETURNING `+channelColumns, workspaceID, supportAddress, provider.ID, records, now))
		return outcome{channel: channel, attached: err == nil}, err
	})
	if err != nil {
		return domain.EmailChannel{}, false, fmt.Errorf("attach provider domain: %w", err)
	}
	return result.channel, result.attached, nil
}

// SaveProviderDomain stores the provider's record snapshot for the provider domain the channel holds, and
// sets or clears the verification to match what the provider reports: a domain the provider no longer
// verifies stops being verified here. It writes only while the channel still has this support address and
// this provider domain, and never for a domain queued for deletion, so a check that raced a change cannot
// bring an old domain back. pgx.ErrNoRows (wrapped) means the channel changed; ErrProviderDomainHeld
// means another workspace has the name verified.
func (s *Store) SaveProviderDomain(ctx context.Context, workspaceID, supportAddress string, provider ProviderDomain, now time.Time) (domain.EmailChannel, error) {
	records, err := json.Marshal(provider.Records)
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("encode provider domain records: %w", err)
	}
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		return scanChannel(tx.QueryRow(ctx, `UPDATE email_channels SET provider_domain_records=$4,
				domain_verified_at=CASE WHEN $5 THEN COALESCE(domain_verified_at,$6) END,
				provider_domain_first_verified_at=CASE WHEN $5 THEN COALESCE(provider_domain_first_verified_at,$6) ELSE provider_domain_first_verified_at END,updated_at=$6
			WHERE workspace_id=$1 AND support_address=$2 AND provider_domain_id=$3
				AND NOT EXISTS (SELECT 1 FROM email_provider_domain_trash WHERE provider_domain_id=$3)
			RETURNING `+channelColumns, workspaceID, supportAddress, provider.ID, records, provider.Verified, now))
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.EmailChannel{}, ErrProviderDomainHeld
	}
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("save provider domain: %w", err)
	}
	return channel, nil
}

// DeleteEmailChannel disconnects email. Conversations, messages and delivery history stay. The provider
// domain is queued for deletion at the provider.
func (s *Store) DeleteEmailChannel(ctx context.Context, workspaceID string) (bool, error) {
	deleted, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id)
			SELECT workspace_id,provider_domain_id FROM email_channels WHERE workspace_id=$1 AND provider_domain_id IS NOT NULL
			ON CONFLICT DO NOTHING`, workspaceID); err != nil {
			return false, err
		}
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
