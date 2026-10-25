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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
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
type messageFields struct {
	kind, body string
	// cc holds the cc fields as sent, one address each.
	cc []string
	// opening is set for the first reply of a new conversation, which also names the contact and the
	// subject; it is always an outgoing reply.
	opening            bool
	contactID, subject string
	// channelID is the channel a new conversation starts from; empty for the default one.
	channelID string
	// contactName and contactEmail add a customer with the conversation, instead of contactID.
	contactName, contactEmail string
	// seen is the seen_reply_number field: the latest reply the sender had seen; nil when not sent.
	seen *int
	// aiDraft is the ai_draft_id field: the AI draft the reply was written from.
	aiDraft string
}

// newContact is the customer the fields add, or nil when they name a contact of the workspace.
func (f messageFields) newContact() *store.NewContact {
	if f.contactName == "" && f.contactEmail == "" {
		return nil
	}
	return &store.NewContact{Name: f.contactName, Email: f.contactEmail}
}

func (f messageFields) validate() error {
	if f.opening {
		added := f.newContact()
		if (f.contactID == "") == (added == nil) || strings.TrimSpace(f.subject) == "" || strings.TrimSpace(f.body) == "" {
			return &uploadError{http.StatusBadRequest, "invalid_request", "Contact, subject and body are required."}
		}
		if added != nil {
			if problem := newContactProblem(added, true); problem != "" {
				return &uploadError{http.StatusBadRequest, "invalid_request", problem}
			}
		}
		return nil
	}
	if (f.kind != "outgoing" && f.kind != "note") || strings.TrimSpace(f.body) == "" {
		return &uploadError{http.StatusBadRequest, "invalid_request", "A reply or note body is required."}
	}
	if f.seen != nil && *f.seen < 0 {
		return &uploadError{http.StatusBadRequest, "invalid_request", "seen_reply_number is the number of a reply, 0 or more."}
	}
	return nil
}

