// Package channelemail is the email channel: a support address that receives mail forwarded to it and sends
// replies through SMTP or the workspace's Gmail. Its configuration is the email_channels table, its
// input the message parser of internal/email, its output the sender of internal/emailoutbox.
package channelemail

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	mailpkg "github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Type is the email channel's type.
const Type channels.Type = domain.ChannelEmail

// Window and limits of inbound mail: a loop guard per sender and an abuse ceiling on new
// conversations per workspace, whatever channel opens them (store.InboundRateLimit and friends).
var defaults = channels.Settings{
	JoinPolicy: channels.JoinPolicy{Kind: channels.JoinAlwaysReopen},
	RateLimits: channels.RateLimits{WindowSeconds: int(store.InboundRateWindow.Seconds()), PerSender: store.InboundRateLimit,
		NewConversations: store.InboundNewConversationLimit, Scope: channels.ScopeWorkspace},
}

// Config is what the email channel needs of the server.
type Config struct {
	// TrustedAuthServID is the authserv-id whose Authentication-Results the parser trusts.
	TrustedAuthServID string
	// HostedMailDomain is the hosted forwarding domain; an address on another workspace's subdomain is refused.
	HostedMailDomain string
	// PlatformForwarding is set when this server receives mail through its own forwarder (the
	// hosted cloud), so a workspace needs no inbound secret.
	PlatformForwarding bool
	// Cloud refuses sender names of brands, as the hosted cloud does for workspace names.
	Cloud bool
	// Outbound says whether replies can be sent now (store.OutboundPolicy).
	Outbound store.OutboundPolicy
	// Sender sends replies; nil sends nothing (a test of the frame).
	Sender *emailoutbox.Worker
	// RevokeGrant revokes a Gmail grant at Google after the channel holding it was removed; nil revokes nothing.
	RevokeGrant func(grant *store.SealedGrant, workspaceID, channelID string)
}

// Adapter is the email channel type.
type Adapter struct{ config Config }

// New returns the email channel type.
func New(config Config) *Adapter { return &Adapter{config: config} }

func (a *Adapter) Definition() channels.Definition {
	return channels.Definition{Type: Type, SharedIdentity: true, Identified: true, Name: "Email", Guide: "email-channel.md",
		Capabilities: []string{"threads", "attachments_in", "attachments_out", "cc", "html"},
		ReplyTo:      channels.IdentityEmail, CarriesOtherReplies: true, WhenOff: channels.OffHold, Defaults: defaults,
		ActivationEvent: "email_channel_saved"}
}

func (a *Adapter) Config() channels.ConfigPart     { return config{a} }
func (a *Adapter) Inbound() channels.InboundPart   { return inbound{a} }
func (a *Adapter) Outbound() channels.OutboundPart { return outbound{a} }

// ConfigValue is the email channel's configuration as the API takes it.
type ConfigValue struct {
	SupportAddress *string `json:"support_address,omitempty"`
	SenderName     *string `json:"sender_name,omitempty"`
}

// Messages of refused configuration, the same the settings page has always shown.
const (
	supportAddressRequired = "A plain support address such as support@example.com is required."
	senderNameInvalid      = "sender_name must be 1 to 100 characters without line breaks or other control characters."
	brandNameMessage       = "This name is not available. Choose another name, or contact us if it is your own brand."
	foreignHosted          = "This address belongs to another workspace's forwarding address."
)

// PlainAddress accepts a bare addr-spec only: no display name, no comments, no line breaks.
func PlainAddress(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Name == "" && parsed.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n<>")
}

type config struct{ a *Adapter }

func fieldError(field, message string) error {
	return &channels.FieldError{Fields: map[string]string{field: message}, Message: message}
}

