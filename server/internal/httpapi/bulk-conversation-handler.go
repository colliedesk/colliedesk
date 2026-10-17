package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) bulkUpdateConversations(response http.ResponseWriter, request *http.Request) {
	var input struct {
		IDs     []string `json:"ids"`
		Changes *struct {
			Status       *string         `json:"status"`
			AssigneeID   json.RawMessage `json:"assignee_id"`
			AddTagIDs    []string        `json:"add_tag_ids"`
			RemoveTagIDs []string        `json:"remove_tag_ids"`
		} `json:"changes"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Changes == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Conversation changes are invalid.")
		return
	}
	changes := input.Changes
	seen := make(map[string]bool, len(input.IDs))
	for _, id := range input.IDs {
		if seen[id] {
			writeError(response, http.StatusBadRequest, "invalid_request", "Each conversation can be listed once.")
			return
		}
		seen[id] = true
	}
	if len(input.IDs) == 0 || len(input.IDs) > store.MaxBulkConversations {
		writeError(response, http.StatusBadRequest, "invalid_request", "Choose between 1 and 50 conversations.")
		return
	}
	if changes.Status == nil && len(changes.AssigneeID) == 0 && len(changes.AddTagIDs) == 0 && len(changes.RemoveTagIDs) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Name at least one change.")
		return
	}
	removing := make(map[string]bool, len(changes.RemoveTagIDs))
	for _, tagID := range changes.RemoveTagIDs {
		removing[tagID] = true
	}
	for _, tagID := range changes.AddTagIDs {
		if removing[tagID] {
			writeError(response, http.StatusBadRequest, "invalid_request", "A tag cannot be added and removed in one update.")
			return
		}
	}
	update := store.BulkConversationUpdate{Status: changes.Status, AddTagIDs: changes.AddTagIDs, RemoveTagIDs: changes.RemoveTagIDs}
	if len(changes.AssigneeID) > 0 {
		var value *string
		if string(changes.AssigneeID) != "null" && json.Unmarshal(changes.AssigneeID, &value) != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Assignee is invalid.")
			return
		}
		update.AssigneeID = &value
	}
	updated, err := a.store.BulkUpdateConversations(request.Context(), principal(request).Workspace.ID, input.IDs, update)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]int{"updated": updated})
}
