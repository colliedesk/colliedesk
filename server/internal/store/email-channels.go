package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
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

// emailChannelColumns reads an email channel (e) with the state of its domain (d): the verification
// and the provider domain belong to the domain, not to one mailbox, so the channels of a workspace on
// one domain verify it once and share one provider domain. emailDomainJoin finds the channel that
// holds that state for e.
const emailChannelColumns = `e.channel_id::text,e.hosted_local,e.support_address,e.sender_name,e.secret_key_id IS NOT NULL,e.secret_rotated_at,` +
	`d.domain_verification_token,d.domain_verified_at,COALESCE(d.provider_domain_id,''),d.provider_domain_records,d.provider_domain_expired_at,e.created_at,e.updated_at`

// emailDomainJoin joins d, the domain holder of e: of the workspace's channels on e's domain, the one
// that is verified or holds a provider domain, else the oldest. Writes of domain state go to it.
const emailDomainJoin = ` JOIN LATERAL (SELECT * FROM email_channels d WHERE d.workspace_id=e.workspace_id
	AND split_part(d.support_address,'@',2)=split_part(e.support_address,'@',2)
	ORDER BY (d.domain_verified_at IS NOT NULL OR d.provider_domain_id IS NOT NULL) DESC,d.created_at,d.channel_id LIMIT 1) d ON true`

// emailChannelSelect is the start of a query of email channels, filtered by its caller on e.
const emailChannelSelect = `SELECT ` + emailChannelColumns + ` FROM email_channels e` + emailDomainJoin

// defaultEmailChannel is the id of the workspace's default email channel ($1).
const defaultEmailChannel = `(SELECT id FROM channels WHERE workspace_id=$1 AND type='email' AND is_default)`

// DomainRecordPrefix names the TXT record that proves control of the support address domain.
const DomainRecordPrefix = "_colliedesk."

func scanChannel(row pgx.Row) (domain.EmailChannel, error) {
	var channel domain.EmailChannel
	var token string
	var records []byte
	if err := row.Scan(&channel.ChannelID, &channel.HostedLocal, &channel.SupportAddress, &channel.SenderName, &channel.SecretSet, &channel.SecretRotatedAt, &token,
		&channel.DomainVerifiedAt, &channel.ProviderDomainID, &records, &channel.DNSRecordsExpiredAt, &channel.CreatedAt, &channel.UpdatedAt); err != nil {
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

// optionalChannel turns "no row" into nil.
func optionalChannel(channel domain.EmailChannel, err error) (*domain.EmailChannel, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get email channel: %w", err)
	}
	return &channel, nil
}

// GetEmailChannel returns the workspace's default email channel (its oldest live one), or nil when
// email is not connected. The routes that name no channel act on it.
func (s *Store) GetEmailChannel(ctx context.Context, workspaceID string) (*domain.EmailChannel, error) {
	return optionalChannel(scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		return scanChannel(tx.QueryRow(ctx, emailChannelSelect+` WHERE e.workspace_id=$1 AND e.channel_id=`+defaultEmailChannel, workspaceID))
	}))
}

// GetEmailChannelByID returns one email channel of the workspace, or nil when it has no such channel.
func (s *Store) GetEmailChannelByID(ctx context.Context, workspaceID, channelID string) (*domain.EmailChannel, error) {
	return optionalChannel(scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		return scanEmailChannelTx(ctx, tx, workspaceID, channelID)
	}))
}

