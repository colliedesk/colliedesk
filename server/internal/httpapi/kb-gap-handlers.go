package httpapi

import (
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// listKBGaps lists what readers asked for and did not find, highest rank first.
func (a *API) listKBGaps(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := domain.KBGapFilter{Status: query.Get("status"), Kind: query.Get("kind")}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit must be a whole number from 1 to "+strconv.Itoa(domain.MaxKBGapsListed)+".")
			return
		}
		filter.Limit = limit
	}
	gaps, err := a.store.ListKBGaps(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": gaps})
}

// getKBGap answers one gap with its newest signals.
func (a *API) getKBGap(response http.ResponseWriter, request *http.Request) {
	gap, err := a.store.GetKBGap(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, gap)
}

// updateKBGap links the gap to an article, dismisses it or opens it again.
func (a *API) updateKBGap(response http.ResponseWriter, request *http.Request) {
	var change domain.KBGapChange
	if err := decodeJSON(request, &change); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send status (open, fixed or dismissed), fixed_by_article_id or both.")
		return
	}
	who := principal(request)
	gap, err := a.store.UpdateKBGap(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), change)
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, gap)
}
