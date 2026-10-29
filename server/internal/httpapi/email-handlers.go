package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const inboundEmailPath = "/api/v1/channels/email/inbound"

type smtpStatus struct {
	Configured    bool   `json:"configured"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	StartTLS      bool   `json:"starttls"`
	Authenticated bool   `json:"authenticated"`
}

type emailChannelSettings struct {
	Channel     *domain.EmailChannel `json:"channel"`
	InboundURL  string               `json:"inbound_url"`
	SMTP        smtpStatus           `json:"smtp"`
	FromPolicy  string               `json:"from_policy"`
	SendingFrom *string              `json:"sending_from"`
	// HostedAddress is where to forward mail on the hosted cloud; nil when that feature is off.
	HostedAddress *string `json:"hosted_address"`
	// SendingPause is set while the workspace's email sending is paused after spam complaints.
	SendingPause *domain.EmailPause `json:"sending_pause"`
	// DomainVerification is "provider" when the email provider lists the DNS records to publish, else "txt".
	DomainVerification string `json:"domain_verification"`
	// Gmail and Microsoft say whether each can be connected and which account is; a channel has one
	// mailbox connection, shown under its provider.
	Gmail     gmailSettings     `json:"gmail"`
	Microsoft microsoftSettings `json:"microsoft"`
}

// emailSettings describes the channel's sending. serverRefused is whether a reply of it waits because
// Microsoft refused this server's credentials.
func (a *API) emailSettings(workspaceSlug string, channel *domain.EmailChannel, pause *domain.EmailPause, mailbox *domain.MailboxConnection, serverRefused bool) emailChannelSettings {
	config := a.config.Email
	smtp := config.SMTP
	channelID := ""
	if channel != nil {
		channelID = channel.ChannelID
	} else {
		// The connection belongs to the channel; without one there is nothing connected.
		mailbox = nil
	}
	var gmail, microsoftConnection *domain.MailboxConnection
	if mailbox != nil && mailbox.Provider == domain.MailboxMicrosoft {
		microsoftConnection = mailbox
	} else {
		gmail = mailbox
	}
	settings := emailChannelSettings{Channel: channel, InboundURL: a.config.Origin + inboundEmailPath, FromPolicy: config.FromPolicy, DomainVerification: "txt", SendingPause: pause,
		SMTP:  smtpStatus{Configured: smtp.Configured(), Host: smtp.Host, Port: smtp.Port, StartTLS: smtp.StartTLS, Authenticated: smtp.Username != ""},
		Gmail: a.gmailSettings(gmail), Microsoft: a.microsoftSettings(channelID, microsoftConnection)}
	// Only while the server has its Microsoft settings: without them a waiting reply fails as not configured.
	settings.Microsoft.ServerCredentials = serverRefused && microsoftConnection != nil && settings.Microsoft.Available
	if a.resend != nil {
		settings.DomainVerification = "provider"
	}
	if settings.FromPolicy == "" {
		settings.FromPolicy = email.FromPolicyVerified
	}
	// The first channel forwards to support@; a channel added beside it has an address of its own.
	local := "support"
	if channel != nil {
		local = channel.HostedLocal
	}
	// Hosted addresses switched off are not offered to a channel; one whose support address is a hosted address
	// keeps it (its setup and forwarding name read it), also after a new slug renamed it, and mail to it is
	// still received.
	address := email.HostedLocalAddress(local, workspaceSlug, config.HostedMailDomain)
	if a.switchedOff(serverconfig.FeatureHostedMail) && !(channel != nil && email.UnderDomain(channel.SupportAddress, config.HostedMailDomain)) {
		address = ""
	}
	if address != "" {
		settings.HostedAddress = &address
	}
	if channel != nil && mailbox != nil {
		// A connection made before it had to be the channel's own address keeps sending; it is flagged.
		matches := email.SameMailbox(mailbox.Address, channel.SupportAddress)
		if mailbox.Provider == domain.MailboxMicrosoft {
			settings.Microsoft.AddressMatches = &matches
		} else {
			settings.Gmail.AddressMatches = &matches
		}
		// A mailbox sends by itself, and a broken connection never hands replies to SMTP.
		if mailbox.Status == domain.MailboxConnected && Outbound(a.config).MailboxEnabled(mailbox.Provider) {
			settings.SendingFrom = &mailbox.Address
		}
		return settings
	}
	if channel == nil || !smtp.Configured() {
		return settings
	}
	switch {
	case settings.FromPolicy == email.FromPolicyAny || channel.DomainVerifiedAt != nil:
		settings.SendingFrom = &channel.SupportAddress
	case config.MailFromDomain != "":
		platform := "reply+…@" + config.MailFromDomain
		settings.SendingFrom = &platform
	}
	return settings
}

// writeEmailSettings answers with the channel and the settings that come with it, including whether the
// workspace's sending is paused.
func (a *API) writeEmailSettings(response http.ResponseWriter, request *http.Request, channel *domain.EmailChannel) {
	workspace := principal(request).Workspace
	pause, err := a.store.GetEmailPause(request.Context(), workspace.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	var mailbox *domain.MailboxConnection
	var serverRefused bool
	if channel != nil {
		if mailbox, err = a.store.GetMailboxConnection(request.Context(), workspace.ID, channel.ChannelID); err != nil {
			writeInternalError(response, err)
			return
		}
		if mailbox != nil && mailbox.Provider == domain.MailboxMicrosoft {
			if serverRefused, err = a.store.MailboxWaitsForServer(request.Context(), workspace.ID, channel.ChannelID); err != nil {
				writeInternalError(response, err)
				return
			}
		}
	}
	writeJSON(response, http.StatusOK, a.emailSettings(workspace.Slug, channel, pause, mailbox, serverRefused))
}

// requestedEmailChannel reads the email channel a route acts on: the one its id names, or the default
// one on the routes without an id, which a workspace without email answers with nil. An id that names
// no email channel of the workspace is answered 404 here, and ok is false.
func (a *API) requestedEmailChannel(response http.ResponseWriter, request *http.Request) (*domain.EmailChannel, bool) {
	workspaceID := principal(request).Workspace.ID
	id := request.PathValue("id")
	if id == "" {
		channel, err := a.store.GetEmailChannel(request.Context(), workspaceID)
		if err != nil {
			writeInternalError(response, err)
			return nil, false
		}
		return channel, true
	}
	var channel *domain.EmailChannel
	var err error
	if uuidShape.MatchString(id) {
		channel, err = a.store.GetEmailChannelByID(request.Context(), workspaceID, id)
	}
	if err != nil {
		writeInternalError(response, err)
		return nil, false
	}
	if channel == nil {
		writeError(response, http.StatusNotFound, "channel_not_found", "No email channel of this workspace has that id.")
		return nil, false
	}
	return channel, true
}

// tokenDomainChecksPerHour bounds the DNS lookups agent tokens can trigger for one workspace; a person
// clicking Verify is not limited.
const tokenDomainChecksPerHour = 10

func (a *API) verifyEmailDomain(response http.ResponseWriter, request *http.Request) {
	if who := principal(request); who.IsToken() {
		if allowed, wait := a.domainChecks.allow(who.Workspace.ID, a.now()); !allowed {
			response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(response, http.StatusTooManyRequests, "rate_limited", "Agent tokens may check the email domain 10 times an hour. Try again later.")
			return
		}
	}
	if a.resend != nil {
		a.verifyProviderDomain(response, request)
		return
	}
	target, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	if target == nil {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	channel, err := a.store.VerifyEmailDomain(request.Context(), principal(request).Workspace.ID, target.ChannelID, a.config.LookupTXT, time.Now().UTC())
	if errors.Is(err, store.ErrDomainRecordMissing) {
		writeError(response, http.StatusConflict, "dns_record_not_found",
			"Add a TXT record named "+channel.DNSRecordName+" with the value "+channel.DNSRecordValue+", then try again. DNS changes can take a few minutes.")
		return
	}
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	a.writeEmailSettings(response, request, &channel)
}

// writeEmailStoreError reports a value another channel has as 409, a missing resource as 404 and
// anything else as a server failure.
func writeEmailStoreError(response http.ResponseWriter, err error) {
	if writeChannelConflict(response, err) {
		return
	}
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "not found") {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	writeInternalError(response, err)
}

func (a *API) getEmailChannel(response http.ResponseWriter, request *http.Request) {
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	a.writeEmailSettings(response, request, channel)
}

func (a *API) putEmailChannel(response http.ResponseWriter, request *http.Request) {
	var input struct {
		SupportAddress *string         `json:"support_address"`
		SenderName     json.RawMessage `json:"sender_name"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.SupportAddress != nil && !plainAddress(*input.SupportAddress)) ||
		(input.SupportAddress == nil && len(input.SenderName) == 0) {
		writeError(response, http.StatusBadRequest, "invalid_request", supportAddressRequired)
		return
	}
	senderName, ok := optionalSenderName(input.SenderName)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_request", senderNameInvalid)
		return
	}
	if senderName != nil && a.refuseBrand(response, *senderName) {
		return
	}
	workspaceID := principal(request).Workspace.ID
	if input.SupportAddress == nil {
		// Only the name changes: the address, its verification and the provider domain stay, whatever
		// address a stale page or a mistaken caller would have sent along.
		channel, err := a.store.SetEmailSenderNameAs(request.Context(), workspaceID, *senderName, channelActor(request))
		if errors.Is(err, store.ErrEmailChannelMissing) {
			writeError(response, http.StatusBadRequest, "invalid_request", supportAddressRequired)
			return
		}
		if err != nil {
			writeEmailStoreError(response, err)
			return
		}
		a.writeEmailSettings(response, request, &channel)
		return
	}
	if a.foreignHostedAddress(principal(request).Workspace.Slug, *input.SupportAddress) {
		writeError(response, http.StatusBadRequest, "invalid_request", "This address belongs to another workspace's forwarding address.")
		return
	}
	before, err := a.store.GetEmailChannel(request.Context(), workspaceID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	channel, err := a.store.UpsertEmailChannelAs(request.Context(), workspaceID, *input.SupportAddress, senderName, channelActor(request))
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	// Activation is connecting email or moving it to another address, not renaming the sender.
	if before == nil || before.SupportAddress != channel.SupportAddress {
		a.recordFunnel("email_channel_saved", workspaceID, nil)
	}
	a.writeEmailSettings(response, request, &channel)
}