func scanEmailChannelTx(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (domain.EmailChannel, error) {
	return scanChannel(tx.QueryRow(ctx, emailChannelSelect+` WHERE e.workspace_id=$1 AND e.channel_id=$2::uuid`, workspaceID, channelID))
}

// EmailChannelTx reads one email channel inside tx; nil when the workspace has no such channel.
func EmailChannelTx(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (*domain.EmailChannel, error) {
	return optionalChannel(scanEmailChannelTx(ctx, tx, workspaceID, channelID))
}

// UpsertEmailChannel connects email or changes the default channel's support address, through the
// channel frame: a workspace without email gets an email channel, one with it changes its default
// channel. See SaveEmailChannel for what changing the address keeps.
func (s *Store) UpsertEmailChannel(ctx context.Context, workspaceID, supportAddress string, senderName *string) (domain.EmailChannel, error) {
	return s.UpsertEmailChannelAs(ctx, workspaceID, supportAddress, senderName, ChannelActor{})
}

// UpsertEmailChannelAs is UpsertEmailChannel recording who made the change.
func (s *Store) UpsertEmailChannelAs(ctx context.Context, workspaceID, supportAddress string, senderName *string, actor ChannelActor) (domain.EmailChannel, error) {
	config := map[string]any{"support_address": supportAddress}
	if senderName != nil {
		config["sender_name"] = *senderName
	}
	patch, err := json.Marshal(config)
	if err != nil {
		return domain.EmailChannel{}, err
	}
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, domain.ChannelEmail)
		if err != nil {
			return domain.EmailChannel{}, err
		}
		if found {
			live, err = s.UpdateChannelTx(ctx, tx, workspaceID, live.ID, ChannelInput{Config: patch}, actor)
		} else {
			live, err = s.CreateChannelTx(ctx, tx, workspaceID, domain.ChannelEmail, ChannelInput{Config: patch}, actor)
		}
		if err != nil {
			return domain.EmailChannel{}, err
		}
		return scanEmailChannelTx(ctx, tx, workspaceID, live.ID)
	})
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("save email channel: %w", err)
	}
	return channel, nil
}

// lockEmailChannels serializes the changes of a workspace's email channels, which move domain state
// between them.
func lockEmailChannels(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	_, err := tx.Exec(ctx, `SELECT 1 FROM email_channels WHERE workspace_id=$1 FOR UPDATE`, workspaceID)
	return err
}

// releaseDomainState takes the domain state off a channel that leaves its domain (its address moves
// to another domain, or it is removed). Another channel of the workspace on that domain takes the
// verification and the provider domain over; with none left, the provider domain is queued for
// deletion at the provider. The caller holds lockEmailChannels.
func releaseDomainState(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) error {
	var providerID *string
	var held bool
	err := tx.QueryRow(ctx, `SELECT provider_domain_id,domain_verified_at IS NOT NULL OR provider_domain_id IS NOT NULL FROM email_channels
		WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID).Scan(&providerID, &held)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !held) {
		return nil
	}
	if err != nil {
		return err
	}
	var heir string
	err = tx.QueryRow(ctx, `SELECT o.channel_id::text FROM email_channels o JOIN email_channels e ON e.workspace_id=o.workspace_id AND e.channel_id=$2::uuid
		WHERE o.workspace_id=$1 AND o.channel_id<>e.channel_id AND split_part(o.support_address,'@',2)=split_part(e.support_address,'@',2)
		ORDER BY o.created_at,o.channel_id LIMIT 1`, workspaceID, channelID).Scan(&heir)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if heir == "" {
		if providerID != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO email_provider_domain_trash(workspace_id,provider_domain_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, workspaceID, *providerID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE email_channels SET domain_verified_at=NULL,provider_domain_id=NULL,provider_domain_records='[]'::jsonb,
			provider_domain_created_at=NULL,provider_domain_expired_at=NULL,provider_domain_first_verified_at=NULL WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID)
		return err
	}
	// The provider domain id is unique: read the state, clear it here, then hand it to the heir.
	var state struct {
		token                                     string
		verified, created, expired, firstVerified *time.Time
		records                                   []byte
	}
	if err := tx.QueryRow(ctx, `SELECT domain_verification_token,domain_verified_at,provider_domain_records,provider_domain_created_at,provider_domain_expired_at,
			provider_domain_first_verified_at FROM email_channels WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID).
		Scan(&state.token, &state.verified, &state.records, &state.created, &state.expired, &state.firstVerified); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE email_channels SET domain_verified_at=NULL,provider_domain_id=NULL,provider_domain_records='[]'::jsonb,provider_domain_created_at=NULL,
			provider_domain_expired_at=NULL,provider_domain_first_verified_at=NULL WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE email_channels SET domain_verification_token=$3,domain_verified_at=$4,provider_domain_id=$5,provider_domain_records=$6,
			provider_domain_created_at=$7,provider_domain_expired_at=$8,provider_domain_first_verified_at=$9,updated_at=now() WHERE workspace_id=$1 AND channel_id=$2::uuid`,
		workspaceID, heir, state.token, state.verified, providerID, state.records, state.created, state.expired, state.firstVerified)
	return err
}

// newHostedLocal is the local part of a new channel's hosted forwarding address: support for the
// workspace's first, a random one for the next, so no address of one channel guesses another's.
func newHostedLocal(ctx context.Context, tx pgx.Tx, workspaceID string) (string, error) {
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_channels WHERE workspace_id=$1 AND hosted_local='support')`, workspaceID).Scan(&taken); err != nil {
		return "", err
	}
	if !taken {
		return "support", nil
	}
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate forwarding address: %w", err)
	}
	return "support-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)), nil
}

