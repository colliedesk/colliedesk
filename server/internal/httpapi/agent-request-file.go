package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// filesNeedApprovalMessage answers a call with files that would have to wait for a person: the
// files would sit in storage for days, so they are not kept. The way forward is in the message.
const filesNeedApprovalMessage = "This needs a person's approval, and a call with files cannot wait for one. " +
	"Send the text without files (as JSON): it becomes a request a person approves. Then attach the files after approval, in a new message."

func writeFilesNeedApproval(response http.ResponseWriter) {
	writeError(response, http.StatusForbidden, "files_need_approval", filesNeedApprovalMessage)
}

// conversationRoutePrefix marks the routes that act on one conversation; their requests name it.
const conversationRoutePrefix = "/api/v1/conversations/{id}"

// fileAgentRequest stores the call of an agent token as a request a person approves and answers 202.
// body is the call's JSON; nil reads it from the request, which must then be JSON or empty. summary,
// when not empty, replaces the one built from the route and body.
func (a *API) fileAgentRequest(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal, kind string, body json.RawMessage, summary string) {
	// A locked workspace cannot approve anything, so nothing is filed that would only expire.
	if a.limits && who.Access.Locked {
		writeLocked(response)
		return
	}
	if body == nil {
		if mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err == nil && mediaType == "multipart/form-data" {
			writeFilesNeedApproval(response)
			return
		}
		// An in-process call (an MCP tool) without a body has no Body at all.
		var source io.Reader = http.NoBody
		if request.Body != nil {
			source = request.Body
		}
		raw, err := io.ReadAll(io.LimitReader(source, domain.MaxAgentRequestBody+1))
		if err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "The request body could not be read.")
			return
		}
		raw = bytes.TrimSpace(raw)
		switch {
		case len(raw) > domain.MaxAgentRequestBody:
			writeError(response, http.StatusRequestEntityTooLarge, "too_large", "A call that waits for approval can carry at most 64 KiB of JSON.")
			return
		case len(raw) > 0 && !json.Valid(raw):
			writeError(response, http.StatusBadRequest, "invalid_request", "The request body must be JSON.")
			return
		}
		body = raw
	}
	if problem := settingsBodyProblem(route, body); problem != "" {
		writeError(response, http.StatusBadRequest, "invalid_request", problem)
		return
	}
	var conversationID *string
	if strings.HasPrefix(route.Pattern, conversationRoutePrefix) {
		id := request.PathValue("id")
		// A request about another workspace's conversation is refused now, not when a person approves it.
		if _, err := a.store.GetConversation(request.Context(), who.Workspace.ID, id); err != nil {
			writeStoreError(response, err)
			return
		}
		conversationID = &id
	}
	var savedReply *store.SavedReplySnapshot
	if strings.HasPrefix(route.Pattern, savedReplyRoutePrefix) {
		snapshot, ok := a.savedReplyBefore(response, request, route, who)
		if !ok {
			return
		}
		savedReply = &snapshot
	}
	if summary == "" && (savedReply != nil || route.Pattern == "/api/v1/saved-replies") {
		summary = summarizeSavedReplyRequest(route, body, savedReply, request.PathValue("version"))
	}
	if summary == "" {
		summary = a.summarizeSettingsRequest(request.Context(), request, route, who, body)
	}
	if summary == "" {
		summary = summarizeRequest(route, kind, body)
	}
	path := request.URL.EscapedPath()
	if request.URL.RawQuery != "" {
		path += "?" + request.URL.RawQuery
	}
	created, err := a.store.CreateAgentRequest(request.Context(), who.Workspace.ID, store.NewAgentRequest{
		TokenID: who.SessionID, AgentID: who.Agent.ID, Kind: kind, Method: route.Method, Route: route.Pattern, Path: path,
		Body: body, Summary: clip(summary), ConversationID: conversationID, SavedReply: savedReply,
	})
	var rate *domain.AgentRequestRateError
	if errors.As(err, &rate) {
		seconds := int((rate.RetryAfter + time.Second - 1) / time.Second)
		response.Header().Set("Retry-After", strconv.Itoa(max(seconds, 1)))
		writeError(response, http.StatusTooManyRequests, "too_many_requests",
			fmt.Sprintf("This token filed %d requests in the last hour. Try again in %d seconds.", domain.MaxAgentRequestsPerHour, max(seconds, 1)))
		return
	}
	if errors.Is(err, domain.ErrTooManyAgentRequests) {
		writeError(response, http.StatusTooManyRequests, "too_many_requests", domain.ErrTooManyAgentRequests.Error()+".")
		return
	}
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusAccepted, map[string]any{
		"request_id": created.ID, "kind": created.Kind, "status": created.Status, "summary": created.Summary,
		"expires_at": created.ExpiresAt.Format(time.RFC3339),
		"message":    "A person must approve this before it happens. Nothing has changed yet. Read the outcome with GET /api/v1/agent-requests/" + created.ID + ".",
	})
}

