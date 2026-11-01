package httpapi

import (
	"errors"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// getSetupProgress tells where the workspace stands on getting started.
func (a *API) getSetupProgress(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	progress, err := a.store.SetupProgress(request.Context(), who.Workspace.ID, a.now())
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.setupLinks(&progress, who.Workspace.Slug)
	writeJSON(response, http.StatusOK, progress)
}

// setSetupGuideHidden hides the setup guide for the caller, or shows it again.
func (a *API) setSetupGuideHidden(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Hidden *bool `json:"hidden"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Hidden == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send hidden: true or false.")
		return
	}
	who := principal(request)
	if err := a.store.SetSetupGuideHidden(request.Context(), who.Workspace.ID, who.Agent.ID, *input.Hidden); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"hidden": *input.Hidden})
}

// setupLinks uses this server's address configuration, including self-hosted help centers.
func (a *API) setupLinks(progress *store.SetupProgress, slug string) {
	targets := map[string]string{"email": "/settings/channels/type/email", "import": "/settings/import-freshdesk", "invite": "/settings", "first_reply": "/",
		"preview": helpcenter.URL(a.config.Origin, a.config.HelpCenterDomain, slug), "custom_domain": "/settings#help-center-domain-heading",
		"plan": "/settings/billing", "cname": "/settings#help-center-domain-heading", "search_console": "/settings#help-center-search-console-heading"}
	for i := range progress.Steps {
		step := &progress.Steps[i]
		step.URL = targets[step.Key]
		if step.Key == "preview" && step.Detail != nil {
			if *step.Detail == "import" {
				step.URL = targets["import"]
			} else {
				step.URL = "/settings#help-center-heading"
			}
		}
		done := map[string]string{"email": "working", "import": "done", "invite": "joined", "first_reply": "sent"}[step.Key]
		if done == "" {
			done = "done"
		}
		if progress.NextURL == nil && step.State != done && step.State != "skipped" {
			next := step.URL
			progress.NextURL = &next
		}
	}
}

func (a *API) setSetupMoveStep(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Step  string `json:"step"`
		State string `json:"state"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send step and state. Nothing changed.")
		return
	}
	who := principal(request)
	err := a.store.SetSetupMoveStep(request.Context(), who.Workspace.ID, input.Step, input.State, a.now())
	if errors.Is(err, store.ErrSetupMove) {
		writeError(response, http.StatusUnprocessableEntity, "invalid_move_step", err.Error()+". Read setup progress for the next action. Nothing changed.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.getSetupProgress(response, request)
}