const supportAddressRequired = "A plain support address such as support@example.com is required."

const senderNameInvalid = "sender_name must be 1 to 100 characters without line breaks or other control characters."

// optionalSenderName reads sender_name when the request has it: a trimmed, header-safe name. Without it
// the channel keeps its name (a new channel starts with the workspace name).
func optionalSenderName(raw json.RawMessage) (*string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	name, ok := domain.NormalizeHeaderName(value)
	return &name, ok
}

// foreignHostedAddress reports whether the address is on the hosted mail domain but not this workspace's own
// subdomain, so a workspace cannot send its customers' replies into another workspace.
func (a *API) foreignHostedAddress(workspaceSlug, address string) bool {
	hosted := strings.ToLower(a.config.Email.HostedMailDomain)
	host := strings.ToLower(email.Domain(address))
	// The hosted domain itself is not a forwarding address; sending as it still needs domain verification.
	return email.UnderDomain(address, hosted) && host != hosted && host != workspaceSlug+"."+hosted
}

// plainAddress accepts a bare addr-spec only: no display name, no comments, no line breaks.
func plainAddress(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Name == "" && parsed.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n<>")
}

func (a *API) deleteEmailChannel(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	channel, err := a.store.GetEmailChannel(request.Context(), workspaceID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if channel == nil {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	// Removing the channel removes its mailbox connection with it; the grant is read first so it can be
	// revoked at its provider afterwards.
	grant, err := a.store.MailboxGrant(request.Context(), workspaceID, channel.ChannelID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	deleted, err := a.store.DeleteEmailChannel(request.Context(), workspaceID)
	var held *channels.HeldError
	if errors.As(err, &held) {
		writeHeld(response, held)
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !deleted {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	a.revokeSealed(grant, workspaceID, channel.ChannelID)
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) rotateEmailSecret(response http.ResponseWriter, request *http.Request) {
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	if channel == nil {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	secret, err := a.store.RotateEmailSecretAs(request.Context(), principal(request).Workspace.ID, channel.ChannelID, time.Now().UTC(), channelActor(request))
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"inbound_secret": secret, "inbound_url": a.config.Origin + inboundEmailPath})
}

func (a *API) retryMessageDelivery(response http.ResponseWriter, request *http.Request) {
	message, err := a.store.RetryDelivery(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("messageId"))
	if errors.Is(err, store.ErrDeliveryNotFailed) {
		writeError(response, http.StatusConflict, "not_failed", "Only a failed delivery can be retried.")
		return
	}
	if errors.Is(err, store.ErrOutboundNotReady) {
		writeError(response, http.StatusConflict, "outbound_not_ready", strings.TrimPrefix(err.Error(), store.ErrOutboundNotReady.Error()+": "))
		return
	}
	if writePlanLimit(response, err) {
		return
	}
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, message)
}

// writeInternalError hides the cause from the client and keeps it in the server log.
func writeInternalError(response http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err, "request_id", response.Header().Get(requestIDHeader))
	writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed. Try again.")
}
