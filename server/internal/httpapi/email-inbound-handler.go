package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	inboundSecretHeader = "X-CollieDesk-Inbound-Secret"
	maxInboundBytes     = 25 << 20
	maxInboundFiles     = 100
)

// receiveInboundEmail accepts one raw message from a forwarder. Accepted outcomes are 200 so the
// forwarder does not retry; storage errors are 500 so it does, and nothing is lost silently.
func (a *API) receiveInboundEmail(response http.ResponseWriter, request *http.Request) {
	secret := request.Header.Get(inboundSecretHeader)
	platform := a.isPlatformSecret(secret)
	workspaceID := ""
	if !platform {
		found, ok, err := a.store.WorkspaceForInboundSecret(request.Context(), secret)
		if err != nil {
			writeInternalError(response, err)
			return
		}
		if !ok {
			writeError(response, http.StatusUnauthorized, "unauthorized", "A valid inbound secret is required.")
			return
		}
		workspaceID = found
	}
	raw, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxInboundBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "The message is larger than 25 MiB.")
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The message body could not be read.")
		return
	}
	parsed, err := email.Parse(raw, a.config.Email.TrustedAuthServID)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_message", "The body is not an RFC 5322 message with a sender.")
		return
	}
	if len(parsed.Files) > maxInboundFiles {
		writeError(response, http.StatusRequestEntityTooLarge, "too_many_files", fmt.Sprintf("The message has more than %d attachments.", maxInboundFiles))
		return
	}
	if platform {
		if workspaceID, err = a.platformWorkspace(request.Context(), parsed); err != nil {
			writeInternalError(response, err)
			return
		}
		if workspaceID == "" {
			reason := "unknown_recipient"
			writeJSON(response, http.StatusOK, store.InboundResult{Status: "dropped", Reason: &reason, Authentication: parsed.Authentication})
			return
		}
	}
	if a.config.Attachments == nil {
		writeInternalError(response, errors.New("attachment storage is not configured; inbound mail cannot be kept"))
		return
	}
	result, err := a.store.IngestEmail(request.Context(), workspaceID, parsed, a.now(), a.stashEmail(workspaceID, raw, parsed.Files))
	if errors.Is(err, store.ErrRateLimited) {
		response.Header().Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many messages from this sender. Retry later.")
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

// isPlatformSecret reports whether the secret is the server-wide secret of the MAIL_FROM_DOMAIN forwarder.
func (a *API) isPlatformSecret(secret string) bool {
	configured := a.config.Email.PlatformInboundSecret
	if configured == "" || secret == "" {
		return false
	}
	want, got := sha256.Sum256([]byte(configured)), sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// platformWorkspace maps reply+<token>@MAIL_FROM_DOMAIN to the workspace of that conversation.
func (a *API) platformWorkspace(ctx context.Context, parsed email.Inbound) (string, error) {
	token := email.PlatformReplyToken(parsed.Recipients, a.config.Email.MailFromDomain)
	if token == "" {
		return "", nil
	}
	workspaceID, _, err := a.store.WorkspaceForReplyToken(ctx, token)
	return workspaceID, err
}

// stashEmail writes the gzip-compressed raw message and every file of the message to attachment storage.
// Objects are content-addressed and may be shared with other messages, so a failure never deletes them;
// the forwarder's retry writes the same keys and references them.
func (a *API) stashEmail(workspaceID string, raw []byte, files []email.File) store.StashFunc {
	return func(ctx context.Context) ([]store.EmailFile, error) {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if _, err := writer.Write(raw); err != nil {
			return nil, fmt.Errorf("compress original message: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("compress original message: %w", err)
		}
		stored := make([]store.EmailFile, 0, len(files)+1)
		put := func(filename, contentType string, data []byte, original bool) error {
			object, err := a.config.Attachments.Put(ctx, workspaceID, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return err
			}
			stored = append(stored, store.EmailFile{Filename: filename, ContentType: contentType, Original: original, Object: object})
			return nil
		}
		if err := put("original-message.eml.gz", "application/gzip", compressed.Bytes(), true); err != nil {
			return nil, err
		}
		for _, file := range files {
			if err := put(file.Filename, file.ContentType, file.Data, false); err != nil {
				return nil, err
			}
		}
		return stored, nil
	}
}
