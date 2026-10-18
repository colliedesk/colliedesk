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
	channel, err := a.store.GetEmailChannel(ctx, workspace.ID)
	if err != nil {
		writeInternalError(response, err)
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
	var provider resend.Domain
	var saved domain.EmailChannel
	var changed bool
	err = a.store.WithProviderDomainLock(ctx, workspace.ID, func(ctx context.Context) error {
		// Read again under the lock: another request may have created the domain, or the address changed.
		current, err := a.store.GetEmailChannel(ctx, workspace.ID)
		if err != nil {
			return err
		}
		if current == nil || current.SupportAddress != channel.SupportAddress {
			changed = true
			return nil
		}
		if provider, err = a.checkProviderDomain(ctx, workspace.ID, *current); err != nil {
			return err
		}
		saved, err = a.store.SaveProviderDomain(ctx, workspace.ID, current.SupportAddress, providerDomain(provider), time.Now().UTC())
		return err
	})
	switch {
	case changed:
		writeError(response, http.StatusConflict, "conflict", "The support address changed while the domain was being checked. Nothing was recorded; try again.")
		return
	case err != nil:
		writeProviderError(response, err)
		return
	case !provider.Verified():
		writeError(response, http.StatusConflict, "dns_record_not_verified", unverifiedMessage(saved.DNSRecords))
		return
	}
	a.writeEmailSettings(response, request, &saved)
}

// checkProviderDomain reads the stored provider domain, asking the provider to re-check it when it is not
// verified. A domain that is no longer at the provider (404) is created again; the id it replaces is queued
// for deletion.
func (a *API) checkProviderDomain(ctx context.Context, workspaceID string, channel domain.EmailChannel) (resend.Domain, error) {
	var provider resend.Domain
	if channel.ProviderDomainID != "" {
		var err error
		provider, err = a.resend.GetDomain(ctx, channel.ProviderDomainID)
		var apiErr *resend.APIError
		switch {
		case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
			channel.ProviderDomainID = ""
		case err != nil || provider.Verified():
			return provider, err
		}
	}
	if channel.ProviderDomainID == "" {
		created, err := a.resend.CreateDomain(ctx, channel.Domain)
		if err != nil {
			return resend.Domain{}, err
		}
		if _, err := a.store.SaveProviderDomain(ctx, workspaceID, channel.SupportAddress, providerDomain(created), time.Now().UTC()); err != nil {
			// The channel went away while the provider was answering: queue the new domain for deletion.
			if queueErr := a.store.QueueProviderDomainDeletion(context.WithoutCancel(ctx), workspaceID, created.ID); queueErr != nil {
				slog.Error("queue email provider domain that was not saved", "workspace", workspaceID, "domain", created.ID, "error", queueErr)
			}
			return resend.Domain{}, err
		}
		provider, channel.ProviderDomainID = created, created.ID
	}
	if provider.Verified() {
		return provider, nil
	}
	if err := a.resend.VerifyDomain(ctx, channel.ProviderDomainID); err != nil {
		return resend.Domain{}, err
	}
	return a.resend.GetDomain(ctx, channel.ProviderDomainID)
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
