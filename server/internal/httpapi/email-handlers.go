package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const inboundEmailPath = "/api/v1/channels/email/inbound"

type smtpStatus struct {
	Configured    bool   `json:"configured"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	StartTLS      bool   `json:"starttls"`
	Authenticated bool   `json:"authenticated"`
}

type emailChannelSettings struct {
	Channel    *domain.EmailChannel `json:"channel"`
	InboundURL string               `json:"inbound_url"`
	SMTP       smtpStatus           `json:"smtp"`
}

func (a *API) emailSettings(channel *domain.EmailChannel) emailChannelSettings {
	smtp := a.config.SMTP
	return emailChannelSettings{Channel: channel, InboundURL: a.config.Origin + inboundEmailPath, SMTP: smtpStatus{
		Configured: smtp.Configured(), Host: smtp.Host, Port: smtp.Port, StartTLS: smtp.StartTLS, Authenticated: smtp.Username != "",
	}}
}

func (a *API) getEmailChannel(response http.ResponseWriter, request *http.Request) {
	channel, err := a.store.GetEmailChannel(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, a.emailSettings(channel))
}

func (a *API) putEmailChannel(response http.ResponseWriter, request *http.Request) {
	var input struct {
		SupportAddress string `json:"support_address"`
	}
	if err := decodeJSON(request, &input); err != nil || !plainAddress(input.SupportAddress) {
		writeError(response, http.StatusBadRequest, "invalid_request", "A plain support address such as support@example.com is required.")
		return
	}
	channel, err := a.store.UpsertEmailChannel(request.Context(), principal(request).Workspace.ID, input.SupportAddress)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, a.emailSettings(&channel))
}

// plainAddress accepts a bare addr-spec only: no display name, no comments, no line breaks.
func plainAddress(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Name == "" && parsed.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n<>")
}

func (a *API) deleteEmailChannel(response http.ResponseWriter, request *http.Request) {
	deleted, err := a.store.DeleteEmailChannel(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !deleted {
		writeError(response, http.StatusNotFound, "not_found", "The email channel is not connected.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) rotateEmailSecret(response http.ResponseWriter, request *http.Request) {
	secret, err := a.store.RotateEmailSecret(request.Context(), principal(request).Workspace.ID, time.Now().UTC())
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"inbound_secret": secret, "inbound_url": a.config.Origin + inboundEmailPath})
}

func (a *API) retryMessageDelivery(response http.ResponseWriter, request *http.Request) {
	message, err := a.store.RetryDelivery(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("messageId"))
	if errors.Is(err, store.ErrDeliveryNotFailed) {
		writeError(response, http.StatusConflict, "not_failed", "Only a failed delivery can be retried.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, message)
}

// writeInternalError hides the cause from the client and keeps it in the server log.
func writeInternalError(response http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	writeError(response, http.StatusInternalServerError, "internal_error", "The request could not be completed. Try again.")
}
