package httpapi

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) markSpam(response http.ResponseWriter, request *http.Request) {
	var input struct {
		IDs  []string `json:"ids"`
		Spam *bool    `json:"spam"`
	}
	if decodeJSON(request, &input) != nil || input.Spam == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send ids (the conversations) and spam: true for Spam, false for Not spam.")
		return
	}
	if len(input.IDs) == 0 || len(input.IDs) > store.MaxBulkConversations {
		writeError(response, http.StatusBadRequest, "invalid_request", "Choose between 1 and 50 conversations.")
		return
	}
	seen := make(map[string]bool, len(input.IDs))
	for _, id := range input.IDs {
		if seen[id] || !contactIDPattern.MatchString(strings.ToLower(id)) {
			writeError(response, http.StatusBadRequest, "invalid_request", "List each conversation once, by its id.")
			return
		}
		seen[id] = true
	}
	who := principal(request)
	result, err := a.store.MarkSpam(request.Context(), who.Workspace.ID, input.IDs, *input.Spam, who.Agent.ID, tokenOf(request))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	slices.SortFunc(result.Senders, func(x, y store.SpamSender) int {
		return cmp.Or(strings.Compare(x.Name, y.Name), strings.Compare(x.ID, y.ID))
	})
	writeJSON(response, http.StatusOK, result)
}

// invalidStatusChange answers 400 and reports true for a status a change cannot set: spam has its own route.
func invalidStatusChange(response http.ResponseWriter, status *string) bool {
	if status == nil || slices.Contains(settableStatuses, *status) {
		return false
	}
	writeError(response, http.StatusBadRequest, "invalid_request", "status is one of open, pending, resolved, closed. "+
		"Mark spam with PUT /api/v1/conversations/spam.")
	return true
}
