package httpapi

import (
	"context"
	"errors"
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
}

func noHelpCenterDomain() helpCenterDomainView {
	return helpCenterDomainView{Status: domain.HelpCenterDomainNone, Records: []helpcenter.DomainRecord{}}
}

func (a *API) helpCenterDomainView(d domain.HelpCenterDomain) helpCenterDomainView {
	host, created := d.Hostname, d.CreatedAt
	return helpCenterDomainView{Hostname: &host, Status: d.Status,
		Records:   helpcenter.Records(d.Hostname, d.Token, a.config.Origin, a.config.HelpCenterDomain, d.WorkspaceSlug),
		CreatedAt: &created, VerifiedAt: d.VerifiedAt, CheckedAt: d.CheckedAt, CheckError: d.CheckError, CertificateRequestedAt: d.CertificateRequestedAt}
}

func (a *API) resolvers() helpcenter.Resolvers {
	return helpcenter.Resolvers{CNAME: a.config.LookupCNAME, TXT: a.config.LookupTXT}
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
	ctx, workspaceID := request.Context(), principal(request).Workspace.ID
	owner, taken, err := a.store.HelpCenterHost(ctx, host)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if taken && owner != workspaceID {
		writeDomainTaken(response)
		return
	}
	if _, err := a.store.SetHelpCenterDomain(ctx, workspaceID, host, a.now()); err != nil {
		writeStoreError(response, err)
		return
	}
	a.helpCenter.ForgetDomains()
	a.writeHelpCenterDomain(response, ctx, workspaceID)
}

func (a *API) deleteHelpCenterDomain(response http.ResponseWriter, request *http.Request) {
	if err := a.store.DeleteHelpCenterDomain(request.Context(), principal(request).Workspace.ID); err != nil {
		writeStoreError(response, err)
		return
	}
	a.helpCenter.ForgetDomains()
	writeJSON(response, http.StatusOK, noHelpCenterDomain())
}

// checkHelpCenterDomain looks the custom domain up in DNS now and records the result. Agent tokens
// share the hourly bound of the email domain check, per workspace; a person is not limited.
func (a *API) checkHelpCenterDomain(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if who.Scope != "" {
		if allowed, wait := a.domainChecks.allow("help-center "+who.Workspace.ID, a.now()); !allowed {
			response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeError(response, http.StatusTooManyRequests, "rate_limited", "Agent tokens may check the help center domain 10 times an hour. Try again later.")
			return
		}
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
	lookup, cancel := context.WithTimeout(ctx, helpCenterDomainCheckTimeout)
	failure := helpcenter.CheckDomain(lookup, a.resolvers(), current.Hostname, helpcenter.SlugHost(a.config.HelpCenterDomain, current.WorkspaceSlug), current.Token)
	cancel()
	_, err = a.store.RecordHelpCenterDomainCheck(ctx, who.Workspace.ID, current.Hostname, a.now(), failure, false)
	a.helpCenter.ForgetDomains()
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

// certifyCustomHost reports whether the reverse proxy may get a certificate for host: it is an
// active custom domain whose help center is open, or the pending domain of a workspace whose DNS
// proves it within helpCenterLiveCheckTimeout (it then becomes active). Live checks are bounded by
// liveChecks, so a stream of questions cannot make the server flood DNS.
func (a *API) certifyCustomHost(ctx context.Context, host string) (bool, error) {
	if workspaceID, found, err := a.store.HelpCenterHost(ctx, host); err != nil || found {
		if err != nil {
			return false, err
		}
		_, open, err := a.store.OpenHelpCenterWorkspace(ctx, workspaceID)
		if err != nil || !open {
			return false, err
		}
		return true, a.store.NoteHelpCenterCertificate(ctx, workspaceID, host, a.now())
	}
	claims, err := a.store.HelpCenterHostClaims(ctx, host)
	if err != nil || len(claims) == 0 {
		return false, err
	}
	done, ok := a.liveChecks.Begin(host)
	if !ok {
		return false, nil
	}
	defer done()
	lookup, cancel := context.WithTimeout(ctx, helpCenterLiveCheckTimeout)
	defer cancel()
	for _, workspaceID := range claims {
		claim, found, err := a.store.GetHelpCenterDomain(ctx, workspaceID)
		if err != nil {
			return false, err
		}
		if !found || claim.Hostname != host || claim.Status != domain.HelpCenterDomainPending {
			continue
		}
		failure := helpcenter.CheckDomain(lookup, a.resolvers(), host, helpcenter.SlugHost(a.config.HelpCenterDomain, claim.WorkspaceSlug), claim.Token)
		_, open, err := a.store.OpenHelpCenterWorkspace(ctx, workspaceID)
		if err != nil {
			return false, err
		}
		_, err = a.store.RecordHelpCenterDomainCheck(ctx, workspaceID, host, a.now(), failure, failure == nil && open)
		if failure != nil || errors.Is(err, domain.ErrHelpCenterDomainTaken) || errors.Is(err, domain.ErrHelpCenterDomainMissing) {
			continue
		}
		if err != nil {
			return false, err
		}
		a.helpCenter.ForgetDomains()
		return open, nil
	}
	return false, nil
}

// RecheckHelpCenterDomains looks up the DNS of every active custom domain and records what it shows.
// A failure warns on the domain (check_error) and never deactivates it: a DNS provider's hiccup must
// not take a help center off line. It reports how many were checked and how many failed.
func (s *Server) RecheckHelpCenterDomains(ctx context.Context) (checked, failed int, err error) {
	a := s.api
	workspaces, err := a.store.ActiveHelpCenterDomainWorkspaces(ctx)
	if err != nil {
		return 0, 0, err
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
		lookup, cancel := context.WithTimeout(ctx, helpCenterRecheckTimeout)
		failure := helpcenter.CheckDomain(lookup, a.resolvers(), current.Hostname, helpcenter.SlugHost(a.config.HelpCenterDomain, current.WorkspaceSlug), current.Token)
		cancel()
		if _, err := a.store.RecordHelpCenterDomainCheck(ctx, workspaceID, current.Hostname, a.now(), failure, false); err != nil && !errors.Is(err, domain.ErrHelpCenterDomainMissing) {
			problems = append(problems, err)
			continue
		}
		checked++
		if failure != nil {
			failed++
		}
		if ctx.Err() != nil {
			break
		}
	}
	return checked, failed, errors.Join(problems...)
}
