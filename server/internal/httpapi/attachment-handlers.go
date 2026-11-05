package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
)

// downloadAttachment streams stored bytes as a download. Files come from
// customers and other systems, so they are never rendered inline on the app
// origin: the response is always application/octet-stream + attachment, with a
// sandboxing Content-Security-Policy. The web app shows images, video and audio
// through <img>, <video> and <audio>, which read the bytes without a page.
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
	// A byte range is answered (206) so the web app's players read metadata and seek without the whole file.
	response.Header().Set("X-Content-SHA256", sha256)
	if !attachments.Serve(response, request, a.config.Attachments, attachments.Served{Key: key, ContentType: "application/octet-stream",
		Size: size, ETag: sha256, Cache: "private, no-store", Disposition: contentDisposition(filename)}) {
		response.Header().Del("X-Content-SHA256")
		log.Printf("attachment %s: serve stored bytes", id)
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
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
