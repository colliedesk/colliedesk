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
//
// A client that stops reading must not pin database connections: every write has a deadline, so
// the export fails, its transaction rolls back and its connection returns to the pool.
func (a *API) exportWorkspace(response http.ResponseWriter, request *http.Request) {
	workspace := principal(request).Workspace
	release, admission := a.exports.acquire(workspace.ID)
	switch admission {
	case exportWorkspaceBusy:
		writeError(response, http.StatusConflict, "export_running", "An export of this workspace is already running. Try again when it finishes.")
		return
	case exportServerBusy:
		response.Header().Set("Retry-After", exportRetryAfterSeconds)
		writeError(response, http.StatusTooManyRequests, "export_busy", "The server is running as many exports as it allows. Try again in a few seconds.")
		return
	}
	defer release()

	exportedAt := a.now().UTC()
	out := &lazyHeaderWriter{ResponseWriter: response, controller: http.NewResponseController(response), timeout: a.exportWriteTimeout(), header: func(h http.Header) {
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="colliedesk-`+workspace.Slug+`-`+exportedAt.Format("20060102T150405Z")+`.zip"`)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")
	}}
	// The deadline outlives this handler on a kept-alive connection unless it is cleared.
	defer func() { _ = out.controller.SetWriteDeadline(time.Time{}) }()
	var opener export.Opener
	if a.config.Attachments != nil {
		opener = a.config.Attachments
	}
	writer := export.NewWriter(out, opener, workspace.ID, exportedAt)
	defer writer.Close()
	err := a.store.ExportWorkspace(request.Context(), workspace.ID, writer, store.ExportLimits{})
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

func (a *API) exportWriteTimeout() time.Duration {
	if a.config.ExportWriteTimeout > 0 {
		return a.config.ExportWriteTimeout
	}
	return defaultExportWriteTimeout * time.Second
}

// lazyHeaderWriter sets the download headers and the 200 status only when the first byte is
// written, so an error that happens earlier can still be answered with a proper status. Each
// write gets a fresh deadline: progress is allowed, a stalled reader is not.
type lazyHeaderWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	timeout    time.Duration
	header     func(http.Header)
	started    bool
}

func (w *lazyHeaderWriter) Write(p []byte) (int, error) {
	if !w.started {
		w.started = true
		w.header(w.Header())
		w.WriteHeader(http.StatusOK)
	}
	// Servers that cannot set deadlines (test recorders) return ErrNotSupported; nothing to bound there.
	_ = w.controller.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.ResponseWriter.Write(p)
}

func (w *lazyHeaderWriter) Flush() {
	if w.started {
		_ = w.controller.SetWriteDeadline(time.Now().Add(w.timeout))
		_ = w.controller.Flush()
	}
}