// createMessageWithFiles handles POST .../messages as multipart/form-data: fields kind and body plus
// repeated files. Each file streams to attachment storage; the message and its attachment rows are
// then written in one transaction. Whatever fails, nothing is left behind.
func (a *API) createMessageWithFiles(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	workspaceID, conversationID := identity.Workspace.ID, request.PathValue("id")
	// A token that may not write any message is refused before a byte is stored; the kind, and so
	// whether a reply needs messages:send, is known only once the fields are read. Files cannot wait
	// days for a person's approval, so such a call is refused with the way forward.
	if !identity.Allows(auth.PermMessagesNote) && !identity.Allows(auth.PermMessagesSend) {
		writeFilesNeedApproval(response)
		return
	}
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
	fields, files, err := a.readMessageParts(request.Context(), reader, workspaceID, conversationID, messageFields{}, &stored)
	if a.writeReplyConflict(response, identity, err) {
		return
	}
	if err != nil {
		a.writeUploadError(response, err)
		return
	}
	if err := fields.validate(); err != nil {
		a.writeUploadError(response, err)
		return
	}
	actor, ok := messageActor(request.Context(), identity, fields.kind)
	if !ok {
		writeFilesNeedApproval(response)
		return
	}
	actor.AIDraftID = fields.aiDraft
	cc, refusal := a.cleanCC(request.Context(), workspaceID, fields.kind, fields.cc)
	if refusal != nil {
		a.writeUploadError(response, refusal)
		return
	}
	item, err := a.store.CreateReply(request.Context(), workspaceID, conversationID, actor, fields.kind, fields.body, cc, fields.seen, files...)
	if a.writeReplyConflict(response, identity, err) {
		return
	}
	if errors.Is(err, store.ErrObjectMissing) {
		writeError(response, http.StatusInternalServerError, "internal_error", "A file was lost while the message was saved. Nothing was sent; try again.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.recordReply(identity, fields.kind)
	stored = nil // referenced now: keep the bytes
	writeJSON(response, http.StatusCreated, item)
}

// noConversationYet stands for the conversation a new conversation's files belong to before it exists.
// Checked against it, the trial refuses early what StartConversation would refuse: a reply to a
// customer who never wrote.
const noConversationYet = "00000000-0000-0000-0000-000000000000"

// startConversationWithFiles handles POST /conversations as multipart/form-data: fields contact_id,
// subject, body and cc, then repeated files. The files stream to storage first; the conversation, its
// first reply and the attachment rows are then written in one transaction, and whatever fails leaves
// nothing behind. The route needs the send tier, so a token that gets here may send.
func (a *API) startConversationWithFiles(response http.ResponseWriter, request *http.Request) {
	identity := principal(request)
	workspaceID := identity.Workspace.ID
	timeout := a.config.UploadTimeout
	if timeout <= 0 {
		timeout = defaultUploadTimeout
	}
	_ = http.NewResponseController(response).SetReadDeadline(time.Now().Add(timeout)) //nolint:errcheck // unsupported writers have no deadline to set
	request.Body = http.MaxBytesReader(response, request.Body, attachments.MaxOutgoingBytes+multipartOverhead)
	reader, err := request.MultipartReader()
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Contact, subject and body are required.")
		return
	}
	var stored []store.NewAttachment
	defer func() { a.discardUnreferenced(context.WithoutCancel(request.Context()), workspaceID, stored) }()
	fields, files, err := a.readMessageParts(request.Context(), reader, workspaceID, noConversationYet, messageFields{opening: true, kind: "outgoing"}, &stored)
	if err != nil {
		a.writeUploadError(response, err)
		return
	}
	if err := fields.validate(); err != nil {
		a.writeUploadError(response, err)
		return
	}
	actor, _ := messageActor(request.Context(), identity, "outgoing")
	cc, refusal := a.cleanCC(request.Context(), workspaceID, "outgoing", fields.cc)
	if refusal != nil {
		a.writeUploadError(response, refusal)
		return
	}
	added := fields.newContact()
	if added != nil {
		newContactProblem(added, true) // trims what validate checked
	}
	item, err := a.store.StartConversation(request.Context(), workspaceID, store.NewConversation{ContactID: fields.contactID, NewContact: added, Subject: fields.subject,
		ChannelID: fields.channelID},
		actor, fields.body, cc, files...)
	if errors.Is(err, store.ErrObjectMissing) {
		writeError(response, http.StatusInternalServerError, "internal_error", "A file was lost while the conversation was saved. Nothing was sent; try again.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.recordReply(identity, "outgoing")
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
// fields carries what is known before the parts are read (opening for a new conversation).
func (a *API) readMessageParts(ctx context.Context, reader *multipart.Reader, workspaceID, conversationID string, fields messageFields, stored *[]store.NewAttachment) (messageFields, []store.NewAttachment, error) {
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
		case name == "kind" && !fields.opening, name == "body", name == "cc", name == "seen_reply_number" && !fields.opening, name == "ai_draft_id" && !fields.opening,
			fields.opening && (name == "contact_id" || name == "subject" || name == "contact_name" || name == "contact_email"):
			text, err := readField(part)
			if err != nil {
				return fields, nil, err
			}
			switch name {
			case "kind":
				fields.kind = text
			case "contact_id":
				fields.contactID = text
			case "subject":
				fields.subject = text
			case "channel_id":
				fields.channelID = text
			case "contact_name":
				fields.contactName = text
			case "contact_email":
				fields.contactEmail = text
			case "body":
				fields.body = text
			case "ai_draft_id":
				fields.aiDraft = text
			case "seen_reply_number":
				seen, err := strconv.Atoi(text)
				if err != nil {
					return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "seen_reply_number is the number of a reply, 0 or more."}
				}
				fields.seen = &seen
			default:
				if len(fields.cc) > maxCC {
					return fields, nil, a.invalidCC("A reply can be copied to at most 10 addresses.")
				}
				fields.cc = append(fields.cc, text)
			}
		case name == "files" && part.FileName() != "":
			// Fields come first, so a refusal that does not depend on the bytes is given before reading any.
			if !admitted {
				if err := fields.validate(); err != nil && fields.opening {
					return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send contact_id, subject and body before the files."}
				} else if err != nil {
					return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send kind and body before the files."}
				}
				cc, refusal := a.cleanCC(ctx, workspaceID, fields.kind, fields.cc)
				if refusal != nil {
					return fields, nil, refusal
				}
				if err := a.store.CheckAttachmentAdmission(ctx, workspaceID, conversationID, fields.channelID, fields.kind, cc); err != nil {
					return fields, nil, err
				}
				// A reply that would be refused for a newer one is refused before its files are stored.
				if fields.kind == "outgoing" && fields.seen != nil && !fields.opening {
					if err := a.store.CheckReplySeen(ctx, workspaceID, conversationID, *fields.seen); err != nil {
						return fields, nil, err
					}
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
			if fields.opening {
				return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send contact_id (or contact_name and contact_email), subject, body, cc and files only; every file needs a filename."}
			}
			return fields, nil, &uploadError{http.StatusBadRequest, "invalid_request", "Send kind, body, cc and files only; every file needs a filename."}
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
