package httpapi

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// downloadAttachment streams stored bytes as a download. Files come from
// customers and other systems, so they are never rendered inline on the app
// origin: the response is always application/octet-stream + attachment.
func (a *API) downloadAttachment(response http.ResponseWriter, request *http.Request) {
	if a.config.Attachments == nil {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	workspaceID := principal(request).Workspace.ID
	item, key, err := a.store.GetAttachment(request.Context(), workspaceID, request.PathValue("id"))
	// Storage keys start with the owning workspace id; a row pointing at
	// another workspace's bytes is never served.
	if err != nil || !strings.HasPrefix(key, workspaceID+"/") {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	body, err := a.config.Attachments.Open(request.Context(), key)
	if err != nil {
		log.Printf("attachment %s: open stored bytes: %v", item.ID, err)
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	defer body.Close()
	header := response.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", contentDisposition(item.Filename))
	header.Set("Content-Length", strconv.FormatInt(item.Size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Content-SHA256", item.SHA256)
	header.Set("Cache-Control", "private, no-store")
	response.WriteHeader(http.StatusOK)
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("attachment %s: stream: %v", item.ID, err)
	}
}

func contentDisposition(filename string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '/' {
			return '_'
		}
		return r
	}, filename)
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + encodeAttrChars(filename)
}

// encodeAttrChars percent-encodes every byte that is not an RFC 5987 attr-char.
func encodeAttrChars(value string) string {
	var b strings.Builder
	for _, c := range []byte(value) {
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