// ErrEmailAddressTaken means another email channel of the workspace already uses the support address.
var ErrEmailAddressTaken = errors.New("another email channel of this workspace uses this address")

// SaveEmailChannel writes the email settings of a channel: a new support address, or a changed one.
// An existing secret is kept. A changed address loses its domain verification and gets a new
// verification token. A changed domain also loses the provider domain and its records, which another
// channel on the old domain takes over (releaseDomainState); the same domain keeps them. senderName,
// when given, replaces the name customers see; a new channel without one starts with the workspace name.
func SaveEmailChannel(ctx context.Context, tx pgx.Tx, workspaceID, channelID, supportAddress string, senderName *string) error {
	token, err := email.NewToken()
	if err != nil {
		return err
	}
	sender := ""
	if senderName != nil {
		sender = *senderName
	} else {
		var name, slug string
		if err := tx.QueryRow(ctx, `SELECT name,slug FROM workspaces WHERE id=$1`, workspaceID).Scan(&name, &slug); err != nil {
			return err
		}
		sender = domain.DefaultSenderName(name, slug)
	}
	if err := lockEmailChannels(ctx, tx, workspaceID); err != nil {
		return err
	}
	var leaves bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM email_channels WHERE workspace_id=$1 AND channel_id=$2::uuid
		AND split_part(support_address,'@',2)<>split_part(lower($3),'@',2))`, workspaceID, channelID, supportAddress).Scan(&leaves); err != nil {
		return err
	}
	if leaves {
		if err := releaseDomainState(ctx, tx, workspaceID, channelID); err != nil {
			return err
		}
	}
	local, err := newHostedLocal(ctx, tx, workspaceID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,sender_name,channel_id,hosted_local)
		VALUES($1,lower($2),$3,$4,$6,$7)
		ON CONFLICT(channel_id) DO UPDATE SET
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
			support_address=EXCLUDED.support_address,updated_at=now()`,
		workspaceID, supportAddress, token, sender, senderName != nil, channelID, local)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "email_channels_workspace_address_idx" {
		return ErrEmailAddressTaken
	}
	return err
}

