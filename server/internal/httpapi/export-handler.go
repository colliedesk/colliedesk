package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// exportWorkspace streams every record of the caller's workspace as one ZIP. Nothing is buffered:
// a failure before the first byte is a JSON error; a failure after it aborts the connection so
// the client sees a truncated archive (no central directory, no manifest), never a quiet success.
func (a *API) exportWorkspace(response http.ResponseWriter, request *http.Request) {
	workspace := principal(request).Workspace
	exportedAt := a.now().UTC()
	out := &lazyHeaderWriter{ResponseWriter: response, header: func(h http.Header) {
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="colliedesk-`+workspace.Slug+`-`+exportedAt.Format("20060102T150405Z")+`.zip"`)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")
	}}
	var opener export.Opener
	if a.config.Attachments != nil {
		opener = a.config.Attachments
	}
	writer := export.NewWriter(out, opener, workspace.ID, exportedAt)
	err := a.store.ExportWorkspace(request.Context(), workspace.ID, writer)
	if err == nil {
		err = writer.Finish(request.Context())
	}
	switch {
	case err == nil:
	case errors.Is(err, store.ErrExportRunning):
		writeError(response, http.StatusConflict, "export_running", "An export of this workspace is already running. Try again when it finishes.")
	case !out.started:
		log.Printf("export %s: %v", workspace.ID, err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The export could not be created.")
	default:
		log.Printf("export %s: aborted after %s: %v", workspace.ID, time.Since(exportedAt).Round(time.Millisecond), err)
		panic(http.ErrAbortHandler)
	}
}

// lazyHeaderWriter sets the download headers and the 200 status only when the first byte is
// written, so an error that happens earlier can still be answered with a proper status.
type lazyHeaderWriter struct {
	http.ResponseWriter
	header  func(http.Header)
	started bool
}

func (w *lazyHeaderWriter) Write(p []byte) (int, error) {
	if !w.started {
		w.started = true
		w.header(w.Header())
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *lazyHeaderWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok && w.started {
		flusher.Flush()
	}
}
