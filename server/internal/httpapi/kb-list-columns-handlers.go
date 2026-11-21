package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// setKBListColumns stores the caller's columns of the knowledge base article list; null is the default.
func (a *API) setKBListColumns(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Columns json.RawMessage `json:"columns"`
	}
	choose := "Send columns: a list of " + strings.Join(domain.KBListColumnNames, ", ") + " in the order to show them, or null for the default."
	if err := decodeJSON(request, &input); err != nil || len(input.Columns) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", choose)
		return
	}
	var columns []string
	if string(input.Columns) != "null" {
		if err := json.Unmarshal(input.Columns, &columns); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", choose)
			return
		}
		if columns == nil {
			columns = []string{}
		}
	}
	if err := domain.CheckKBListColumns(columns); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", err.Error()+".")
		return
	}
	who := principal(request)
	if err := a.store.SetKBListColumns(request.Context(), who.Workspace.ID, who.Agent.ID, columns); err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, domain.EffectiveKBListColumns(columns))
}
