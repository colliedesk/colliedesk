package httpapi

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/github/githubsig"
)

// maxGitHubWebhookBytes bounds a GitHub delivery (GitHub caps payloads at 25 MB; Autofix's events are far
// smaller).
const maxGitHubWebhookBytes = 5 << 20

// receiveGitHubWebhook applies the Colliedesk GitHub App's events. The signature is the only credential:
// nothing is read from the database until it verifies. A delivery already applied answers duplicate;
// an event acts only in the workspace that owns its installation. A failure answers 500 so GitHub
// redelivers (nothing recorded the delivery).
func (a *API) receiveGitHubWebhook(response http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, maxGitHubWebhookBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(response, http.StatusRequestEntityTooLarge, "payload_too_large", "A webhook body over 5 MB is refused; nothing was stored.")
		return
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The webhook body could not be read.")
		return
	}
	if err := githubsig.Verify(a.config.GitHub.WebhookSecret, request.Header.Get(githubsig.Header), body); err != nil || a.githubSync == nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "A valid webhook signature is required.")
		return
	}
	delivery := request.Header.Get("X-GitHub-Delivery")
	if len(delivery) > 100 {
		delivery = ""
	}
	status, err := a.githubSync.HandleEvent(request.Context(), request.Header.Get("X-GitHub-Event"), delivery, body)
	if err != nil {
		slog.Error("autofix: apply a GitHub webhook", "error", err, "event", request.Header.Get("X-GitHub-Event"), "delivery", delivery)
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": status})
}