// SetEmailSenderNameTx changes only the name customers see of one channel, inside tx.
// ErrEmailChannelMissing when the workspace has no such email channel.
func SetEmailSenderNameTx(ctx context.Context, tx pgx.Tx, workspaceID, channelID, senderName string) error {
	result, err := tx.Exec(ctx, `UPDATE email_channels SET sender_name=$3,updated_at=now() WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID, senderName)
	if err != nil {
		return fmt.Errorf("save sender name: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrEmailChannelMissing
	}
	return nil
}

// RemoveEmailChannel deletes the email settings of a channel inside tx. Its domain state goes to
// another channel on the domain, or its provider domain is queued for deletion at the provider;
// conversations, messages and delivery history stay.
func RemoveEmailChannel(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) error {
	if err := lockEmailChannels(ctx, tx, workspaceID); err != nil {
		return err
	}
	if err := releaseDomainState(ctx, tx, workspaceID, channelID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM email_channels WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID)
	return err
}

// SetEmailSenderName changes only the name customers see of the default channel, through the channel
// frame. The address, its domain verification and the provider domain stay. ErrEmailChannelMissing
// when email is not connected.
func (s *Store) SetEmailSenderName(ctx context.Context, workspaceID, senderName string) (domain.EmailChannel, error) {
	return s.SetEmailSenderNameAs(ctx, workspaceID, senderName, ChannelActor{})
}

// SetEmailSenderNameAs is SetEmailSenderName recording who made the change.
func (s *Store) SetEmailSenderNameAs(ctx context.Context, workspaceID, senderName string, actor ChannelActor) (domain.EmailChannel, error) {
	patch, err := json.Marshal(map[string]string{"sender_name": senderName})
	if err != nil {
		return domain.EmailChannel{}, err
	}
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, domain.ChannelEmail)
		if err != nil {
			return domain.EmailChannel{}, err
		}
		if !found {
			return domain.EmailChannel{}, ErrEmailChannelMissing
		}
		if _, err := s.UpdateChannelTx(ctx, tx, workspaceID, live.ID, ChannelInput{Config: patch}, actor); err != nil {
			return domain.EmailChannel{}, err
		}
		return scanEmailChannelTx(ctx, tx, workspaceID, live.ID)
	})
	if errors.Is(err, ErrEmailChannelMissing) || errors.Is(err, pgx.ErrNoRows) {
		return domain.EmailChannel{}, ErrEmailChannelMissing
	}
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("save sender name: %w", err)
	}
	return channel, nil
}

// ErrDomainRecordMissing means the verification TXT record was not found with the expected value.
var ErrDomainRecordMissing = errors.New("domain verification record not found")

// VerifyEmailDomain looks up the channel's TXT record and records the verification when it matches,
// for every channel of the workspace on that domain.
func (s *Store) VerifyEmailDomain(ctx context.Context, workspaceID, channelID string, lookupTXT func(context.Context, string) ([]string, error), now time.Time) (domain.EmailChannel, error) {
	channel, err := s.GetEmailChannelByID(ctx, workspaceID, channelID)
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
		// Only while the address and the token are the ones checked.
		if _, err := tx.Exec(ctx, `UPDATE email_channels SET domain_verified_at=$4,updated_at=$4 WHERE workspace_id=$1 AND channel_id=(
				SELECT d.channel_id FROM email_channels e`+emailDomainJoin+` WHERE e.workspace_id=$1 AND e.channel_id=$2::uuid AND e.support_address=$3)
			AND 'colliedesk-verification='||domain_verification_token=$5`, workspaceID, channelID, channel.SupportAddress, now, channel.DNSRecordValue); err != nil {
			return domain.EmailChannel{}, err
		}
		return scanEmailChannelTx(ctx, tx, workspaceID, channelID)
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

// domainHolderOf is the id of the channel that holds the domain state of channel $2 of workspace $1,
// while that channel still has the support address $3.
const domainHolderOf = `(SELECT d.channel_id FROM email_channels e` + emailDomainJoin + ` WHERE e.workspace_id=$1 AND e.channel_id=$2::uuid AND e.support_address=$3)`

// AttachProviderDomain stores a provider domain just created for the domain of the channel with this
// support address, but only while its domain still holds expectedID (empty for none). It reports false
// when the channel changed meanwhile, for example another request attached its own domain first; the
// caller then queues the domain it created for deletion. An id it replaces is queued here, in the same
// transaction. No provider call is made while the transaction is open.
func (s *Store) AttachProviderDomain(ctx context.Context, workspaceID, channelID, supportAddress, expectedID string, provider ProviderDomain, now time.Time) (domain.EmailChannel, bool, error) {
	records, err := json.Marshal(provider.Records)
	if err != nil {
		return domain.EmailChannel{}, false, fmt.Errorf("encode provider domain records: %w", err)
	}
	type outcome struct {
		channel  domain.EmailChannel
		attached bool
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (outcome, error) {
		if err := lockEmailChannels(ctx, tx, workspaceID); err != nil {
			return outcome{}, err
		}
		var holder, current string
		err := tx.QueryRow(ctx, `SELECT channel_id::text,COALESCE(provider_domain_id,'') FROM email_channels WHERE workspace_id=$1 AND channel_id=`+domainHolderOf,
			workspaceID, channelID, supportAddress).Scan(&holder, &current)
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
		if _, err := tx.Exec(ctx, `UPDATE email_channels SET provider_domain_id=$3,provider_domain_records=$4,domain_verified_at=NULL,provider_domain_first_verified_at=NULL,provider_domain_created_at=$5,provider_domain_expired_at=NULL,updated_at=$5
			WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, holder, provider.ID, records, now); err != nil {
			return outcome{}, err
		}
		channel, err := scanEmailChannelTx(ctx, tx, workspaceID, channelID)
		return outcome{channel: channel, attached: err == nil}, err
	})
	if err != nil {
		return domain.EmailChannel{}, false, fmt.Errorf("attach provider domain: %w", err)
	}
	return result.channel, result.attached, nil
}

