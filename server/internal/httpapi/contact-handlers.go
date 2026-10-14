package httpapi

import (
	"net/http"
	"strings"
)

func (a *API) createContact(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Name) == "" || !strings.Contains(input.Email, "@") {
		writeError(response, http.StatusBadRequest, "invalid_request", "Name and a valid email are required.")
		return
	}
	contact, err := a.store.CreateContact(request.Context(), principal(request).Workspace.ID, input.Name, input.Email)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, contact)
}

func (a *API) getContact(response http.ResponseWriter, request *http.Request) {
	contact, err := a.store.GetContact(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, contact)
}

func (a *API) listContacts(response http.ResponseWriter, request *http.Request) {
	contacts, err := a.store.ListContacts(request.Context(), principal(request).Workspace.ID, request.URL.Query().Get("search"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": contacts})
}
