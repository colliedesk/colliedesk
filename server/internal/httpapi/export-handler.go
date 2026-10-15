package httpapi

import (
	"context"
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
	a.streamExport(response, request, workspace.ID, workspace.Slug)
}

// refuseExportIfBusy writes the 409 or 429 answer for a refused admission and reports whether the
// export may go on.
func (a *API) refuseExportIfBusy(response http.ResponseWriter, admission exportAdmission) bool {
	switch admission {
	case exportWorkspaceBusy:
		writeError(response, http.StatusConflict, "export_running", "An export of this workspace is already running. Try again when it finishes.")
		return false
	case exportServerBusy:
		response.Header().Set("Retry-After", exportRetryAfterSeconds)
		writeError(response, http.StatusTooManyRequests, "export_busy", "The server is running as many exports as it allows. Try again in a few seconds.")
		return false
	}
	return true
}

// streamExport takes an export slot for the workspace and streams its archive.
func (a *API) streamExport(response http.ResponseWriter, request *http.Request, workspaceID, workspaceSlug string) {
	release, admission := a.exports.acquire(workspaceID)
	if !a.refuseExportIfBusy(response, admission) {
		return
	}
	defer release()

	exportedAt := a.now().UTC()
	// A client that trickles bytes in just fast enough for the write deadline must still not hold
	// a slot and a snapshot for ever.
	ctx, cancel := context.WithTimeout(request.Context(), a.exportTotalTimeout())
	defer cancel()
	deadline, _ := ctx.Deadline()
	out := &lazyHeaderWriter{ResponseWriter: response, ctx: ctx, controller: http.NewResponseController(response), timeout: a.exportWriteTimeout(), limit: deadline, header: func(h http.Header) {
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="colliedesk-`+workspaceSlug+`-`+exportedAt.Format("20060102T150405Z")+`.zip"`)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "private, no-store")
	}}
	// The deadline outlives this handler on a kept-alive connection unless it is cleared.
	defer func() { _ = out.controller.SetWriteDeadline(time.Time{}) }()
	var opener export.Opener
	if a.config.Attachments != nil {
		opener = a.config.Attachments
	}
	writer := export.NewWriter(out, opener, workspaceID, exportedAt)
	defer writer.Close()
	err := a.store.ExportWorkspace(ctx, workspaceID, writer, store.ExportLimits{})
	if err == nil {
		err = writer.Finish(ctx)
	}
	switch {
	case err == nil:
	case errors.Is(err, store.ErrExportRunning):
		writeError(response, http.StatusConflict, "export_running", "An export of this workspace is already running. Try again when it finishes.")
	case !out.started:
		log.Printf("export %s: %v", workspaceID, err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The export could not be created.")
	default:
		log.Printf("export %s: aborted after %s: %v", workspaceID, time.Since(exportedAt).Round(time.Millisecond), err)
		panic(http.ErrAbortHandler)
	}
}

func (a *API) exportWriteTimeout() time.Duration {
	if a.config.ExportWriteTimeout > 0 {
		return a.config.ExportWriteTimeout
	}
	return defaultExportWriteTimeout * time.Second
}

func (a *API) exportTotalTimeout() time.Duration {
	if a.config.ExportTimeout > 0 {
		return a.config.ExportTimeout
	}
	return defaultExportTotalTimeout
}

// lazyHeaderWriter sets the download headers and the 200 status only when the first byte is
// written, so an error that happens earlier can still be answered with a proper status. Each
// write gets a fresh deadline: progress is allowed, a stalled reader is not.
type lazyHeaderWriter struct {
	http.ResponseWriter
	ctx        context.Context
	controller *http.ResponseController
	timeout    time.Duration
	limit      time.Time // the whole export must end by then
	header     func(http.Header)
	started    bool
}

// extend gives the next write a fresh deadline, never later than the end of the whole export.
func (w *lazyHeaderWriter) extend() {
	next := time.Now().Add(w.timeout)
	if next.After(w.limit) {
		next = w.limit
	}
	// Servers that cannot set deadlines (test recorders) return ErrNotSupported; nothing to bound there.
	_ = w.controller.SetWriteDeadline(next)
}

func (w *lazyHeaderWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if !w.started {
		w.started = true
		w.header(w.Header())
		w.WriteHeader(http.StatusOK)
	}
	w.extend()
	return w.ResponseWriter.Write(p)
}

func (w *lazyHeaderWriter) Flush() {
	if w.started {
		w.extend()
		_ = w.controller.Flush()
	}
}