// SaveProviderDomain stores the provider's record snapshot for the provider domain the channel's
// domain holds, and sets or clears the verification to match what the provider reports: a domain the
// provider no longer verifies stops being verified here. It writes only while the channel still has
// this support address and its domain this provider domain, and never for a domain queued for
// deletion, so a check that raced a change cannot bring an old domain back. pgx.ErrNoRows (wrapped)
// means the channel changed; ErrProviderDomainHeld means another workspace has the name verified.
func (s *Store) SaveProviderDomain(ctx context.Context, workspaceID, channelID, supportAddress string, provider ProviderDomain, now time.Time) (domain.EmailChannel, error) {
	records, err := json.Marshal(provider.Records)
	if err != nil {
		return domain.EmailChannel{}, fmt.Errorf("encode provider domain records: %w", err)
	}
	channel, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.EmailChannel, error) {
		result, err := tx.Exec(ctx, `UPDATE email_channels SET provider_domain_records=$5,
				domain_verified_at=CASE WHEN $6 THEN COALESCE(domain_verified_at,$7) END,
				provider_domain_first_verified_at=CASE WHEN $6 THEN COALESCE(provider_domain_first_verified_at,$7) ELSE provider_domain_first_verified_at END,updated_at=$7
			WHERE workspace_id=$1 AND channel_id=`+domainHolderOf+` AND provider_domain_id=$4
				AND NOT EXISTS (SELECT 1 FROM email_provider_domain_trash WHERE provider_domain_id=$4)`,
			workspaceID, channelID, supportAddress, provider.ID, records, provider.Verified, now)
		if err != nil {
			return domain.EmailChannel{}, err
		}
		if result.RowsAffected() == 0 {
			return domain.EmailChannel{}, pgx.ErrNoRows
		}
		return scanEmailChannelTx(ctx, tx, workspaceID, channelID)
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

// DeleteEmailChannel disconnects the default email channel: it is removed through the frame.
// Conversations, messages and delivery history stay. Its domain state goes to another channel on the
// domain, or its provider domain is queued for deletion at the provider.
func (s *Store) DeleteEmailChannel(ctx context.Context, workspaceID string) (bool, error) {
	return s.DeleteEmailChannelAs(ctx, workspaceID, ChannelActor{})
}

// DeleteEmailChannelAs is DeleteEmailChannel recording who removed it.
func (s *Store) DeleteEmailChannelAs(ctx context.Context, workspaceID string, actor ChannelActor) (bool, error) {
	deleted, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (bool, error) {
		live, found, err := s.defaultChannelOfType(ctx, tx, workspaceID, domain.ChannelEmail)
		if err != nil || !found {
			return false, err
		}
		return true, s.RemoveChannelTx(ctx, tx, workspaceID, live.ID, actor)
	})
	if err != nil {
		return false, fmt.Errorf("delete email channel: %w", err)
	}
	return deleted, nil
}

