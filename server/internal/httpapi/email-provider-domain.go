package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

func providerDomain(provider resend.Domain) store.ProviderDomain {
	records := make([]domain.DNSRecord, len(provider.Records))
	for i, record := range provider.Records {
		records[i] = domain.DNSRecord(record)
	}
	return store.ProviderDomain{ID: provider.ID, Records: records, Verified: provider.Verified()}
}

// ownsDomain reports whether the workspace's _colliedesk TXT record is published. It is the proof of
// control asked for before the provider is called, so a domain nobody proved never takes a slot in the
// shared provider account.
func (a *API) ownsDomain(ctx context.Context, channel domain.EmailChannel) (bool, error) {
	values, err := a.config.LookupTXT(ctx, channel.DNSRecordName)
	var dnsErr *net.DNSError
	if err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound) {
		return false, fmt.Errorf("look up %s: %w", channel.DNSRecordName, err)
	}
	return slices.Contains(values, channel.DNSRecordValue), nil
}

// verifyProviderDomain marks the domain verified only when the provider reports it verified, and only after
// the workspace proved control of the domain by TXT record. Then the domain is created at the provider once,
// and each check asks the provider to re-check and returns the records with their status. Nothing is
// recorded when the provider cannot be reached, so a failed call never looks like a verification.
func (a *API) verifyProviderDomain(response http.ResponseWriter, request *http.Request) {
	ctx, workspace := request.Context(), principal(request).Workspace
	channel, ok := a.requestedEmailChannel(response, request)
	if !ok {
		return
	}
	if channel == nil {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	proven, err := a.ownsDomain(ctx, *channel)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !proven {
		writeError(response, http.StatusConflict, "dns_record_not_found",
			"First prove you control "+channel.Domain+": add a TXT record named "+channel.DNSRecordName+" with the value "+channel.DNSRecordValue+", then try again. DNS changes can take a few minutes.")
		return
	}
	if allowed, wait := a.providerChecks.allow(workspace.ID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "The domain was checked a moment ago. Try again in a few seconds.")
		return
	}
	provider, current, err := a.checkProviderDomain(ctx, workspace.ID, *channel)
	if err == nil {
		var saved domain.EmailChannel
		if saved, err = a.store.SaveProviderDomain(ctx, workspace.ID, current.ChannelID, current.SupportAddress, providerDomain(provider), time.Now().UTC()); err == nil {
			if !provider.Verified() {
				writeError(response, http.StatusConflict, "dns_record_not_verified", unverifiedMessage(saved.DNSRecords))
				return
			}
			a.writeEmailSettings(response, request, &saved)
			return
		}
	}
	switch {
	case errors.Is(err, errChannelChanged), errors.Is(err, pgx.ErrNoRows):
		writeError(response, http.StatusConflict, "conflict", "The support address changed while the domain was being checked. Nothing was recorded; try again.")
	case errors.Is(err, store.ErrProviderDomainHeld):
		writeError(response, http.StatusConflict, "email_domain_unavailable", "Another workspace already has this domain verified with the email provider. Only one workspace can send as a given domain name; ask its owner or your operator.")
	default:
		writeProviderError(response, err)
	}
}

// errChannelChanged means the support address or the provider domain changed while a check was running.
var errChannelChanged = errors.New("email channel changed during the domain check")

// checkProviderDomain creates the provider domain when the channel has none, then asks the provider to
// re-check it and reads it. No transaction is open during any provider call. The new domain is stored with
// a compare-and-set on the id read before: if another request stored one first, the domain created here is
// queued for deletion and the stored one is used. A stored domain the provider no longer knows (404) is
// replaced the same way, and the id it replaces is queued for deletion.
func (a *API) checkProviderDomain(ctx context.Context, workspaceID string, channel domain.EmailChannel) (resend.Domain, domain.EmailChannel, error) {
	for range 2 {
		var provider resend.Domain
		stored := channel.ProviderDomainID
		if stored != "" {
			var err error
			provider, err = a.resend.GetDomain(ctx, stored)
			var apiErr *resend.APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				provider = resend.Domain{}
			} else if err != nil || provider.Verified() {
				return provider, channel, err
			}
		}
		if provider.ID == "" {
			created, err := a.resend.CreateDomain(ctx, channel.Domain)
			if err != nil {
				return resend.Domain{}, channel, err
			}
			attached, ok, err := a.store.AttachProviderDomain(ctx, workspaceID, channel.ChannelID, channel.SupportAddress, stored, providerDomain(created), time.Now().UTC())
			if err != nil || !ok {
				if queueErr := a.store.QueueProviderDomainDeletion(context.WithoutCancel(ctx), workspaceID, created.ID); queueErr != nil {
					slog.Error("queue email provider domain that was not stored", "workspace", workspaceID, "domain", created.ID, "error", queueErr)
				}
				if err != nil {
					return resend.Domain{}, channel, err
				}
				// Another request stored a domain first, or the address changed: read it again.
				fresh, err := a.store.GetEmailChannelByID(ctx, workspaceID, channel.ChannelID)
				if err != nil {
					return resend.Domain{}, channel, err
				}
				if fresh == nil || fresh.SupportAddress != channel.SupportAddress {
					return resend.Domain{}, channel, errChannelChanged
				}
				channel = *fresh
				continue
			}
			channel, provider = attached, created
		}
		if provider.Verified() {
			return provider, channel, nil
		}
		if err := a.resend.VerifyDomain(ctx, provider.ID); err != nil {
			return resend.Domain{}, channel, err
		}
		again, err := a.resend.GetDomain(ctx, provider.ID)
		return again, channel, err
	}
	return resend.Domain{}, channel, errChannelChanged
}

func unverifiedMessage(records []domain.DNSRecord) string {
	parts := make([]string, len(records))
	for i, record := range records {
		parts[i] = fmt.Sprintf("%s %s %s: %s", record.Record, record.Type, record.Name, record.Status)
	}
	return "Publish the DNS records listed in the email settings, then try again. DNS changes can take a few minutes. Status: " + strings.Join(parts, "; ") + "."
}

// writeProviderError answers the provider's validation refusal of the domain name as 409 with its reason,
// which is about what the tenant typed. Every other failure (credentials, permission, plan or rate limit,
// outage) is the operator's: the tenant gets a generic 502 and the cause goes to the log, never the key.
func writeProviderError(response http.ResponseWriter, err error) {
	var apiErr *resend.APIError
	if errors.As(err, &apiErr) && apiErr.Rejected() {
		writeError(response, http.StatusConflict, "email_domain_unavailable", "The email provider did not accept this domain: "+apiErr.Message)
		return
	}
	if errors.As(err, &apiErr) {
		slog.Error("email provider refused the request", "status", apiErr.Status, "name", apiErr.Name, "message", apiErr.Message)
	} else {
		slog.Error("email provider request failed", "error", err)
	}
	writeError(response, http.StatusBadGateway, "email_provider_unavailable", "The email provider could not be reached. Nothing was changed; try again.")
}
