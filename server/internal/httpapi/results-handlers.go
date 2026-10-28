package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) getResults(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	page, err := a.store.ReadResults(request.Context(), who.Workspace.ID, results.Viewer{AgentID: who.Agent.ID, Admin: who.Agent.Role == "admin",
		CanSend: a.config.Email.SMTP.Configured() && a.config.Email.MailFromDomain != "", Now: a.now()})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) getResultsShareLink(response http.ResponseWriter, request *http.Request) {
	link, err := a.store.ResultsShareLink(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"link": link})
}

func (a *API) createResultsShareLink(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	token, link, err := a.store.CreateResultsShareLink(request.Context(), who.Workspace.ID, who.Agent.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	// The token is in the fragment: a browser never sends it to a server, a log or a Referer.
	writeJSON(response, http.StatusCreated, map[string]any{"link": link, "url": strings.TrimRight(a.config.Origin, "/") + "/r#" + token})
}

func (a *API) revokeResultsShareLink(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if err := a.store.RevokeResultsShareLink(request.Context(), who.Workspace.ID, who.Agent.ID); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"link": nil})
}

// getSharedResults answers the public share page. The same 404 answers a link that was turned off,
// replaced or never existed, and a locked workspace's, so a guess learns nothing.
func (a *API) getSharedResults(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("X-Robots-Tag", "noindex")
	if a.refuseBusyAddress(response, clientAddress(request, a.config.TrustedProxies)) {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Token == "" || len(input.Token) > 100 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the token of the share link.")
		return
	}
	shared, err := a.store.SharedResults(request.Context(), input.Token, a.now())
	if errors.Is(err, store.ErrShareLinkOff) {
		writeError(response, http.StatusNotFound, "share_link_off", "This link was turned off. Ask the team for a new one.")
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, shared)
}

func (a *API) setResultsEmail(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Weekly *bool `json:"weekly"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Weekly == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send weekly: true or false.")
		return
	}
	who := principal(request)
	if err := a.store.SetResultsEmail(request.Context(), who.Workspace.ID, who.Agent.ID, *input.Weekly); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"weekly": *input.Weekly})
}