// RotateEmailSecretAs replaces the inbound secret of an email channel and returns the raw value once,
// recording in the channel's history who replaced it (never the secret). The secret has the form
// "<key id>.<random>": the key id finds the channel, the hash of the whole value proves it.
func (s *Store) RotateEmailSecretAs(ctx context.Context, workspaceID, channelID string, now time.Time, actor ChannelActor) (string, error) {
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
		result, err := tx.Exec(ctx, `UPDATE email_channels SET secret_key_id=$3,secret_hash=$4,secret_rotated_at=$5,updated_at=$5
			WHERE workspace_id=$1 AND channel_id=$2::uuid`, workspaceID, channelID, keyID, hash[:], now)
		if err != nil || result.RowsAffected() != 1 {
			return false, err
		}
		channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
		if err != nil {
			return false, err
		}
		return true, insertChannelEvent(ctx, tx, channel, "credential_changed", actor, map[string]any{"name": "inbound_secret"})
	})
	if err != nil {
		return "", fmt.Errorf("rotate inbound secret: %w", err)
	}
	if !rotated {
		return "", fmt.Errorf("rotate inbound secret: email channel not found")
	}
	return secret, nil
}

// ChannelForInboundSecret resolves the workspace and the email channel that own an inbound secret.
// The digest comparison is constant-time; an unknown key id and a wrong secret look the same.
func (s *Store) ChannelForInboundSecret(ctx context.Context, secret string) (workspaceID, channelID string, ok bool, err error) {
	keyID, _, found := strings.Cut(secret, ".")
	if !found || keyID == "" || len(secret) > 256 {
		return "", "", false, nil
	}
	var stored []byte
	err = s.db.Privileged(ctx, database.PrivilegeInboundSecret, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text,channel_id::text,secret_hash FROM email_channels WHERE secret_key_id=$1`, keyID).Scan(&workspaceID, &channelID, &stored)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		stored = make([]byte, sha256.Size)
	} else if err != nil {
		return "", "", false, fmt.Errorf("find inbound secret: %w", err)
	}
	hash := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(hash[:], stored) != 1 || workspaceID == "" {
		return "", "", false, nil
	}
	return workspaceID, channelID, true, nil
}

// ChannelForHostedAddress resolves the email channel that receives mail at a hosted forwarding address
// <local>@<slug>.<hosted domain> of a workspace: the channel whose forwarding local part it is, else
// the workspace's default email channel, as every local part reached the one channel before. found is
// false when the workspace has no email channel.
func (s *Store) ChannelForHostedAddress(ctx context.Context, workspaceID, local string) (string, bool, error) {
	id, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*string, error) {
		var id *string
		err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT e.channel_id FROM email_channels e JOIN channels c ON c.workspace_id=e.workspace_id AND c.id=e.channel_id
				WHERE e.workspace_id=$1 AND e.hosted_local=lower($2) AND c.deleted_at IS NULL),`+defaultEmailChannel+`)::text`, workspaceID, local).Scan(&id)
		return id, err
	})
	if err != nil {
		return "", false, fmt.Errorf("route hosted address: %w", err)
	}
	if id == nil {
		return "", false, nil
	}
	return *id, true, nil
}

// OwnEmailAddresses lists what mail of the workspace comes from: the support addresses of its email
// channels and the Gmail accounts they send through.
func (s *Store) OwnEmailAddresses(ctx context.Context, workspaceID string) (support, gmail []string, err error) {
	type lists struct{ support, gmail []string }
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (lists, error) {
		var got lists
		err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(support_address ORDER BY created_at),'{}'),
				COALESCE((SELECT array_agg(address ORDER BY connected_at) FROM mailbox_connections WHERE workspace_id=$1),'{}')
			FROM email_channels WHERE workspace_id=$1`, workspaceID).Scan(&got.support, &got.gmail)
		return got, err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list own email addresses: %w", err)
	}
	return got.support, got.gmail, nil
}

func randomURLToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
