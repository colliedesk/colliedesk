package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
)

const (
	// helpCenterDomainCheckTimeout bounds the DNS check a person or an agent asks for.
	helpCenterDomainCheckTimeout = 3 * time.Second
	// helpCenterLiveCheckTimeout bounds the DNS check the reverse proxy's certificate question waits for.
	helpCenterLiveCheckTimeout = 2 * time.Second
	// helpCenterRecheckTimeout bounds each domain's daily re-check.
	helpCenterRecheckTimeout = 5 * time.Second
	// Live checks of the certificate question: at most this many at once, one per host name per
	// back-off, and this many host names remembered.
	helpCenterLiveChecks       = 4
	helpCenterLiveCheckBackoff = 30 * time.Second
	helpCenterLiveCheckHosts   = 4096
)

// helpCenterDomainView is a workspace's custom domain as the API shows it (HelpCenterDomain).
type helpCenterDomainView struct {
	Hostname               *string                   `json:"hostname"`
	Status                 string                    `json:"status"`
	Records                []helpcenter.DomainRecord `json:"records"`
	CreatedAt              *time.Time                `json:"created_at"`
	VerifiedAt             *time.Time                `json:"verified_at"`
	CheckedAt              *time.Time                `json:"checked_at"`
	CheckError             *string                   `json:"check_error"`
	CertificateRequestedAt *time.Time                `json:"certificate_requested_at"`
	FailingSince           *time.Time                `json:"failing_since"`
	PendingReason          *string                   `json:"pending_reason"`
}

func noHelpCenterDomain() helpCenterDomainView {
	return helpCenterDomainView{Status: domain.HelpCenterDomainNone, Records: []helpcenter.DomainRecord{}}
}

func (a *API) helpCenterDomainView(d domain.HelpCenterDomain) helpCenterDomainView {
	host, created := d.Hostname, d.CreatedAt
	return helpCenterDomainView{Hostname: &host, Status: d.Status,
		Records:   helpcenter.Records(d.Hostname, d.Claim, d.Token, a.config.Origin, a.config.HelpCenterDomain),
		CreatedAt: &created, VerifiedAt: d.VerifiedAt, CheckedAt: d.CheckedAt, CheckError: d.CheckError, CertificateRequestedAt: d.CertificateRequestedAt,
		FailingSince: d.FailingSince, PendingReason: d.PendingReason}
}

func (a *API) resolvers() helpcenter.Resolvers {
	return helpcenter.Resolvers{CNAME: a.config.LookupCNAME, TXT: a.config.LookupTXT}
}

// lookUpDomain asks DNS once what proves a claim of host, within timeout.
func (a *API) lookUpDomain(ctx context.Context, host string, timeout time.Duration) helpcenter.DomainEvidence {
	lookup, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return helpcenter.LookupDomain(lookup, a.resolvers(), host, a.config.HelpCenterDomain)
}

// checkClaim reports whether the evidence proves the workspace's claim. What DNS answered stays in the
// server's log: the error the workspace sees only says what to set.
func checkClaim(evidence helpcenter.DomainEvidence, workspaceID string, claim domain.HelpCenterDomain) error {
	failure := evidence.Check(claim.Claim, claim.Token)
	if failure != nil {
		slog.Info("help center domain not proven", "workspace_id", workspaceID, "hostname", claim.Hostname, "dns", evidence.Detail())
	}
	return failure
}

