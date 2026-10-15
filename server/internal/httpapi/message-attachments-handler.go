package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	// maxMessageFieldBytes bounds the kind and body fields, as decodeJSON bounds a JSON message.
	maxMessageFieldBytes = 1 << 20
	// multipartOverhead is what a request may carry beyond the files: the fields and part headers.
	multipartOverhead = 1 << 20
	// diskReserve is kept free on the attachments volume after a message's files are written.
	diskReserve     = 64 << 20
	maxFilenameRune = 255
	// defaultUploadTimeout is how long a message with files may take to arrive.
	defaultUploadTimeout = 3 * time.Minute
)

var errOutgoingTooLarge = errors.New("outgoing attachments exceed the size limit")

var (
	tooManyFilesMessage = fmt.Sprintf("A message can carry at most %d files. Nothing was sent or stored.", attachments.MaxOutgoingFiles)
	tooLargeMessage     = fmt.Sprintf("The files of one message can be at most %d MiB in total. Nothing was sent or stored.", attachments.MaxOutgoingBytes>>20)
)

// messageFields are the text fields of a multipart message request.
type messageFields struct{ kind, body string }

func (f messageFields) validate() error {
	if (f.kind != "outgoing" && f.kind != "note") || strings.TrimSpace(f.body) == "" {
		return &uploadError{http.StatusBadRequest, "invalid_request", "A reply or note body is required."}
	}
	return nil
}

// createMessageWithFiles handles POST .../messages as multipart/form-data: fields kind and body plus
// repeated files. Each file streams to attachment storage; the message and its attachment rows are
// then written in one transaction. Whatever fails, nothing is left behind.
func (a *API) createMessageWithFiles(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	workspaceID, conversationID := identity.Workspace.ID, request.PathValue("id")
	// A conversation of another workspace is refused before a byte is stored.
	if _, err := a.store.GetConversation(request.Context(), workspaceID, conversationID); err != nil {
		writeStoreError(response, err)
		return
	}
	// A slow or stalled client cannot hold the request, and the files it already sent, open forever.
	timeout := a.config.UploadTimeout
	if timeout <= 0 {
		timeout = defaultUploadTimeout
	}
	_ = http.NewResponseController(response).SetReadDeadline(time.Now().Add(timeout)) //nolint:errcheck // unsupported writers have no deadline to set
	request.Body = http.MaxBytesReader(response, request.Body, attachments.MaxOutgoingBytes+multipartOverhead)
	reader, err := request.MultipartReader()
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A reply or note body is required.")
		return
	}
	var stored []store.NewAttachment
	defer func() { a.discardUnreferenced(context.WithoutCancel(request.Context()), workspaceID, stored) }()
	fields, files, err := a.readMessageParts(request.Context(), reader, workspaceID, conversationID, &stored)
	if err != nil {
		a.writeUploadError(response, err)
		return
	}
	if err := fields.validate(); err != nil {
		a.writeUploadError(response, err)
		return
	}
	item, err := a.store.CreateMessage(request.Context(), workspaceID, conversationID, identity.Agent.ID, fields.kind, fields.body, files...)
	if errors.Is(err, store.ErrObjectMissing) {
		writeError(response, http.StatusInternalServerError, "internal_error", "A file was lost while the message was saved. Nothing was sent; try again.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	stored = nil // referenced now: keep the bytes
	writeJSON(response, http.StatusCreated, item)
}

type uploadError struct {
	status        int
	code, message string
}

func (e *uploadError) Error() string { return e.message }

func (a *API) writeUploadError(response http.ResponseWriter, err error) {
	var upload *uploadError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &upload):
		writeError(response, upload.status, upload.code, upload.message)
	case errors.Is(err, errOutgoingTooLarge), errors.As(err, &tooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "attachments_too_large", tooLargeMessage)
	case errors.Is(err, os.ErrDeadlineExceeded):
		writeError(response, http.StatusRequestTimeout, "upload_timeout", "The upload took too long. Nothing was sent or stored.")
	case errors.Is(err, context.Canceled):
		writeError(response, http.StatusBadRequest, "invalid_request", "The upload was interrupted.")
	case writePlanLimit(response, err):
	default:
		slog.Error("store message attachment", "error", err)
		writeError(response, http.StatusInternalServerError, "internal_error", "The files could not be stored. Nothing was sent.")
	}
}

