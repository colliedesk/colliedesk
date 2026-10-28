package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const kbMediaTrashRoute = "/api/v1/kb/media/trash"

// warnMissingKBMedia sets the media ids the saved article's text names that the library does not
// have; a failure to look only costs the warning.
func (a *API) warnMissingKBMedia(ctx context.Context, workspaceID string, article *domain.KBArticle) {
	missing, err := a.store.MissingKBMedia(ctx, workspaceID, domain.KBMediaIDs(article.BodyMD))
	if err != nil {
		log.Printf("knowledge base article %s: check its media: %v", article.ID, err)
		return
	}
	if len(missing) > 0 {
		article.MissingMedia = missing
	}
}

func writeKBMediaTrashError(response http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrKBMediaBatchNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The trash batch was not found.")
		return
	}
	writeKBMediaError(response, err)
}

func (a *API) listTrashedKBMedia(response http.ResponseWriter, request *http.Request) {
	limit := 0
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > domain.MaxKBMediaPage {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit is a whole number from 1 to 100.")
			return
		}
		limit = parsed
	}
	items, next, err := a.store.ListTrashedKBMedia(request.Context(), principal(request).Workspace.ID, request.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeKBMediaError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalString(next)})
}

func (a *API) trashKBMedia(response http.ResponseWriter, request *http.Request) {
	var input struct {
		IDs     []string `json:"ids"`
		BatchID string   `json:"batch_id"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send ids, a list of 1 to 100 media ids, and batch_id to add to an earlier batch.")
		return
	}
	who := principal(request)
	results, batch, err := a.store.TrashKBMedia(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), input.IDs, input.BatchID)
	if err != nil {
		writeKBMediaTrashError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"results": results, "batch_id": optionalString(batch)})
}

func (a *API) trashOneKBMedia(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	id := request.PathValue("id")
	results, batch, err := a.store.TrashKBMedia(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), []string{id}, "")
	if errors.As(err, new(*domain.InvalidKBError)) {
		writeKBMediaError(response, domain.ErrKBMediaNotFound)
		return
	}
	if err != nil {
		writeKBMediaTrashError(response, err)
		return
	}
	switch results[0].Status {
	case domain.KBMediaNotFound:
		writeKBMediaError(response, domain.ErrKBMediaNotFound)
		return
	case domain.KBMediaInUse:
		articles, err := a.store.KBMediaInUseArticles(request.Context(), who.Workspace.ID, results[0].ID)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		list := make([]map[string]any, len(articles))
		for i, article := range articles {
			list[i] = map[string]any{"id": article.ID, "number": article.Number, "title": article.Title, "status": article.Status}
		}
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": "media_in_use",
			"message": "Articles still show this; change them first, then move it to the trash.", "articles": list})})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"results": results, "batch_id": optionalString(batch)})
}

func (a *API) trashUnusedKBMedia(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Before      *time.Time `json:"before"`
		IncludeKept bool       `json:"include_kept"`
		BatchID     string     `json:"batch_id"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Before == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send before, the time the list was read (RFC 3339), and include_kept or batch_id when wanted.")
		return
	}
	who := principal(request)
	if !who.Allows(auth.PermKBMediaDelete) {
		a.proposeTrashOfUnusedKBMedia(response, request, who, *input.Before, input.IncludeKept)
		return
	}
	moved, more, batch, err := a.store.TrashUnusedKBMedia(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), input.IncludeKept, *input.Before, input.BatchID)
	if err != nil {
		writeKBMediaTrashError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"trashed": moved, "more": more, "batch_id": optionalString(batch)})
}