// writeHelpCenterDomain answers with the workspace's custom domain as it is now.
func (a *API) writeHelpCenterDomain(response http.ResponseWriter, ctx context.Context, workspaceID string) {
	current, found, err := a.store.GetHelpCenterDomain(ctx, workspaceID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !found {
		writeJSON(response, http.StatusOK, noHelpCenterDomain())
		return
	}
	writeJSON(response, http.StatusOK, a.helpCenterDomainView(current))
}

func writeDomainTaken(response http.ResponseWriter) {
	writeError(response, http.StatusConflict, "domain_taken", "Another workspace's help center already serves on this host name.")
}

func (a *API) getHelpCenterDomain(response http.ResponseWriter, request *http.Request) {
	a.writeHelpCenterDomain(response, request.Context(), principal(request).Workspace.ID)
}

func (a *API) putHelpCenterDomain(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Hostname *string `json:"hostname"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Hostname == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send hostname, such as help.example.com.")
		return
	}
	host, err := helpcenter.CustomHost(*input.Hostname, a.config.Origin, a.config.HelpCenterDomain)
	if err != nil {
		reason := sentence(err) + "."
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "validation_failed", "message": reason + " Nothing was saved.", "fields": map[string]string{"hostname": reason}})})
		return
	}
	// Another workspace active on host does not refuse the claim: it waits, pending, and takes the host
	// over once DNS proves it while the holder's records no longer do (ActivateHelpCenterDomain).
	ctx, workspaceID := request.Context(), principal(request).Workspace.ID
	previous, _, err := a.store.GetHelpCenterDomain(ctx, workspaceID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	set, err := a.store.SetHelpCenterDomain(ctx, workspaceID, host, a.now())
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if set.Claim != previous.Claim {
		a.helpCenter.ForgetDomain(workspaceID, previous.Hostname, previous.Claim)
		a.helpCenter.ForgetDomain(workspaceID, set.Hostname, set.Claim)
	}
	a.writeHelpCenterDomain(response, ctx, workspaceID)
}

func (a *API) deleteHelpCenterDomain(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	removed, found, err := a.store.DeleteHelpCenterDomain(request.Context(), workspaceID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if found {
		a.helpCenter.ForgetDomain(workspaceID, removed.Hostname, removed.Claim)
	}
	writeJSON(response, http.StatusOK, noHelpCenterDomain())
}

// checkHelpCenterDomain looks the custom domain up in DNS now and records the result. Every check
// makes the server ask DNS, so one workspace's checks share the hourly bound of the email domain
// check, whether a person or an agent token asks.
func (a *API) checkHelpCenterDomain(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if allowed, wait := a.domainChecks.allow("help-center "+who.Workspace.ID, a.now()); !allowed {
		response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "The help center domain may be checked 10 times an hour. Try again later.")
		return
	}
	ctx := request.Context()
	current, found, err := a.store.GetHelpCenterDomain(ctx, who.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !found {
		writeError(response, http.StatusNotFound, "not_found", "No custom domain is set.")
		return
	}
	_, err = a.recordCheck(ctx, who.Workspace.ID, current, a.lookUpDomain(ctx, current.Hostname, helpCenterDomainCheckTimeout), false)
	switch {
	case errors.Is(err, domain.ErrHelpCenterDomainTaken):
		writeDomainTaken(response)
		return
	case errors.Is(err, domain.ErrHelpCenterDomainMissing):
		writeError(response, http.StatusConflict, "domain_changed", "The custom domain changed during the check; check it again.")
		return
	case err != nil:
		writeStoreError(response, err)
		return
	}
	a.writeHelpCenterDomain(response, ctx, who.Workspace.ID)
}

// recordCheck records what a DNS check of the workspace's claim current showed: active when the
// evidence proves it (taking the host over from a holder it no longer proves), else the failure, which
// releases an active domain after domain.HelpCenterDomainRelease. A change of status, here or of the
// holder taken over, is served from the next request.
func (a *API) recordCheck(ctx context.Context, workspaceID string, current domain.HelpCenterDomain, evidence helpcenter.DomainEvidence, certificate bool) (domain.HelpCenterDomain, error) {
	var after domain.HelpCenterDomain
	var released string
	var err error
	if failure := checkClaim(evidence, workspaceID, current); failure != nil {
		after, err = a.store.RecordHelpCenterDomainFailure(ctx, workspaceID, current.Claim, a.now(), failure.Error())
	} else {
		after, released, err = a.store.ActivateHelpCenterDomain(ctx, workspaceID, current.Claim, a.now(), evidence.Proof, certificate)
	}
	if err == nil && after.Status != current.Status {
		a.helpCenter.ForgetDomain(workspaceID, current.Hostname, current.Claim)
	}
	if err == nil && released != "" {
		a.helpCenter.ForgetDomain(released, current.Hostname, "")
	}
	return after, err
}

// certifyClaimHost reports whether the reverse proxy may get a certificate for a claim's CNAME target
// (a claim exists and its workspace's help center is open, so the target's redirect keeps working) and
// whether the claim exists.
func (a *API) certifyClaimHost(ctx context.Context, claim string) (open, claimed bool, err error) {
	workspaceID, found, err := a.store.HelpCenterClaim(ctx, claim)
	if err != nil || !found {
		return false, false, err
	}
	_, open, err = a.store.OpenHelpCenterWorkspace(ctx, workspaceID)
	return open, true, err
}

// certifyCustomHost reports whether the reverse proxy may get a certificate for host (it is an
// active custom domain whose help center is open, or the pending domain of a workspace whose DNS
// proves it within helpCenterLiveCheckTimeout, which then becomes active) and whether any workspace
// claims host, active or pending. Live checks are bounded by liveChecks, so a stream of questions
// cannot make the server flood DNS.
func (a *API) certifyCustomHost(ctx context.Context, host string) (open, claimed bool, err error) {
	if workspaceID, found, err := a.store.HelpCenterHost(ctx, host); err != nil || found {
		if err != nil {
			return false, false, err
		}
		_, open, err := a.store.OpenHelpCenterWorkspace(ctx, workspaceID)
		if err != nil || !open {
			return false, true, err
		}
		// The question is public: note the certificate at most once an hour per name in this process
		// (and in the database across processes), never on every question.
		if allowed, _ := a.certificateNotes.allow(host, a.now()); !allowed {
			return true, true, nil
		}
		return true, true, a.store.NoteHelpCenterCertificate(ctx, workspaceID, host, a.now())
	}
	pending, err := a.store.HelpCenterHostPending(ctx, host)
	if err != nil || !pending {
		return false, false, err
	}
	done, ok := a.liveChecks.Begin(host)
	if !ok {
		return false, true, nil
	}
	defer done()
	evidence := a.lookUpDomain(ctx, host, helpCenterLiveCheckTimeout)
	// However many workspaces claim the name, only the claims DNS proves are read, oldest first.
	claims, err := a.store.HelpCenterHostProvenClaims(ctx, host, evidence.Proof)
	if err != nil {
		return false, true, err
	}
	for _, workspaceID := range claims {
		claim, found, err := a.store.GetHelpCenterDomain(ctx, workspaceID)
		if err != nil {
			return false, true, err
		}
		if !found || claim.Hostname != host || claim.Status != domain.HelpCenterDomainPending {
			continue
		}
		_, open, err := a.store.OpenHelpCenterWorkspace(ctx, workspaceID)
		if err != nil {
			return false, true, err
		}
		after, err := a.recordCheck(ctx, workspaceID, claim, evidence, open)
		if errors.Is(err, domain.ErrHelpCenterDomainTaken) || errors.Is(err, domain.ErrHelpCenterDomainMissing) {
			continue
		}
		if err != nil {
			return false, true, err
		}
		if after.Status == domain.HelpCenterDomainActive {
			return open, true, nil
		}
	}
	return false, true, nil
}

// RecheckHelpCenterDomains looks up the DNS of every active custom domain and records what it shows.
// A failure warns on the domain (check_error) and keeps it serving, so a DNS provider's hiccup does not
// take a help center off line; a domain no check has proved for domain.HelpCenterDomainRelease goes
// back to pending (released). It reports how many were checked, failed and were released.
func (s *Server) RecheckHelpCenterDomains(ctx context.Context) (checked, failed, released int, err error) {
	a := s.api
	workspaces, err := a.store.ActiveHelpCenterDomainWorkspaces(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	var problems []error
	for _, workspaceID := range workspaces {
		current, found, err := a.store.GetHelpCenterDomain(ctx, workspaceID)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if !found || current.Status != domain.HelpCenterDomainActive {
			continue
		}
		after, err := a.recordCheck(ctx, workspaceID, current, a.lookUpDomain(ctx, current.Hostname, helpCenterRecheckTimeout), false)
		if errors.Is(err, domain.ErrHelpCenterDomainMissing) {
			continue
		}
		if err != nil {
			problems = append(problems, err)
			continue
		}
		checked++
		if after.CheckError != nil {
			failed++
		}
		if after.Status != domain.HelpCenterDomainActive {
			released++
		}
		if ctx.Err() != nil {
			break
		}
	}
	return checked, failed, released, errors.Join(problems...)
}