// readMessageParts reads the fields and streams every file to storage, recording each stored object
// in stored as soon as it exists so the caller can clean up after any later failure.
func (a *API) readMessageParts(ctx context.Context, reader *multipart.Reader, workspaceID, conversationID string, stored *[]store.NewAttachment) (messageFields, []store.NewAttachment, error) {
	var fields messageFields
	remaining := int64(attachments.MaxOutgoingBytes)
	admitted := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return fields, *stored, nil
		}
		if err != nil {
			return fields, nil, invalidUpload(err)
		}
		switch name := part.FormName(); {
		case name == "kind" || name == "body":
			text, err := readField(part)
			if err != nil {
				return fields, nil, err
			}
			if name == "kind" {
				fields.kind = text
			} else {
				fields.body = text
			}
		case name == "files" && part.FileName() != "":
			// Fields come first, so a refusal that does not depend on the bytes is given before reading any.
			if !admitted {
				if err := fields.validate(); err != nil {
					return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send kind and body before the files."}
				}
				if err := a.store.CheckAttachmentAdmission(ctx, workspaceID, conversationID, fields.kind); err != nil {
					return fields, nil, err
				}
				admitted = true
			}
			if len(*stored) >= attachments.MaxOutgoingFiles {
				return fields, nil, &uploadError{http.StatusRequestEntityTooLarge, "attachments_too_many", tooManyFilesMessage}
			}
			if err := a.checkDiskSpace(); err != nil {
				return fields, nil, err
			}
			object, err := a.config.Attachments.Put(ctx, workspaceID, &capReader{reader: part, remaining: &remaining}, -1)
			if err != nil {
				return fields, nil, err
			}
			*stored = append(*stored, store.NewAttachment{Filename: cleanFilename(part.FileName()), ContentType: cleanContentType(part.Header.Get("Content-Type")), Object: object})
		default:
			return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send kind, body and files only; every file needs a filename."}
		}
	}
}

func invalidUpload(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	return &uploadError{http.StatusBadRequest, "invalid_request", "The multipart request could not be read."}
}

func readField(part io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(part, maxMessageFieldBytes+1))
	if err != nil {
		return "", invalidUpload(err)
	}
	if len(data) > maxMessageFieldBytes || !utf8.Valid(data) {
		return "", &uploadError{http.StatusBadRequest, "invalid_request", "A reply or note body is required."}
	}
	return string(data), nil
}

// checkDiskSpace refuses a file while the attachments volume cannot hold a whole message and the
// reserve. A volume that cannot report its space is trusted; Put still fails on a full disk.
func (a *API) checkDiskSpace() error {
	if a.config.Attachments == nil {
		return &uploadError{http.StatusServiceUnavailable, "attachments_unavailable", "Attachment storage is not configured on this server."}
	}
	reporter, ok := a.config.Attachments.(attachments.SpaceReporter)
	if !ok {
		return nil
	}
	if free, err := reporter.Available(); err == nil && free < attachments.MaxOutgoingBytes+diskReserve {
		return &uploadError{http.StatusInsufficientStorage, "storage_full", "The server is low on disk space for attachments. Nothing was sent or stored."}
	}
	return nil
}

// discardUnreferenced deletes stored objects no attachment row points at; bytes another message
// already references stay. An error keeps the object: a stray file costs space, a deleted shared
// file loses a customer's data.
func (a *API) discardUnreferenced(ctx context.Context, workspaceID string, stored []store.NewAttachment) {
	seen := map[string]bool{}
	for _, file := range stored {
		key := file.Object.Key
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := a.store.DiscardObject(ctx, workspaceID, key); err != nil {
			slog.Error("discard attachment after failed upload", "error", err)
		}
	}
}

// capReader fails once the shared byte budget of one message is spent.
type capReader struct {
	reader    io.Reader
	remaining *int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if limit := *c.remaining + 1; int64(len(p)) > limit {
		p = p[:limit]
	}
	n, err := c.reader.Read(p)
	*c.remaining -= int64(n)
	if *c.remaining < 0 {
		return n, errOutgoingTooLarge
	}
	return n, err
}

func cleanFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if runes := []rune(name); len(runes) > maxFilenameRune {
		name = string(runes[:maxFilenameRune])
	}
	return strings.TrimSpace(name)
}

func cleanContentType(value string) string {
	parsed, _, err := mime.ParseMediaType(value)
	if err != nil || strings.HasPrefix(parsed, "multipart/") || strings.HasPrefix(parsed, "message/") {
		return "application/octet-stream"
	}
	return parsed
}