func (c config) Validate(ctx context.Context, tx pgx.Tx, workspaceID string, current *channels.Channel, patch json.RawMessage) (channels.ConfigChange, error) {
	var value ConfigValue
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &value); err != nil {
			return channels.ConfigChange{}, fieldError("support_address", supportAddressRequired)
		}
	}
	if value.SupportAddress == nil && value.SenderName == nil {
		if current == nil {
			return channels.ConfigChange{}, fieldError("support_address", supportAddressRequired)
		}
		return channels.ConfigChange{}, nil
	}
	detail := map[string]any{}
	if value.SupportAddress != nil {
		if !PlainAddress(*value.SupportAddress) {
			return channels.ConfigChange{}, fieldError("support_address", supportAddressRequired)
		}
		address := strings.ToLower(*value.SupportAddress)
		if hosted := strings.ToLower(c.a.config.HostedMailDomain); hosted != "" {
			var slug string
			if err := tx.QueryRow(ctx, `SELECT slug FROM workspaces WHERE id=$1`, workspaceID).Scan(&slug); err != nil {
				return channels.ConfigChange{}, fmt.Errorf("load workspace: %w", err)
			}
			host := strings.ToLower(mailpkg.Domain(address))
			if mailpkg.UnderDomain(address, hosted) && host != hosted && host != slug+"."+hosted {
				return channels.ConfigChange{}, fieldError("support_address", foreignHosted)
			}
		}
		// One @ and no + at the start, so the mailbox it names is read the same everywhere (email.MailboxKey).
		if strings.Count(address, "@") != 1 || mailpkg.MailboxKey(address) == "" || strings.HasPrefix(address, "+") {
			return channels.ConfigChange{}, fieldError("support_address", supportAddressRequired)
		}
		value.SupportAddress = &address
		detail["support_address"] = address
	}
	if value.SenderName != nil {
		name, ok := domain.NormalizeHeaderName(*value.SenderName)
		if !ok {
			return channels.ConfigChange{}, fieldError("sender_name", senderNameInvalid)
		}
		if _, denied := domain.DeniedBrand(name); denied && c.a.config.Cloud {
			return channels.ConfigChange{}, &channels.FieldError{Fields: map[string]string{"sender_name": brandNameMessage}, Message: brandNameMessage, Code: "name_not_allowed"}
		}
		value.SenderName = &name
		detail["sender_name"] = name
	}
	if current == nil && value.SupportAddress == nil {
		return channels.ConfigChange{}, fieldError("support_address", supportAddressRequired)
	}
	change := channels.ConfigChange{Value: value, Detail: detail}
	if current == nil {
		// A second mailbox is called by its address unless it is given a name.
		change.Name = *value.SupportAddress
	}
	return change, nil
}

func (c config) Save(ctx context.Context, tx pgx.Tx, channel channels.Channel, change channels.ConfigChange) (channels.Event, error) {
	value, ok := change.Value.(ConfigValue)
	if !ok {
		return channels.Event{}, nil
	}
	if value.SupportAddress != nil {
		return channels.Event{Kind: "config_changed", Detail: change.Detail},
			store.SaveEmailChannel(ctx, tx, channel.WorkspaceID, channel.ID, *value.SupportAddress, value.SenderName)
	}
	return channels.Event{Kind: "config_changed", Detail: change.Detail}, store.SetEmailSenderNameTx(ctx, tx, channel.WorkspaceID, channel.ID, *value.SenderName)
}

func (c config) Public(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	return store.EmailChannelTx(ctx, tx, channel.WorkspaceID, channel.ID)
}

