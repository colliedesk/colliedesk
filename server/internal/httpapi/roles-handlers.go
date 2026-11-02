package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// replyDraftsPath is where a reply is left for a person to send (a written draft above the reply box).
const replyDraftsPath = "/api/v1/conversations/{id}/reply-drafts"

const (
	createRoleOperation       = "role"
	resetRoleOperation        = "role_reset"
	restoreRoleOperation      = "role_restore"
	createReplyDraftOperation = "reply_draft"
)

func (a *API) listRoles(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	deleted := request.URL.Query().Get("deleted") == "true"
	roles, err := a.store.ListRoles(request.Context(), who.Workspace.ID, callerOf(who), deleted)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": roles})
}

func (a *API) getRole(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	role, err := a.store.GetRole(request.Context(), who.Workspace.ID, callerOf(who), request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, role)
}

// roleBody is the body of a new role or of a change to one.
type roleBody struct {
	Name              *string    `json:"name"`
	Description       *string    `json:"description"`
	Permissions       []string   `json:"permissions"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
}

// readRoleBody decodes a role body and checks its permissions are known ones.
func readRoleBody(response http.ResponseWriter, request *http.Request, creating bool) (roleBody, bool) {
	var input roleBody
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 1<<16))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || (creating && input.ExpectedUpdatedAt != nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send name, description and permissions (resource:action).")
		return input, false
	}
	if creating && (input.Name == nil || input.Permissions == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "A new role needs a name and a list of permissions (it may be empty).")
		return input, false
	}
	if !creating && input.Name == nil && input.Description == nil && input.Permissions == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send at least one of name, description and permissions.")
		return input, false
	}
	if input.Permissions != nil {
		clean, err := auth.CleanPermissions(input.Permissions)
		var invalid *auth.InvalidTokenError
		if errors.As(err, &invalid) {
			writeError(response, http.StatusBadRequest, "invalid_request", invalid.Reason)
			return input, false
		}
		input.Permissions = clean
	}
	return input, true
}

func (input roleBody) store() store.RoleInput {
	return store.RoleInput{Name: input.Name, Description: input.Description, Permissions: input.Permissions, ExpectedUpdatedAt: input.ExpectedUpdatedAt}
}

func (a *API) createRole(response http.ResponseWriter, request *http.Request) {
	input, ok := readRoleBody(response, request, true)
	if !ok {
		return
	}
	who := principal(request)
	key := append([]string{strings.TrimSpace(*input.Name), stringValue(input.Description)}, input.Permissions...)
	a.createOnce(response, request, createRoleOperation, key, func(ctx context.Context) (any, error) {
		role, err := a.store.CreateRole(ctx, who.Workspace.ID, callerOf(who), input.store())
		if err != nil {
			writeStoreError(response, err)
			return nil, errAnswered
		}
		return role, nil
	})
}

func (a *API) updateRole(response http.ResponseWriter, request *http.Request) {
	input, ok := readRoleBody(response, request, false)
	if !ok {
		return
	}
	who := principal(request)
	role, err := a.store.UpdateRole(request.Context(), who.Workspace.ID, callerOf(who), request.PathValue("id"), input.store())
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, role)
}

func (a *API) resetRole(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	a.runOnce(response, request, http.StatusOK, resetRoleOperation, []string{request.PathValue("id")}, func(ctx context.Context) (any, error) {
		role, err := a.store.ResetRole(ctx, who.Workspace.ID, callerOf(who), request.PathValue("id"), auth.AgentRolePermissions())
		if err != nil {
			writeStoreError(response, err)
			return nil, errAnswered
		}
		return role, nil
	})
}

func (a *API) deleteRole(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if err := a.store.DeleteRole(request.Context(), who.Workspace.ID, callerOf(who), request.PathValue("id")); err != nil {
		writeStoreError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreRole(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	a.runOnce(response, request, http.StatusOK, restoreRoleOperation, []string{request.PathValue("id")}, func(ctx context.Context) (any, error) {
		role, err := a.store.RestoreRole(ctx, who.Workspace.ID, callerOf(who), request.PathValue("id"))
		if err != nil {
			writeStoreError(response, err)
			return nil, errAnswered
		}
		return role, nil
	})
}

// setAgentRoles sets the whole list of a member's roles, by ids or by names.
func (a *API) setAgentRoles(response http.ResponseWriter, request *http.Request) {
	var input struct {
		RoleIDs []string `json:"role_ids"`
		Roles   []string `json:"roles"`
	}
	if err := decodeJSON(request, &input); err != nil || (len(input.RoleIDs) > 0) == (len(input.Roles) > 0) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send role_ids or roles (names), not both, with at least one role: everyone needs one.")
		return
	}
	if !uuidShape.MatchString(request.PathValue("id")) || slices.ContainsFunc(input.RoleIDs, func(id string) bool { return !uuidShape.MatchString(id) }) {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
		return
	}
	who := principal(request)
	member, err := a.store.SetAgentRoles(request.Context(), who.Workspace.ID, callerOf(who), request.PathValue("id"), input.RoleIDs, input.Roles)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, member)
}

// createReplyDraft leaves the words of a reply for a person to send: a written draft above the reply box.
// Without messages:send the copies may name only people already on the conversation, so a draft cannot
// slip a new address past the person who sends it.
func (a *API) createReplyDraft(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Text string   `json:"text"`
		CC   []string `json:"cc"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Text) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send text, the reply (Markdown), and cc when it copies someone.")
		return
	}
	who := principal(request)
	conversationID := request.PathValue("id")
	if _, err := a.store.GetConversation(request.Context(), who.Workspace.ID, conversationID); err != nil {
		writeStoreError(response, err)
		return
	}
	cc, refusal := a.cleanCC(request.Context(), who.Workspace.ID, "outgoing", input.CC)
	if refusal != nil {
		a.writeUploadError(response, refusal)
		return
	}
	if len(cc) > 0 && !who.Can(auth.PermMessagesSend) {
		_, known, err := a.store.ConversationParticipants(request.Context(), who.Workspace.ID, conversationID)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		for _, address := range cc {
			if !known[address] {
				writeError(response, http.StatusBadRequest, "invalid_request", address+" is not on this conversation. "+
					"A draft from someone without messages:send may copy only the customer and people already copied; a person can add others when sending.")
				return
			}
		}
	}
	key := append([]string{conversationID, input.Text}, cc...)
	a.createOnce(response, request, createReplyDraftOperation, key, func(ctx context.Context) (any, error) {
		draft, err := a.ai.CreateWrittenDraft(ctx, who.Workspace.ID, aiActor(request), conversationID, input.Text, cc)
		if err != nil {
			writeAIError(response, err)
			return nil, errAnswered
		}
		return draft, nil
	})
}

// agentRequestsRemoved answers the workspace realm of the two decision routes that remain for the
// operator console.
func (a *API) agentRequestsRemoved(response http.ResponseWriter, _ *http.Request) {
	writeError(response, http.StatusGone, "agent_requests_removed",
		"Agent requests were removed: an agent token acts at once when it holds the permission, and is refused (permission_missing) when it does not.")
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
