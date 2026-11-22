package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// A workspace has a primary help center, served at its own address, and up to domain.MaxHelpCenters
// minus one more, each served below the primary's address at /<handle> or on a custom domain of its
// own (docs/user/knowledge-base.md, "Several help centers").

// addressHelpCenters fills in the public address of each help center: its active custom domain while
// the workspace's help center is paid for, else the primary's address, below /<handle> for the others.
func (a *API) addressHelpCenters(response http.ResponseWriter, request *http.Request, centers []domain.HelpCenterSummary) bool {
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return false
	}
	paid := !awaitingPayment(principal(request).Access)
	for index := range centers {
		center := &centers[index]
		switch {
		case center.Primary:
			center.URL = home
		case center.Hostname != "" && paid:
			center.URL = a.domainAddress(center.Hostname)
		case center.Handle != nil:
			center.URL = home + "/" + *center.Handle
		}
	}
	return true
}

func (a *API) writeHelpCenterSummary(response http.ResponseWriter, request *http.Request, status int, center domain.HelpCenterSummary) {
	centers := []domain.HelpCenterSummary{center}
	if a.addressHelpCenters(response, request, centers) {
		writeJSON(response, status, centers[0])
	}
}

func (a *API) listHelpCenters(response http.ResponseWriter, request *http.Request) {
	centers, err := a.store.ListHelpCenters(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if a.addressHelpCenters(response, request, centers) {
		writeJSON(response, http.StatusOK, map[string]any{"help_centers": centers, "limit": domain.MaxHelpCenters})
	}
}

func (a *API) createHelpCenter(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name     *string `json:"name"`
		Handle   *string `json:"handle"`
		Title    string  `json:"title"`
		Locale   *string `json:"locale"`
		Enabled  bool    `json:"enabled"`
		ShowTags bool    `json:"show_tags"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Name == nil || input.Handle == nil || input.Locale == nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"Send name, handle (the path below the primary help center's address, such as hardware) and locale (such as en); title and enabled are optional.")
		return
	}
	fingerprint := []string{*input.Name, *input.Handle, input.Title, *input.Locale, strconv.FormatBool(input.Enabled), strconv.FormatBool(input.ShowTags)}
	a.createOnce(response, request, createHelpCenterOperation, fingerprint, func(ctx context.Context) (any, error) {
		created, err := a.store.CreateHelpCenter(ctx, principal(request).Workspace.ID, domain.NewHelpCenter{
			Name: *input.Name, Handle: *input.Handle, Title: input.Title, Locale: *input.Locale, Enabled: input.Enabled, ShowTags: input.ShowTags})
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		centers := []domain.HelpCenterSummary{created}
		if !a.addressHelpCenters(response, request, centers) {
			return nil, errAnswered
		}
		return centers[0], nil
	})
}

func (a *API) getHelpCenterByID(response http.ResponseWriter, request *http.Request) {
	center, err := a.store.GetHelpCenterByID(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.writeHelpCenterSummary(response, request, http.StatusOK, center)
}

func (a *API) patchHelpCenter(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name     *string `json:"name"`
		Handle   *string `json:"handle"`
		Title    *string `json:"title"`
		Locale   *string `json:"locale"`
		Enabled  *bool   `json:"enabled"`
		ShowTags *bool   `json:"show_tags"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the fields to change: name, handle, title, locale, enabled or show_tags.")
		return
	}
	changed, err := a.store.PatchHelpCenter(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), domain.HelpCenterPatch{
		Name: input.Name, Handle: input.Handle, Title: input.Title, Locale: input.Locale, Enabled: input.Enabled, ShowTags: input.ShowTags})
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.writeHelpCenterSummary(response, request, http.StatusOK, changed)
}

// deleteHelpCenter takes a help center other than the primary off line and releases its custom domain
// at once: the cache forgets the host, so it no longer serves it and another help center may claim it.
func (a *API) deleteHelpCenter(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	workspaceID, id := who.Workspace.ID, request.PathValue("id")
	// A help center on a custom domain leaves the public web with it, as DELETE /help-center/domain does.
	onDomain := func() (bool, error) {
		_, found, err := a.store.GetHelpCenterDomainIn(request.Context(), workspaceID, id)
		if errors.Is(err, domain.ErrHelpCenterNotFound) {
			return false, nil
		}
		return found, err
	}
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, onDomain) {
		return
	}
	released, err := a.store.DeleteHelpCenter(request.Context(), workspaceID, id)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	for _, d := range released {
		a.helpCenter.ForgetDomain(workspaceID, d.Hostname, d.Claim)
	}
	response.WriteHeader(http.StatusNoContent)
}
