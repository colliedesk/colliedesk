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
	workspaceID := principal(request).Workspace.ID
	item, key, err := a.store.GetAttachment(request.Context(), workspaceID, request.PathValue("id"))
	a.streamDownload(response, request, err == nil, workspaceID, item.ID, item.Filename, item.Size, item.SHA256, key)
}

// streamDownload sends stored bytes as a download (application/octet-stream, attachment), or 404 when
// found is false, the server has no storage, or the key is not the workspace's own: storage keys start
// with the owning workspace id, so a row pointing at another workspace's bytes is never served.
func (a *API) streamDownload(response http.ResponseWriter, request *http.Request, found bool, workspaceID, id, filename string, size int64, sha256, key string) {
	if !found || a.config.Attachments == nil || !strings.HasPrefix(key, workspaceID+"/") {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	body, err := a.config.Attachments.Open(request.Context(), key)
	if err != nil {
		log.Printf("attachment %s: open stored bytes: %v", id, err)
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	defer body.Close()
	header := response.Header()
	header.Set("Content-Type", "application/octet-stream")
	header.Set("Content-Disposition", contentDisposition(filename))
	header.Set("Content-Length", strconv.FormatInt(size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Content-SHA256", sha256)
	header.Set("Cache-Control", "private, no-store")
	response.WriteHeader(http.StatusOK)
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("attachment %s: stream: %v", id, err)
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
