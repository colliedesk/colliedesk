package httpapi

import (
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbFileHead is how many first bytes of a file judge its type.
const kbFileHead = 8 << 10

// writeKBFileStartRefusal refuses, before any byte is sent, a file over the size limit or whose name
// is not of an allowed type; it answers whether the upload may start.
func writeKBFileStartRefusal(response http.ResponseWriter, name string, size int64) bool {
	switch {
	case strings.TrimSpace(name) == "":
		writeError(response, http.StatusBadRequest, "invalid_request", "A file needs its name, with its extension (Warranty form.pdf).")
	case media.CheckFileName(name) != nil:
		writeError(response, http.StatusBadRequest, "unsupported_file_type", sentence(media.ErrFileType)+"; nothing was stored.")
	case size > domain.MaxKBFileBytes:
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "A file holds at most 25 MiB; nothing was stored.")
	default:
		return true
	}
	return false
}

// completeKBFileUpload judges a file whose every byte arrived by its first bytes and its name, reads
// its text for search and stores it. A refused file cancels the upload and drops its parts.
func (a *API) completeKBFileUpload(response http.ResponseWriter, request *http.Request, upload domain.KBMediaUpload, path string, actor store.Actor) {
	who := principal(request)
	staging := a.kbStaging()
	contentType, text, err := checkKBFile(path, upload.Name, upload.Size)
	if err != nil {
		_ = a.store.CancelKBMediaUpload(request.Context(), who.Workspace.ID, upload.ID, "", true)
		_ = staging.RemoveStaged(upload.ID)
		writeKBUploadError(response, err)
		return
	}
	if err := a.store.CheckKBVideoAdmission(request.Context(), who.Workspace.ID, upload.Size); err != nil {
		writeKBUploadError(response, err)
		return
	}
	object, err := staging.Adopt(request.Context(), who.Workspace.ID, path)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	item, _, err := a.store.CompleteKBMediaUpload(request.Context(), who.Workspace.ID, actor, upload.ID,
		store.NewKBVideo{Kind: domain.KBMediaFile, Text: text, Object: object, ContentType: contentType})
	if err != nil {
		if discardErr := a.store.DiscardObject(request.Context(), who.Workspace.ID, object.Key); discardErr != nil {
			log.Printf("knowledge base upload %s: discard unrecorded bytes: %v", upload.ID, discardErr)
		}
		writeKBUploadError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}

// checkKBFile returns the content type and the searchable text of the staged file named name.
func checkKBFile(path, name string, size int64) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || info.Size() != size {
		return "", "", domain.ErrKBMediaUploadIncomplete
	}
	head := make([]byte, min(size, kbFileHead))
	if _, err := io.ReadFull(file, head); err != nil {
		return "", "", err
	}
	contentType, err := media.InspectFile(name, head)
	if err != nil {
		return "", "", err
	}
	return contentType, media.FileText(contentType, file, size), nil
}
