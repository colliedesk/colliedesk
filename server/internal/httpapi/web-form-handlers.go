package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/webform"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	webFormPathPrefix    = webform.SubmitPathPrefix
	webFormSubmitPattern = webFormPathPrefix + "{workspace_slug}"
	// webFormChannelPattern is one form's own address, by its channel id.
	webFormChannelPattern = webform.ChannelSubmitPathPrefix + "{channel_id}"
	maxWebFormBody        = 64 << 10
	maxWebFormMessage     = 5000
	maxWebFormSource      = 100
	maxWebFormEmail       = 254
	webFormPreflightAge   = "600"
)

// slugShape is the shape the workspaces table enforces; anything else cannot be a workspace.
var slugShape = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (a *API) getWebForm(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	form, err := a.store.GetWebForm(request.Context(), who.Workspace.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	form.SubmitPath = webFormPathPrefix + who.Workspace.Slug
	writeJSON(response, http.StatusOK, form)
}

func (a *API) putWebForm(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Enabled        *bool    `json:"enabled"`
		AllowedOrigins []string `json:"allowed_origins"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Enabled == nil || input.AllowedOrigins == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Both enabled and allowed_origins are required.")
		return
	}
	origins, err := normalizeOrigins(input.AllowedOrigins)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	who := principal(request)
	form, err := a.store.SetWebForm(request.Context(), who.Workspace.ID, *input.Enabled, origins)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	form.SubmitPath = webFormPathPrefix + who.Workspace.Slug
	writeJSON(response, http.StatusOK, form)
}

// normalizeOrigins lowercases, validates and de-duplicates the allowed origins, keeping their order.
func normalizeOrigins(values []string) ([]string, error) { return webform.NormalizeOrigins(values) }

// allowedOrigin reports whether the request's Origin header is on the workspace's list.
func allowedOrigin(request *http.Request, origins []string) (string, bool) {
	origin := request.Header.Get("Origin")
	return origin, origin != "" && slices.Contains(origins, origin)
}

func writeFormNotFound(response http.ResponseWriter) {
	writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
}

// refuseBusyAddress answers 429 and reports true when the address has used up its requests to the
// public forms of every workspace together. It runs before any database lookup.
func (a *API) refuseBusyAddress(response http.ResponseWriter, client string) bool {
	allowed, wait := a.webFormClients.allow(client, a.now())
	if allowed {
		return false
	}
	response.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a minute.")
	return true
}

// openRequestedForm reads the form a public request names, after wellFormedFormName: by the workspace
// slug (its default form) or by its channel id.
func (a *API) openRequestedForm(request *http.Request) (store.OpenForm, bool, error) {
	if slug := request.PathValue("workspace_slug"); slug != "" {
		return a.store.OpenWebForm(request.Context(), slug)
	}
	return a.store.OpenWebFormChannel(request.Context(), request.PathValue("channel_id"))
}

// wellFormedFormName reports whether the request names a form in a shape one can have, before any lookup.
func wellFormedFormName(request *http.Request) bool {
	if slug := request.PathValue("workspace_slug"); slug != "" {
		return len(slug) <= 63 && slugShape.MatchString(slug)
	}
	return uuidShape.MatchString(request.PathValue("channel_id"))
}

// preflightWebForm answers the browser's CORS preflight. Every refusal is the same 404 as a
// missing form, so an origin that is not listed learns nothing about the workspace.
func (a *API) preflightWebForm(response http.ResponseWriter, request *http.Request) {
	if !wellFormedFormName(request) {
		writeFormNotFound(response)
		return
	}
	if a.refuseBusyAddress(response, clientAddress(request, a.config.TrustedProxies)) {
		return
	}
	form, open, err := a.openRequestedForm(request)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	origin, listed := allowedOrigin(request, form.Origins)
	if !open || !listed {
		writeFormNotFound(response)
		return
	}
	header := response.Header()
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	header.Set("Access-Control-Allow-Headers", "Content-Type")
	header.Set("Access-Control-Max-Age", webFormPreflightAge)
	header.Set("Vary", "Origin")
	response.WriteHeader(http.StatusNoContent)
}

// submitWebForm opens a conversation from a public website form. It sends nothing to the submitter.
// A form that is unknown, off or locked answers one and the same 404.
func (a *API) submitWebForm(response http.ResponseWriter, request *http.Request) {
	if !wellFormedFormName(request) {
		writeFormNotFound(response)
		return
	}
	client := clientAddress(request, a.config.TrustedProxies)
	if a.refuseBusyAddress(response, client) {
		return
	}
	form, open, err := a.openRequestedForm(request)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if !open {
		writeFormNotFound(response)
		return
	}
	header := response.Header()
	header.Add("Vary", "Origin")
	if origin, listed := allowedOrigin(request, form.Origins); listed {
		header.Set("Access-Control-Allow-Origin", origin)
	}
	// Only a form that exists is charged to the address-and-form bucket, so junk names cannot fill it;
	// both addresses of the default form share one bucket.
	if allowed, wait := a.webForms.allow(client+"|"+form.ChannelID, a.now()); !allowed {
		header.Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many submissions. Try again in a minute.")
		return
	}
	// Browsers send application/json across origins only after a preflight, which this form
	// answers for listed origins alone. text/plain would skip that check.
	if media, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err != nil || media != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the form as application/json.")
		return
	}

	var input struct {
		Email   string `json:"email"`
		Message string `json:"message"`
		Source  string `json:"source"`
		Website string `json:"website"`
	}
	// Unknown fields are ignored: a site's form often has inputs this channel does not read.
	if err := json.NewDecoder(io.LimitReader(request.Body, maxWebFormBody)).Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be a JSON object with an email.")
		return
	}
	// The hidden field is for bots: they get the success answer and nothing is stored.
	if strings.TrimSpace(input.Website) != "" {
		writeJSON(response, http.StatusAccepted, map[string]bool{"ok": true})
		return
	}
	submission, fields := validateWebForm(input.Email, input.Message, input.Source)
	if len(fields) > 0 {
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "validation_failed", "message": "Check the highlighted fields.", "fields": fields})})
		return
	}
	err = a.submitToWebForm(request, form, submission)
	switch {
	case errors.Is(err, domain.ErrWebFormClosed), errors.Is(err, channels.ErrChannelOff), errors.Is(err, domain.ErrMailboxClosed):
		header.Del("Access-Control-Allow-Origin")
		writeFormNotFound(response)
	case errors.Is(err, store.ErrRateLimited):
		header.Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many submissions. Try again later.")
	case err != nil:
		writeInternalError(response, err)
	default:
		writeJSON(response, http.StatusAccepted, map[string]bool{"ok": true})
	}
}

// validateWebForm checks the public input and returns the normalized submission, or one message per bad field.
func validateWebForm(emailAddress, message, source string) (store.WebFormSubmission, map[string]string) {
	fields := map[string]string{}
	emailAddress = strings.TrimSpace(emailAddress)
	parsed, err := mail.ParseAddress(emailAddress)
	switch {
	case emailAddress == "":
		fields["email"] = "Enter your email address."
	case err != nil || parsed.Name != "" || parsed.Address != emailAddress || len(emailAddress) > maxWebFormEmail ||
		strings.ContainsAny(emailAddress, "\r\n<>") || !strings.Contains(emailAddress[strings.LastIndex(emailAddress, "@")+1:], "."):
		fields["email"] = "Enter a valid email address."
	}
	message = strings.ReplaceAll(message, "\x00", "")
	if utf8.RuneCountInString(message) > maxWebFormMessage {
		fields["message"] = fmt.Sprintf("Keep the message under %d characters.", maxWebFormMessage)
	}
	// A subject is one line: collapse every run of whitespace, line breaks included.
	source = strings.Join(strings.Fields(strings.ReplaceAll(source, "\x00", "")), " ")
	if utf8.RuneCountInString(source) > maxWebFormSource {
		fields["source"] = fmt.Sprintf("Keep the source under %d characters.", maxWebFormSource)
	}
	if len(fields) > 0 {
		return store.WebFormSubmission{}, fields
	}
	return store.WebFormSubmission{Email: strings.ToLower(emailAddress), Message: strings.TrimSpace(message), Source: source}, nil
}

// submitToWebForm hands a submission to the form's channel. A form turned off, removed or a workspace
// locked since it was opened answers like a form that does not exist.
func (a *API) submitToWebForm(request *http.Request, form store.OpenForm, submission store.WebFormSubmission) error {
	channel, err := a.store.GetChannel(request.Context(), form.WorkspaceID, form.ChannelID)
	if err != nil {
		return err
	}
	if !channel.Live() {
		return domain.ErrWebFormClosed
	}
	body, err := json.Marshal(webform.Submission{Email: submission.Email, Message: submission.Message, Source: submission.Source})
	if err != nil {
		return err
	}
	_, err = a.pipeline.Receive(request.Context(), channel, []channels.Raw{{JSON: body, ReceivedAt: a.now()}})
	return err
}
