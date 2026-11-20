package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// writeKBTemplateError maps the refusals of template writes; anything else is a store failure.
func writeKBTemplateError(response http.ResponseWriter, err error) {
	var invalid *domain.InvalidSavedReplyError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.Is(err, domain.ErrKBTemplateDuplicateName):
		writeError(response, http.StatusConflict, "duplicate_name", domain.ErrKBTemplateDuplicateName.Error()+". Names are compared ignoring case.")
	case errors.Is(err, domain.ErrKBTemplateChanged):
		writeError(response, http.StatusConflict, "changed_elsewhere", "This template was changed by someone else. Reload to see their version, then edit again.")
	case errors.Is(err, domain.ErrKBTemplateLimit):
		writeError(response, http.StatusConflict, "kb_template_limit", domain.ErrKBTemplateLimit.Error()+".")
	case errors.Is(err, domain.ErrKBTemplateInTrash):
		writeError(response, http.StatusConflict, "template_in_trash", "The "+domain.ErrKBTemplateInTrash.Error()+".")
	case errors.Is(err, domain.ErrKBTemplateNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The template was not found. GET /api/v1/kb/templates lists them.")
	case errors.Is(err, domain.ErrKBTemplateVersionNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The template has no version with that number.")
	default:
		writeStoreError(response, err)
	}
}

func (a *API) listKBTemplates(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListKBTemplates(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) getKBTemplate(response http.ResponseWriter, request *http.Request) {
	template, err := a.store.GetKBTemplate(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBTemplateError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, template)
}

func (a *API) createKBTemplate(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A template needs a name and a Markdown body; description is optional.")
		return
	}
	who := principal(request)
	a.createOnce(response, request, createKBTemplateOperation, []string{input.Name, input.Description, input.Body}, func(ctx context.Context) (any, error) {
		template, err := a.store.CreateKBTemplate(ctx, who.Workspace.ID, savedReplyActor(ctx, who), input.Name, input.Description, input.Body)
		if err != nil {
			writeKBTemplateError(response, err)
			return nil, errAnswered
		}
		return template, nil
	})
}

func (a *API) updateKBTemplate(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name        *string    `json:"name"`
		Description *string    `json:"description"`
		Body        *string    `json:"body"`
		UpdatedAt   *time.Time `json:"updated_at"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil ||
		(input.Name == nil && input.Description == nil && input.Body == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new name, description, body or several.")
		return
	}
	who := principal(request)
	template, err := a.store.UpdateKBTemplate(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), *input.UpdatedAt,
		domain.KBTemplateChange{Name: input.Name, Description: input.Description, Body: input.Body})
	if err != nil {
		writeKBTemplateError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, template)
}

func (a *API) deleteKBTemplate(response http.ResponseWriter, request *http.Request) {
	seen, ok := seenAt(response, request)
	if !ok {
		return
	}
	who := principal(request)
	if err := a.store.DeleteKBTemplate(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), seen); err != nil {
		writeKBTemplateError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) listTrashedKBTemplates(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	items, next, err := a.store.ListTrashedKBTemplates(request.Context(), principal(request).Workspace.ID, limit, request.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrBadCursor) {
		writeError(response, http.StatusBadRequest, "invalid_request", "cursor must be the next_cursor of the previous page.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalString(next)})
}

func (a *API) restoreKBTemplate(response http.ResponseWriter, request *http.Request) {
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	who := principal(request)
	// A repeated press (same Idempotency-Key) answers the first undelete again.
	id := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, restoreKBTemplateOperation, []string{id, seen.UTC().Format(time.RFC3339Nano)}, func(ctx context.Context) (any, error) {
		template, err := a.store.UndeleteKBTemplate(ctx, who.Workspace.ID, savedReplyActor(ctx, who), id, seen)
		if errors.Is(err, domain.ErrKBTemplateNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "No template with this id is in the trash.")
			return nil, errAnswered
		}
		if err != nil {
			writeKBTemplateError(response, err)
			return nil, errAnswered
		}
		return template, nil
	})
}

func (a *API) listKBTemplateVersions(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	before := 0
	if raw := request.URL.Query().Get("before_version"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(response, http.StatusBadRequest, "invalid_request", "before_version must be a version number, 1 or more.")
			return
		}
		before = parsed
	}
	items, next, err := a.store.KBTemplateVersions(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), limit, before)
	if err != nil {
		writeKBTemplateError(response, err)
		return
	}
	var nextVersion *int
	if next > 0 {
		nextVersion = &next
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_before_version": nextVersion})
}

func (a *API) restoreKBTemplateVersion(response http.ResponseWriter, request *http.Request) {
	version, err := strconv.Atoi(request.PathValue("version"))
	if err != nil || version < 1 {
		writeError(response, http.StatusNotFound, "not_found", "The template has no version with that number.")
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	who := principal(request)
	// A repeated press (same Idempotency-Key) answers the first restore again and adds no version.
	id := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, restoreKBTemplateVersionOperation, []string{id, strconv.Itoa(version), seen.UTC().Format(time.RFC3339Nano)},
		func(ctx context.Context) (any, error) {
			template, err := a.store.RestoreKBTemplateVersion(ctx, who.Workspace.ID, savedReplyActor(ctx, who), id, version, seen)
			if err != nil {
				writeKBTemplateError(response, err)
				return nil, errAnswered
			}
			return template, nil
		})
}