// requestTitles names, for a person deciding, what each requestable route does.
var requestTitles = map[string]string{
	"PATCH /api/v1/conversations":                                "Change several conversations",
	"POST /api/v1/conversations":                                 "Open a conversation",
	"PATCH /api/v1/conversations/{id}":                           "Change a conversation",
	"PUT /api/v1/conversations/{id}/tags/{tagId}":                "Tag a conversation",
	"DELETE /api/v1/conversations/{id}/tags/{tagId}":             "Remove a tag from a conversation",
	"POST /api/v1/conversations/{id}/messages":                   "Add a private note",
	"POST /api/v1/conversations/{id}/messages/{messageId}/retry": "Send a failed reply again",
	"POST /api/v1/contacts":                                      "Add a customer",
	"POST /api/v1/tags":                                          "Create a tag",
	"PATCH /api/v1/tags/{id}":                                    "Rename a tag",
	"DELETE /api/v1/tags/{id}":                                   "Delete a tag",
	"POST /api/v1/agent-requests/{id}/cancel":                    "Cancel an agent request",
	"POST /api/v1/auth/tokens":                                   "Create an agent token",
	"GET /api/v1/export":                                         "Export all workspace data",
	"POST /api/v1/export/link":                                   "Export all workspace data",
	"POST /api/v1/billing/checkout":                              "Start a plan checkout",
	"POST /api/v1/billing/portal":                                "Open the billing portal",
	"DELETE /api/v1/channels/email":                              "Disconnect the email channel",
	"POST /api/v1/channels/email/secret":                         "Rotate the inbound email secret",
	"PATCH /api/v1/workspace/slug":                               "Change the workspace address",
	"PATCH /api/v1/workspace/timezone":                           "Change the workspace timezone",
	"PATCH /api/v1/agents/me/timezone":                           "Change my timezone",
	"PATCH /api/v1/agents/{id}":                                  "Change a team member",
	"POST /api/v1/auth/invites":                                  "Invite a team member",
	"DELETE /api/v1/auth/invites/{id}":                           "Revoke an invite",
	"DELETE /api/v1/auth/sessions/{id}":                          "Revoke a session",
	"PUT /api/v1/channels/web-form":                              "Change the web form",
	"PUT /api/v1/channels/email":                                 "Change the email channel",
	"POST /api/v1/channels/email/domain-verification":            "Verify the email domain",
	"GET /api/v1/billing":                                        "Read the plan and billing status",
	"POST /api/v1/saved-replies":                                 "Create a saved reply",
	"PATCH /api/v1/saved-replies/{id}":                           "Edit a saved reply",
	"DELETE /api/v1/saved-replies/{id}":                          "Delete a saved reply",
	"POST /api/v1/saved-replies/{id}/restore":                    "Restore a saved reply from the trash",
	"POST /api/v1/saved-replies/{id}/versions/{version}/restore": "Restore an earlier version of a saved reply",
}

const summaryDetail = 300

// summarizeRequest is the one line a person reads before deciding: what the call does and what it
// sets. It holds only what the token sent, never text a customer wrote.
func summarizeRequest(route Route, kind string, body json.RawMessage) string {
	title, ok := requestTitles[route.Method+" "+route.Pattern]
	if !ok {
		title = route.Method + " " + route.Pattern
	}
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	if kind == domain.AgentRequestReplyDraft {
		title = "Reply to the customer"
	}
	if text, ok := fields["body"].(string); ok && route.Pattern == conversationRoutePrefix+"/messages" {
		return clip(title + ": " + strings.Join(strings.Fields(text), " "))
	}
	var details []string
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		encoded, err := json.Marshal(fields[key])
		if err != nil {
			continue
		}
		details = append(details, key+" "+strings.Trim(string(encoded), `"`))
	}
	if len(details) == 0 {
		return title
	}
	return clip(title + ": " + strings.Join(details, ", "))
}

func clip(text string) string {
	runes := []rune(text)
	if len(runes) <= summaryDetail {
		return text
	}
	return string(runes[:summaryDetail-1]) + "…"
}