// proposeTrashOfUnusedKBMedia answers a token below send: a request a person approves always names
// the items it moves, so it files the trash of up to 100 unused items by id, never "all unused".
func (a *API) proposeTrashOfUnusedKBMedia(response http.ResponseWriter, request *http.Request, who auth.Principal, before time.Time, includeKept bool) {
	ids, err := a.store.UnusedKBMediaIDs(request.Context(), who.Workspace.ID, includeKept, before, domain.MaxKBMediaBatch)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if len(ids) == 0 {
		writeJSON(response, http.StatusOK, map[string]any{"trashed": 0, "more": false, "batch_id": nil})
		return
	}
	body, _ := json.Marshal(map[string]any{"ids": ids})
	route := Route{Method: http.MethodPost, Pattern: kbMediaTrashRoute}
	proposal := request.Clone(request.Context())
	proposal.Method, proposal.URL.Path, proposal.URL.RawPath, proposal.URL.RawQuery = http.MethodPost, kbMediaTrashRoute, "", ""
	a.fileAgentRequest(response, proposal, route, who, domain.AgentRequestAction, body, "")
}

func (a *API) restoreKBMedia(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	results, err := a.store.RestoreKBMedia(request.Context(), who.Workspace.ID, []string{request.PathValue("id")}, "")
	if err != nil || results[0].Status != domain.KBMediaRestored {
		if err != nil && !errors.As(err, new(*domain.InvalidKBError)) {
			writeKBMediaTrashError(response, err)
			return
		}
		writeError(response, http.StatusNotFound, "not_found", "No item with this id is in the library's trash.")
		return
	}
	media, err := a.store.GetKBMediaDetails(request.Context(), who.Workspace.ID, results[0].ID)
	if err != nil {
		writeKBMediaError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, media.KBMedia)
}

func (a *API) restoreKBMediaBatch(response http.ResponseWriter, request *http.Request) {
	var input struct {
		IDs     []string `json:"ids"`
		BatchID string   `json:"batch_id"`
	}
	if err := decodeJSON(request, &input); err != nil || (len(input.IDs) == 0 && input.BatchID == "") {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send ids, a list of 1 to 100 media ids, or the batch_id of a move to the trash.")
		return
	}
	results, err := a.store.RestoreKBMedia(request.Context(), principal(request).Workspace.ID, input.IDs, input.BatchID)
	if err != nil {
		writeKBMediaTrashError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"results": results})
}

func (a *API) purgeKBMedia(response http.ResponseWriter, request *http.Request) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send ids, a list of 1 to 100 ids of items in the library's trash.")
		return
	}
	results, err := a.store.PurgeKBMediaNow(request.Context(), principal(request).Workspace.ID, input.IDs)
	if err != nil {
		writeKBMediaTrashError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"results": results})
}

func writeKBMediaReplaceError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrKBMediaReplacementNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The replacement was not found.")
	case errors.Is(err, domain.ErrKBMediaKindMismatch):
		writeError(response, http.StatusBadRequest, "invalid_request", sentence(err)+".")
	case errors.Is(err, domain.ErrKBMediaGone):
		writeError(response, http.StatusConflict, "media_gone", sentence(err)+".")
	default:
		writeKBMediaError(response, err)
	}
}

func (a *API) replaceKBMedia(response http.ResponseWriter, request *http.Request) {
	// A replacement writes a new version of every live article that shows the item.
	who := principal(request)
	if a.askUnlessHeld(response, request, kbMediaReplaceRoute, who, auth.PermKBArticlesPublish, nil, func() (bool, error) {
		return a.store.KBMediaShownPublished(request.Context(), who.Workspace.ID, request.PathValue("id"))
	}) {
		return
	}
	var input struct {
		With string `json:"with_media_id"`
	}
	if err := decodeJSON(request, &input); err != nil || input.With == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send with_media_id, the item to put in its place.")
		return
	}
	replacement, err := a.store.ReplaceKBMedia(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), input.With)
	if err != nil {
		writeKBMediaReplaceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, replacement)
}

func (a *API) undoKBMediaReplacement(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.askUnlessHeld(response, request, kbMediaReplacementUndoRoute, who, auth.PermKBArticlesPublish, nil, func() (bool, error) {
		return a.store.KBReplacementTouchesPublished(request.Context(), who.Workspace.ID, request.PathValue("id"))
	}) {
		return
	}
	replacement, err := a.store.UndoKBMediaReplacement(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"))
	if err != nil {
		writeKBMediaReplaceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, replacement)
}
