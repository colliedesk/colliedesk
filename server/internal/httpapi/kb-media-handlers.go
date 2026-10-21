package httpapi

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// maxKBThumbnailBytes bounds what a thumbnail download reads; a 320-pixel JPEG or PNG is far smaller.
const maxKBThumbnailBytes = 1 << 20

func writeKBMediaError(response http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrKBMediaNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The media was not found.")
		return
	}
	writeKBError(response, err)
}

func (a *API) listKBMedia(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	filter := domain.KBMediaFilter{Kind: query.Get("kind"), Query: query.Get("q"), Usage: query.Get("usage"), Sort: query.Get("sort"), Cursor: query.Get("cursor")}
	if len([]rune(filter.Query)) > 200 {
		writeError(response, http.StatusBadRequest, "invalid_request", "q holds at most 200 characters.")
		return
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > domain.MaxKBMediaPage {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit is a whole number from 1 to 100.")
			return
		}
		filter.Limit = limit
	}
	items, next, summary, err := a.store.ListKBMedia(request.Context(), principal(request).Workspace.ID, filter)
	if err != nil {
		writeKBMediaError(response, err)
		return
	}
	page := map[string]any{"items": items, "next_cursor": optionalString(next)}
	if summary != nil {
		page["summary"] = summary
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) getKBMediaDetails(response http.ResponseWriter, request *http.Request) {
	details, err := a.store.GetKBMediaDetails(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBMediaError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, details)
}

func (a *API) updateKBMedia(response http.ResponseWriter, request *http.Request) {
	var input struct {
		UpdatedAt *time.Time `json:"updated_at"`
		Name      *string    `json:"name"`
		Alt       *string    `json:"alt"`
		Kept      *bool      `json:"kept"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil || (input.Name == nil && input.Alt == nil && input.Kept == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new name, alt or kept.")
		return
	}
	who := principal(request)
	media, err := a.store.UpdateKBMedia(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"),
		domain.KBMediaPatch{UpdatedAt: *input.UpdatedAt, Name: input.Name, Alt: input.Alt, Kept: input.Kept})
	if err != nil {
		writeKBMediaError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, media)
}

func (a *API) getKBMediaThumbnail(response http.ResponseWriter, request *http.Request) {
	notFound := func() { writeError(response, http.StatusNotFound, "not_found", "The media has no thumbnail.") }
	if a.config.Attachments == nil {
		notFound()
		return
	}
	workspaceID := principal(request).Workspace.ID
	media, err := a.store.GetKBMedia(request.Context(), workspaceID, request.PathValue("id"))
	// Storage keys start with the owning workspace id; a row pointing at another workspace's bytes is never served.
	if err != nil || media.ThumbKey == "" || !strings.HasPrefix(media.ThumbKey, workspaceID+"/") {
		if err != nil && !errors.Is(err, domain.ErrKBMediaNotFound) {
			writeStoreError(response, err)
			return
		}
		notFound()
		return
	}
	body, err := a.config.Attachments.Open(request.Context(), media.ThumbKey)
	if err != nil {
		log.Printf("knowledge base thumbnail %s: open stored bytes: %v", media.ID, err)
		notFound()
		return
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxKBThumbnailBytes))
	if err != nil {
		log.Printf("knowledge base thumbnail %s: read: %v", media.ID, err)
		notFound()
		return
	}
	// The server wrote it as a JPEG or a PNG; the bytes say which.
	contentType := "image/jpeg"
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		contentType = "image/png"
	}
	header := response.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(data)))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// thumbnail_url carries the thumbnail's hash (?v=), so a new thumbnail is a new URL.
	header.Set("Cache-Control", "private, max-age=31536000, immutable")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(data); err != nil {
		log.Printf("knowledge base thumbnail %s: write: %v", media.ID, err)
	}
}
