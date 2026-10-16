package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	inboundSecretHeader = "X-Colliedesk-Inbound-Secret"
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
	parsed.Platform = platform
	parsed.Cc = a.withoutPlatformAddresses(parsed.Cc)
	if platform {
		var hosted bool
		if workspaceID, hosted, err = a.platformWorkspace(request.Context(), parsed); err != nil {
			writeInternalError(response, err)
			return
		}
		if workspaceID == "" && hosted {
			writeError(response, http.StatusNotFound, "unknown_recipient", "No connected workspace receives mail at this address.")
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
	if errors.Is(err, domain.ErrMailboxClosed) {
		writeError(response, http.StatusForbidden, "mailbox_closed", "This mailbox is not accepting new mail.")
		return
	}
	if writePlanLimit(response, err) {
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

// platformWorkspace routes mail that arrived with the server-wide secret. A recipient under
// HOSTED_MAIL_DOMAIN picks the workspace by its slug (hosted is true even when nothing matches, so the
// caller rejects instead of dropping); otherwise reply+<token>@MAIL_FROM_DOMAIN picks the workspace of
// that conversation.
func (a *API) platformWorkspace(ctx context.Context, parsed email.Inbound) (workspaceID string, hosted bool, err error) {
	config := a.config.Email
	if email.UnderDomain(parsed.EnvelopeTo, config.HostedMailDomain) {
		// Only the envelope recipient counts: To and Cc are written by the sender.
		workspaceID, err = a.hostedWorkspace(ctx, parsed.EnvelopeTo)
		return workspaceID, true, err
	}
	token := email.PlatformReplyToken(parsed.Recipients, config.MailFromDomain)
	if token == "" {
		return "", false, nil
	}
	workspaceID, _, err = a.store.WorkspaceForReplyToken(ctx, token)
	return workspaceID, false, err
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

// hostedWorkspace returns the workspace that receives mail at a hosted address, or "" when no workspace
// with a connected email channel owns it.
func (a *API) hostedWorkspace(ctx context.Context, recipient string) (string, error) {
	slug := email.HostedSlug(recipient, a.config.Email.HostedMailDomain)
	if slug == "" {
		return "", nil
	}
	found, ok, err := a.store.WorkspaceForMailSlug(ctx, slug)
	if err != nil || !ok {
		return "", err
	}
	channel, err := a.store.GetEmailChannel(ctx, found)
	if err != nil || channel == nil {
		return "", err
	}
	return found, nil
}

// checkInboundRecipient answers the hosted-cloud MTA before it accepts RCPT TO: 204 when inbound would
// route mail for this address, 404 when it would reject it. Refusing in the SMTP session means an
// unknown address is never bounced afterwards to a sender that may be forged.
func (a *API) checkInboundRecipient(response http.ResponseWriter, request *http.Request) {
	if !a.isPlatformSecret(request.Header.Get(inboundSecretHeader)) {
		writeError(response, http.StatusUnauthorized, "unauthorized", "The server-wide inbound secret is required.")
		return
	}
	var body struct {
		Recipient string `json:"recipient"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 4096)).Decode(&body); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be {\"recipient\": \"<address>\"}.")
		return
	}
	workspaceID, err := a.hostedWorkspace(request.Context(), body.Recipient)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if workspaceID == "" {
		writeError(response, http.StatusNotFound, "unknown_recipient", "No connected workspace receives mail at this address.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// withoutPlatformAddresses drops the addresses on the hosted forwarding and platform sending domains from
// a Cc list: they are ours, so they are never offered as people to copy.
func (a *API) withoutPlatformAddresses(addresses []string) []string {
	config := a.config.Email
	var kept []string
	for _, address := range addresses {
		if !email.UnderDomain(address, config.HostedMailDomain) && !email.UnderDomain(address, config.MailFromDomain) {
			kept = append(kept, address)
		}
	}
	return kept
}
