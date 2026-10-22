package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

// In-app imports (docs/user/import-freshdesk.md). The handlers only read and write import_jobs; the
// worker of `collie serve` talks to Freshdesk. No answer ever carries the API key.

func (a *API) listImports(response http.ResponseWriter, request *http.Request) {
	jobs, err := a.imports.List(request.Context(), principal(request).Workspace.ID, 100)
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": jobs})
}

func (a *API) previewImport(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Source  string             `json:"source"`
		Domain  string             `json:"domain"`
		APIKey  string             `json:"api_key"`
		Options importjobs.Options `json:"options"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Source != "freshdesk" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send source freshdesk, domain (your <account>.freshdesk.com address), api_key and optional options.")
		return
	}
	key := strings.TrimSpace(input.APIKey)
	if key == "" || len(key) > 200 {
		writeError(response, http.StatusBadRequest, "freshdesk_key_missing", "Paste a Freshdesk admin's API key (Profile settings > API key in Freshdesk).")
		return
	}
	who := principal(request)
	job, err := a.imports.Preview(request.Context(), who.Workspace.ID, who.Agent.ID, input.Domain, key, input.Options)
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, job)
}

func (a *API) getImport(response http.ResponseWriter, request *http.Request) {
	job, err := a.imports.Get(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, job)
}

func (a *API) startImport(response http.ResponseWriter, request *http.Request) {
	a.importTransition(response, request, a.imports.Start)
}

func (a *API) cancelImport(response http.ResponseWriter, request *http.Request) {
	a.importTransition(response, request, a.imports.Cancel)
}

func (a *API) importTransition(response http.ResponseWriter, request *http.Request, change func(ctx context.Context, workspaceID, id string) (importjobs.Job, error)) {
	job, err := change(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, job)
}

func (a *API) setImportKey(response http.ResponseWriter, request *http.Request) {
	var input struct {
		APIKey string `json:"api_key"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.APIKey) == "" || len(input.APIKey) > 200 {
		writeError(response, http.StatusBadRequest, "freshdesk_key_missing", "Paste a Freshdesk admin's API key (Profile settings > API key in Freshdesk).")
		return
	}
	job, err := a.imports.SetKey(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), strings.TrimSpace(input.APIKey))
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, job)
}

func (a *API) getImportReport(response http.ResponseWriter, request *http.Request) {
	format := request.URL.Query().Get("format")
	if format != "" && format != "json" && format != "markdown" {
		writeError(response, http.StatusBadRequest, "invalid_request", "format is json or markdown.")
		return
	}
	id := request.PathValue("id")
	report, markdown, ok, err := a.imports.Report(request.Context(), principal(request).Workspace.ID, id)
	switch {
	case err != nil:
		writeImportError(response, err)
	case !ok || (format == "markdown" && markdown == nil):
		writeError(response, http.StatusNotFound, "report_not_ready", "This import has no report yet; the interim report of the recent tickets has no Markdown.")
	case format == "markdown":
		response.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		response.Header().Set("Content-Disposition", `attachment; filename="freshdesk-import-`+id+`.md"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(*markdown))
	default:
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(report)
	}
}

func (a *API) undoImport(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	job, err := a.imports.Undo(request.Context(), who.Workspace.ID, who.Agent.ID, request.PathValue("id"))
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, job)
}

func (a *API) listImportPeople(response http.ResponseWriter, request *http.Request) {
	people, err := a.imports.People(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": people})
}

func (a *API) linkImportPerson(response http.ResponseWriter, request *http.Request) {
	var input struct {
		AgentID string `json:"agent_id"`
	}
	if err := decodeJSON(request, &input); err != nil || input.AgentID == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send agent_id, the Colliedesk agent to link.")
		return
	}
	person, err := a.imports.Link(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("externalId"), input.AgentID)
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, person)
}

func (a *API) unlinkImportPerson(response http.ResponseWriter, request *http.Request) {
	person, err := a.imports.Unlink(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("externalId"))
	if err != nil {
		writeImportError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, person)
}

func writeImportError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, importjobs.ErrNoPeople):
		writeError(response, http.StatusConflict, "import_state", err.Error()+"; wait for the preview.")
	case errors.Is(err, importjobs.ErrAgent):
		writeError(response, http.StatusBadRequest, "invalid_agent", "Link to an active agent of this workspace.")
	case errors.Is(err, importjobs.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No import with that id in this workspace.")
	case errors.Is(err, importjobs.ErrActive):
		writeError(response, http.StatusConflict, "import_running", "An import or undo is already queued, running or waiting in this workspace. Wait for it, or cancel it.")
	case errors.Is(err, importjobs.ErrState):
		writeError(response, http.StatusConflict, "import_state", "The import is not in a state that allows this (a preview starts within an hour; only an ended import can be undone; only a job waiting for its key takes one).")
	case errors.Is(err, importjobs.ErrOptions):
		writeError(response, http.StatusBadRequest, "invalid_request", err.Error()+".")
	case errors.Is(err, freshdesk.ErrOldHelpCenterHost):
		writeError(response, http.StatusBadRequest, "invalid_request", "options.old_help_center_domain: "+err.Error()+".")
	case errors.Is(err, freshdesk.ErrAccountHost):
		writeError(response, http.StatusBadRequest, "freshdesk_domain_invalid", err.Error()+", for example acme.freshdesk.com.")
	default:
		writeStoreError(response, err)
	}
}