func (c config) Summary(ctx context.Context, tx pgx.Tx, channel channels.Channel) (string, error) {
	settings, err := store.EmailChannelTx(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil || settings == nil {
		return "", err
	}
	return settings.SupportAddress, nil
}

func (c config) Credentials() []channels.CredentialSpec { return nil }

// Status reasons of the email channel.
const (
	ReasonSendingPaused    = "sending_paused"
	ReasonCannotSend       = "cannot_send"
	ReasonGmailBroken      = "gmail_broken"
	ReasonDomainUnverified = "domain_unverified"
	ReasonNoInboundSecret  = "no_inbound_secret"
)

func (c config) Status(ctx context.Context, tx pgx.Tx, channel channels.Channel) (channels.Status, error) {
	var paused, verified, secret bool
	var mailboxStatus *string
	settings, err := store.EmailChannelTx(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil {
		return channels.Status{}, err
	}
	if settings == nil {
		return channels.Status{State: channels.StateNotConnected}, nil
	}
	verified, secret = settings.DomainVerifiedAt != nil, settings.SecretSet
	err = tx.QueryRow(ctx, `SELECT w.email_paused_at IS NOT NULL,(SELECT status FROM mailbox_connections m WHERE m.workspace_id=w.id AND m.channel_id=$2::uuid)
		FROM workspaces w WHERE w.id=$1`, channel.WorkspaceID, channel.ID).
		Scan(&paused, &mailboxStatus)
	if err != nil {
		return channels.Status{}, fmt.Errorf("read email channel state: %w", err)
	}
	policy := c.a.config.Outbound
	attention := func(code string) (channels.Status, error) {
		return channels.Status{State: channels.StateNeedsAttention, ReasonCode: code}, nil
	}
	switch {
	case paused:
		return attention(ReasonSendingPaused)
	case mailboxStatus != nil && *mailboxStatus == domain.MailboxFailed:
		return attention(ReasonGmailBroken)
	case mailboxStatus != nil && !policy.GmailEnabled, mailboxStatus == nil && !policy.Enabled:
		return attention(ReasonCannotSend)
	case mailboxStatus == nil && policy.RequireVerifiedDomain && !verified && policy.FallbackDomain == "":
		return attention(ReasonDomainUnverified)
	case !secret && !c.a.config.PlatformForwarding:
		return attention(ReasonNoInboundSecret)
	}
	return channels.Status{State: channels.StateWorking}, nil
}

// BeforeRemove reads the Gmail grant the channel holds, which goes with it, so it is revoked at Google
// once the removal committed.
func (c config) BeforeRemove(ctx context.Context, tx pgx.Tx, channel channels.Channel) (func(context.Context), error) {
	if c.a.config.RevokeGrant == nil {
		return nil, nil
	}
	var grant store.SealedGrant
	err := tx.QueryRow(ctx, `SELECT token_key_id,refresh_token_sealed,address FROM mailbox_connections WHERE workspace_id=$1 AND channel_id=$2::uuid`, channel.WorkspaceID, channel.ID).
		Scan(&grant.KeyID, &grant.Sealed, &grant.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read gmail grant: %w", err)
	}
	return func(context.Context) { c.a.config.RevokeGrant(&grant, channel.WorkspaceID, channel.ID) }, nil
}

func (c config) Remove(ctx context.Context, tx pgx.Tx, channel channels.Channel) error {
	return store.RemoveEmailChannel(ctx, tx, channel.WorkspaceID, channel.ID)
}

// snapshot is what the parser needs to drop our own mail and copies: the channel's support address,
// and every address mail of the workspace leaves from (its mailboxes and the Gmail accounts they send
// through).
type snapshot struct {
	Support string
	Own     []string
}

func (c config) Snapshot(ctx context.Context, tx pgx.Tx, channel channels.Channel) (any, error) {
	settings, err := store.EmailChannelTx(ctx, tx, channel.WorkspaceID, channel.ID)
	if err != nil || settings == nil {
		return snapshot{}, err
	}
	own, err := store.OwnEmailAddressesTx(ctx, tx, channel.WorkspaceID)
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{Support: settings.SupportAddress, Own: own}, nil
}

// ownAddress reports whether mail from address comes from the workspace itself.
func (s snapshot) ownAddress(address string) bool {
	if address == s.Support {
		return true
	}
	return slices.ContainsFunc(s.Own, func(own string) bool { return mailpkg.SameMailbox(address, own) })
}

// Envelope is what the inbound route learned of a message beside its bytes, kept in the journal so a
// held message parses the same way when it is opened later.
type Envelope struct {
	MessageID string   `json:"message_id"`
	From      string   `json:"from"`
	Platform  bool     `json:"platform"`
	Cc        []string `json:"cc,omitempty"`
}

type inbound struct{ a *Adapter }

func (inbound) Mode() channels.IngestMode { return channels.Sync }

const bounceReason = "The recipient's mail server bounced this reply."

// Parse makes a message of a parsed email (what the inbound route read), or of a held message's
// stored original and its envelope.
func (i inbound) Parse(_ context.Context, _ channels.Channel, snapshotOf any, raw channels.Raw) ([]channels.Inbound, error) {
	own, _ := snapshotOf.(snapshot)
	supportAddress := own.Support
	parsed, ok := raw.Parsed.(mailpkg.Inbound)
	body := raw.Body
	if !ok {
		var envelope Envelope
		if err := json.Unmarshal(raw.JSON, &envelope); err != nil {
			return nil, fmt.Errorf("read held email envelope: %w", err)
		}
		reader, err := gzip.NewReader(bytes.NewReader(raw.Body))
		if err != nil {
			return nil, fmt.Errorf("open held email: %w", err)
		}
		if body, err = io.ReadAll(reader); err != nil {
			return nil, fmt.Errorf("read held email: %w", err)
		}
		if parsed, err = mailpkg.Parse(body, i.a.config.TrustedAuthServID); err != nil {
			return nil, err
		}
		parsed.Platform, parsed.Cc = envelope.Platform, envelope.Cc
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(body); err != nil {
		return nil, fmt.Errorf("compress original message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("compress original message: %w", err)
	}
	in := channels.Inbound{ExternalID: parsed.MessageID, Sender: channels.Sender{Identity: channels.Identity{Kind: channels.IdentityEmail, Value: parsed.FromAddress},
		Name: parsed.FromName, Email: parsed.FromAddress}, HTML: parsed.BodyHTML, StorageBound: storageBound(parsed)}
	in.Thread.References = append(in.Thread.References, parsed.InReplyTo...)
	for index := len(parsed.References) - 1; index >= 0; index-- {
		in.Thread.References = append(in.Thread.References, parsed.References[index])
	}
	if token := mailpkg.ReplyToken(parsed.Recipients); token != "" {
		in.Thread.Keys = []string{"reply-token:" + token}
	}
	in.Subject = parsed.Subject
	if in.Subject == "" {
		in.Subject = "(no subject)"
	}
	in.Text = parsed.Body
	if strings.TrimSpace(in.Text) == "" {
		in.Text = "(no text content)"
	}
	in.Files = append(in.Files, channels.InboundFile{Name: "original-message.eml.gz", ContentType: "application/gzip", Data: compressed.Bytes(), Original: true})
	for _, file := range parsed.Files {
		in.Files = append(in.Files, channels.InboundFile{Name: file.Filename, ContentType: file.ContentType, Data: file.Data, Inline: file.Embedded})
	}
	via := domain.ViaWorkspace
	if parsed.Platform {
		via = domain.ViaPlatform
	}
	in.EmailMeta = &domain.IncomingEmail{From: parsed.FromAddress, Authentication: parsed.Authentication, Via: via, Attachments: parsed.Attachments,
		CC: copiedAddresses(parsed, supportAddress)}
	in.Summary, _ = json.Marshal(Envelope{MessageID: parsed.MessageID, From: parsed.FromAddress, Platform: parsed.Platform, Cc: parsed.Cc})
	switch {
	case own.ownAddress(parsed.FromAddress):
		in.DropReason = mailpkg.DropOwnAddress
	case parsed.DropReason == mailpkg.DropMailerDaemon && len(parsed.BouncedMessageIDs) > 0:
		in.Kind = channels.KindDeliveryUpdate
		in.Delivery = &channels.DeliveryUpdate{References: parsed.BouncedMessageIDs, Recipients: parsed.BouncedRecipients, Reason: bounceReason}
		in.DropReason = parsed.DropReason
	case parsed.DropReason != "":
		in.DropReason = parsed.DropReason
	}
	return []channels.Inbound{in}, nil
}

// storageBound is the most attachment storage one message can use: the raw message (stored
// gzip-compressed, so smaller) and its decoded files.
func storageBound(in mailpkg.Inbound) int64 {
	total := int64(in.RawBytes)
	for _, file := range in.Files {
		total += int64(len(file.Data))
	}
	return total
}

// copiedAddresses are the other people on the customer's Cc line: not the sender, not the support
// address or a plus form of it (the mail reached us through it).
func copiedAddresses(in mailpkg.Inbound, supportAddress string) []string {
	var kept []string
	for _, address := range in.Cc {
		if address != in.FromAddress && !mailpkg.SameMailbox(address, supportAddress) {
			kept = append(kept, address)
		}
	}
	return kept
}

type outbound struct{ a *Adapter }

func (o outbound) Prepare(ctx context.Context, tx pgx.Tx, request channels.PrepareRequest) (channels.Prepared, error) {
	return o.sender().Prepare(ctx, tx, request)
}

func (o outbound) Send(ctx context.Context, out channels.Outbound, kit channels.SendKit) channels.SendResult {
	return o.sender().Send(ctx, out, kit)
}

func (o outbound) sender() *emailoutbox.Worker {
	if o.a.config.Sender != nil {
		return o.a.config.Sender
	}
	return emailoutbox.New(nil, nil, emailoutbox.Options{})
}
