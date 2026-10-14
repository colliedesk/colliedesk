package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	inboundSecretHeader = "X-CollieDesk-Inbound-Secret"
	maxInboundBytes     = 25 << 20
)

// receiveInboundEmail accepts one raw message from a forwarder. Accepted outcomes are 200 so the
// forwarder does not retry; storage errors are 500 so it does, and nothing is lost silently.
func (a *API) receiveInboundEmail(response http.ResponseWriter, request *http.Request) {
	workspaceID, ok, err := a.store.WorkspaceForInboundSecret(request.Context(), request.Header.Get(inboundSecretHeader))
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid inbound secret is required.")
		return
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
	result, err := a.store.IngestEmail(request.Context(), workspaceID, parsed, time.Now().UTC())
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
