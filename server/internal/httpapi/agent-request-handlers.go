package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A token sees only the requests it made; a person sees the workspace's.
func requestScope(request *http.Request) string {
	if who := principal(request); who.Scope != "" {
		return who.SessionID
	}
	return ""
}

func (a *API) listAgentRequests(response http.ResponseWriter, request *http.Request) {
	status := request.URL.Query().Get("status")
	if status != "" && !slices.Contains(domain.AgentRequestStatuses, status) {
		writeError(response, http.StatusBadRequest, "invalid_request", "status must be one of: "+strings.Join(domain.AgentRequestStatuses, ", ")+".")
		return
	}
	who := principal(request)
	full := request.URL.Query().Get("detail") == "full"
	items, err := a.store.ListAgentRequests(request.Context(), who.Workspace.ID, store.AgentRequestFilter{Status: status, TokenID: requestScope(request), Full: full})
	if err != nil {
		writeInternalError(response, err)
		return
	}
	for index := range items {
		items[index].ApprovableBy = approvableBy(items[index])
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) getAgentRequest(response http.ResponseWriter, request *http.Request) {
	item, err := a.store.GetAgentRequest(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), requestScope(request))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	item.ApprovableBy = approvableBy(item)
	writeJSON(response, http.StatusOK, item)
}

// approveTimeout bounds one approval, call included. It does not follow the approver's connection: a
// closed tab must not leave a request claimed with no result.
const approveTimeout = 2 * time.Minute

// approveAgentRequest checks the person may run the call, marks the request approved, then runs the
// call as that person and records what it answered. The answer goes back to the approver whole; the
// stored copy leaves out credentials and links (withheldResults).
func (a *API) approveAgentRequest(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Body *string   `json:"body"`
		CC   *[]string `json:"cc"`
	}
	if !decodeOptionalJSON(response, request, &input) {
		return
	}
	who := principal(request)
	workspaceID, id := who.Workspace.ID, request.PathValue("id")
	ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), approveTimeout)
	defer cancel()
	existing, err := a.store.GetAgentRequest(ctx, workspaceID, id, "")
	if err != nil {
		writeStoreError(response, err)
		return
	}
	// Refused before anything changes, so a person who may approve it still can.
	if !auth.HasPermission(who.Agent.Role, approvalPermission(existing)) {
		writeError(response, http.StatusForbidden, "forbidden", "Only an admin can approve this request. It keeps waiting for one.")
		return
	}
	var edited json.RawMessage
	if input.Body != nil || input.CC != nil {
		if edited, err = editedReply(existing, input.Body, input.CC); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	claimed, err := a.store.ClaimAgentRequest(ctx, workspaceID, id, who.Agent.ID, edited)
	if writeDecisionError(response, err) {
		return
	}
	status, answer, complete := a.replayApproved(ctx, who, claimed)
	stored, note := json.RawMessage(nil), (*string)(nil)
	switch {
	case !complete:
		note = new("The answer was larger than 64 KiB, so it was not kept.")
		answer = nil
	case withheldResults[claimed.Method+" "+claimed.Route] && status < 300:
		note = new("The answer held a credential or a link. It was shown once to the person who approved, and not kept.")
	case json.Valid(answer):
		stored = answer
	}
	finished, err := a.store.FinishAgentRequest(ctx, workspaceID, id, status, stored, note)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	finished.ApprovableBy = approvableBy(finished)
	var result json.RawMessage
	if json.Valid(answer) {
		result = answer
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, map[string]any{"request": finished, "result": result})
}

func (a *API) rejectAgentRequest(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Reason *string `json:"reason"`
	}
	if !decodeOptionalJSON(response, request, &input) {
		return
	}
	if input.Reason != nil {
		reason := strings.TrimSpace(*input.Reason)
		if len([]rune(reason)) > 1000 {
			writeError(response, http.StatusBadRequest, "invalid_request", "A reason can be at most 1000 characters.")
			return
		}
		input.Reason = &reason
		if reason == "" {
			input.Reason = nil
		}
	}
	who := principal(request)
	item, err := a.store.RejectAgentRequest(request.Context(), who.Workspace.ID, request.PathValue("id"), who.Agent.ID, input.Reason)
	if writeDecisionError(response, err) {
		return
	}
	item.ApprovableBy = approvableBy(item)
	writeJSON(response, http.StatusOK, item)
}

func (a *API) cancelAgentRequest(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	item, err := a.store.CancelAgentRequest(request.Context(), who.Workspace.ID, request.PathValue("id"), who.Agent.ID, requestScope(request))
	if writeDecisionError(response, err) {
		return
	}
	item.ApprovableBy = approvableBy(item)
	writeJSON(response, http.StatusOK, item)
}

// writeDecisionError answers a refused or failed decision and reports whether it did.
func writeDecisionError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, domain.ErrAgentRequestDecided):
		writeError(response, http.StatusConflict, "already_decided", "This request was already approved, rejected, cancelled or expired. A decided request never runs again; ask the agent to make a new one.")
	case errors.Is(err, domain.ErrAgentRequestTokenRevoked):
		writeError(response, http.StatusConflict, "token_revoked", "The token that asked was revoked, so its request can only be rejected.")
	case errors.Is(err, domain.ErrNotRequestOwner):
		writeError(response, http.StatusForbidden, "forbidden", "Only the token that asked, or the person who owns it, can cancel a request. Reject it instead.")
	default:
		writeStoreError(response, err)
	}
	return true
}

// editedReply returns the call of a reply draft as the approver edited it: text replaced, copies
// removed. The approval can only take copies away: an address the token did not ask for, and that no
// one saw in the request, is never added here.
func editedReply(item domain.AgentRequest, text *string, cc *[]string) (json.RawMessage, error) {
	if item.Kind != domain.AgentRequestReplyDraft {
		return nil, errors.New("Only a reply draft can be edited before approval; approve or reject this request as it is.")
	}
	var call map[string]any
	if err := json.Unmarshal(item.Body, &call); err != nil || call == nil {
		return nil, errors.New("This draft cannot be edited.")
	}
	if text != nil {
		if strings.TrimSpace(*text) == "" {
			return nil, errors.New("The reply is empty. Discard the draft instead.")
		}
		call["body"] = *text
	}
	if cc != nil {
		asked := map[string]bool{}
		if listed, ok := call["cc"].([]any); ok {
			for _, address := range listed {
				if value, ok := address.(string); ok {
					asked[strings.ToLower(value)] = true
				}
			}
		}
		kept := make([]string, 0, len(*cc))
		for _, address := range *cc {
			if !asked[strings.ToLower(address)] {
				return nil, errors.New(address + " was not copied on the draft. Approving can remove copies, not add them; add the address in a new reply.")
			}
			kept = append(kept, strings.ToLower(address))
		}
		delete(call, "cc")
		if len(kept) > 0 {
			call["cc"] = kept
		}
	}
	return json.Marshal(call)
}

// decodeOptionalJSON decodes a body that may be absent; it answers 400 for one that is not valid.
func decodeOptionalJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	if request.ContentLength == 0 {
		return true
	}
	if err := decodeJSON(request, target); err != nil && !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_request", "The request body must be a JSON object with the documented fields.")
		return false
	}
	return true
}
