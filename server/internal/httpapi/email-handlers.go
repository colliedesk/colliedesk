package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
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
	// DomainVerification is "provider" when the email provider lists the DNS records to publish, else "txt".
	DomainVerification string `json:"domain_verification"`
}

func (a *API) emailSettings(workspaceSlug string, channel *domain.EmailChannel) emailChannelSettings {
	config := a.config.Email
	smtp := config.SMTP
	settings := emailChannelSettings{Channel: channel, InboundURL: a.config.Origin + inboundEmailPath, FromPolicy: config.FromPolicy, DomainVerification: "txt",
		SMTP: smtpStatus{Configured: smtp.Configured(), Host: smtp.Host, Port: smtp.Port, StartTLS: smtp.StartTLS, Authenticated: smtp.Username != ""}}
	if a.resend != nil {
		settings.DomainVerification = "provider"
	}
	if settings.FromPolicy == "" {
		settings.FromPolicy = email.FromPolicyVerified
	}
	if address := email.HostedAddress(workspaceSlug, config.HostedMailDomain); address != "" {
		settings.HostedAddress = &address
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

// tokenDomainChecksPerHour bounds the DNS lookups agent tokens can trigger for one workspace; a person
// clicking Verify is not limited.
const tokenDomainChecksPerHour = 10

func (a *API) verifyEmailDomain(response http.ResponseWriter, request *http.Request) {
	if who := principal(request); who.Scope != "" {
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
	channel, err := a.store.VerifyEmailDomain(request.Context(), principal(request).Workspace.ID, a.config.LookupTXT, time.Now().UTC())
	if errors.Is(err, store.ErrDomainRecordMissing) {
		writeError(response, http.StatusConflict, "dns_record_not_found",
			"Add a TXT record named "+channel.DNSRecordName+" with the value "+channel.DNSRecordValue+", then try again. DNS changes can take a few minutes.")
		return
	}
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, a.emailSettings(principal(request).Workspace.Slug, &channel))
}

// writeEmailStoreError reports a missing resource as 404 and anything else as a server failure.
func writeEmailStoreError(response http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "not found") {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	writeInternalError(response, err)
}

func (a *API) getEmailChannel(response http.ResponseWriter, request *http.Request) {
	channel, err := a.store.GetEmailChannel(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, a.emailSettings(principal(request).Workspace.Slug, channel))
}

func (a *API) putEmailChannel(response http.ResponseWriter, request *http.Request) {
	var input struct {
		SupportAddress string `json:"support_address"`
	}
	if err := decodeJSON(request, &input); err != nil || !plainAddress(input.SupportAddress) {
		writeError(response, http.StatusBadRequest, "invalid_request", "A plain support address such as support@example.com is required.")
		return
	}
	if a.foreignHostedAddress(principal(request).Workspace.Slug, input.SupportAddress) {
		writeError(response, http.StatusBadRequest, "invalid_request", "This address belongs to another workspace's forwarding address.")
		return
	}
	channel, err := a.store.UpsertEmailChannel(request.Context(), principal(request).Workspace.ID, input.SupportAddress)
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	if a.resend != nil && channel.ProviderDomainID == "" {
		// The address is saved either way; verification retries creating the domain.
		if created, err := a.createProviderDomain(request.Context(), principal(request).Workspace.ID, channel); err != nil {
			slog.Warn("create email provider domain", "workspace", principal(request).Workspace.ID, "error", err)
		} else {
			channel = created
		}
	}
	writeJSON(response, http.StatusOK, a.emailSettings(principal(request).Workspace.Slug, &channel))
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
	deleted, err := a.store.DeleteEmailChannel(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !deleted {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) rotateEmailSecret(response http.ResponseWriter, request *http.Request) {
	secret, err := a.store.RotateEmailSecret(request.Context(), principal(request).Workspace.ID, time.Now().UTC())
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
	slog.Error("request failed", "error", err)
	writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed. Try again.")
}
