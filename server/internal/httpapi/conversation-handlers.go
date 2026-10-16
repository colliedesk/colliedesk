package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) createConversation(response http.ResponseWriter, request *http.Request) {
	var input struct {
		ContactID string `json:"contact_id"`
		Subject   string `json:"subject"`
	}
	if err := decodeJSON(request, &input); err != nil || input.ContactID == "" || strings.TrimSpace(input.Subject) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Contact and subject are required.")
		return
	}
	item, err := a.store.CreateConversation(request.Context(), principal(request).Workspace.ID, input.ContactID, input.Subject)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}

func (a *API) getConversation(response http.ResponseWriter, request *http.Request) {
	item, err := a.store.GetConversation(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (a *API) listConversations(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	page := positiveInt(query.Get("page"), 1)
	pageSize := positiveInt(query.Get("page_size"), 50)
	if pageSize > 100 {
		pageSize = 100
	}
	identity := principal(request)
	items, err := a.store.ListConversations(request.Context(), identity.Workspace.ID, store.ConversationFilter{
		Status: query.Get("status"), Assignee: query.Get("assignee"), TagID: query.Get("tag"),
		Search: query.Get("search"), AgentID: identity.Agent.ID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (a *API) updateConversation(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Status     *string         `json:"status"`
		Priority   *string         `json:"priority"`
		AssigneeID json.RawMessage `json:"assignee_id"`
		TagIDs     *[]string       `json:"tag_ids"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Conversation changes are invalid.")
		return
	}
	var assignee **string
	if len(input.AssigneeID) > 0 {
		var value *string
		if string(input.AssigneeID) != "null" && json.Unmarshal(input.AssigneeID, &value) != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Assignee is invalid.")
			return
		}
		assignee = &value
	}
	identity := principal(request)
	item, err := a.store.UpdateConversation(request.Context(), identity.Workspace.ID, request.PathValue("id"), store.ConversationUpdate{
		Status: input.Status, Priority: input.Priority, AssigneeID: assignee, TagIDs: input.TagIDs, ActorID: identity.Agent.ID,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
