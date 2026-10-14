package httpapi

import (
	"net/http"
	"strings"
)

func (a *API) createTag(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Tag name is required.")
		return
	}
	if input.Color == "" {
		input.Color = "#64748b"
	}
	tag, err := a.store.CreateTag(request.Context(), principal(request).Workspace.ID, strings.TrimSpace(input.Name), input.Color)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, tag)
}

func (a *API) updateTag(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Name) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Tag name is required.")
		return
	}
	tag, err := a.store.UpdateTag(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), strings.TrimSpace(input.Name))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, tag)
}

func (a *API) deleteTag(response http.ResponseWriter, request *http.Request) {
	ok, err := a.store.DeleteTag(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The tag was not found.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) addConversationTag(response http.ResponseWriter, request *http.Request) {
	item, err := a.store.AddConversationTag(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("tagId"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (a *API) removeConversationTag(response http.ResponseWriter, request *http.Request) {
	item, err := a.store.RemoveConversationTag(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("tagId"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}
