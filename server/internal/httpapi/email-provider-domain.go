package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/resend"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// createProviderDomain adds the channel's domain to the provider once and keeps its id and records.
func (a *API) createProviderDomain(ctx context.Context, workspaceID string, channel domain.EmailChannel) (domain.EmailChannel, error) {
	created, err := a.resend.CreateDomain(ctx, channel.Domain)
	if err != nil {
		return channel, err
	}
	return a.store.SaveProviderDomain(ctx, workspaceID, channel.SupportAddress, providerDomain(created), time.Now().UTC())
}

func providerDomain(provider resend.Domain) store.ProviderDomain {
	records := make([]domain.DNSRecord, len(provider.Records))
	for i, record := range provider.Records {
		records[i] = domain.DNSRecord(record)
	}
	return store.ProviderDomain{ID: provider.ID, Records: records, Verified: provider.Verified()}
}

// verifyProviderDomain marks the domain verified only when the provider reports it verified. A domain that
// is not yet gets one check request, and the answer returns the records with their status. Nothing is
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
	provider, err := a.checkProviderDomain(ctx, workspace.ID, *channel)
	if err != nil {
		writeProviderError(response, err)
		return
	}
	saved, err := a.store.SaveProviderDomain(ctx, workspace.ID, channel.SupportAddress, providerDomain(provider), time.Now().UTC())
	if err != nil {
		writeEmailStoreError(response, err)
		return
	}
	if !provider.Verified() {
		writeError(response, http.StatusConflict, "dns_record_not_verified", unverifiedMessage(saved.DNSRecords))
		return
	}
	a.writeEmailSettings(response, request, &saved)
}

// checkProviderDomain creates the domain when it does not exist yet, then reads its status, asking the
// provider to re-check first when it is not verified.
func (a *API) checkProviderDomain(ctx context.Context, workspaceID string, channel domain.EmailChannel) (resend.Domain, error) {
	if channel.ProviderDomainID == "" {
		created, err := a.createProviderDomain(ctx, workspaceID, channel)
		if err != nil {
			return resend.Domain{}, err
		}
		channel = created
	}
	provider, err := a.resend.GetDomain(ctx, channel.ProviderDomainID)
	if err != nil || provider.Verified() {
		return provider, err
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

// writeProviderError answers a provider refusal of this domain as 409 with the provider's own reason, and
// anything else (unreachable, bad credentials, server error) as 502 with the cause kept in the log.
func writeProviderError(response http.ResponseWriter, err error) {
	var apiErr *resend.APIError
	if errors.As(err, &apiErr) && apiErr.Rejected() {
		writeError(response, http.StatusConflict, "email_domain_unavailable", "The email provider did not accept this domain: "+apiErr.Message)
		return
	}
	slog.Error("email provider request failed", "error", err)
	writeError(response, http.StatusBadGateway, "email_provider_unavailable", "The email provider could not be reached. Nothing was changed; try again.")
}
