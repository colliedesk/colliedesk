package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// createInvite invites someone with roles, for a person or an agent token holding team:invite and every
// permission of those roles. The one-time link is in this answer only.
func (a *API) createInvite(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Email       string   `json:"email"`
		RoleIDs     []string `json:"role_ids"`
		Roles       []string `json:"roles"`
		RoleCeiling *string  `json:"role_ceiling"`
	}
	if err := decodeJSON(request, &input); err != nil || strings.TrimSpace(input.Email) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "An email is required, with role_ids or roles (names) when the invite gives more than the Agent role.")
		return
	}
	if input.RoleCeiling != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "role_ceiling was replaced by roles: send role_ids, or roles by name, for example [\"Agent\"].")
		return
	}
	if len(input.RoleIDs) > 0 && len(input.Roles) > 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send role_ids or roles (names), not both.")
		return
	}
	key := append([]string{input.Email, "ids"}, slices.Sorted(slices.Values(input.RoleIDs))...)
	key = append(append(key, "names"), slices.Sorted(slices.Values(input.Roles))...)
	// A repeat of one press (the same Idempotency-Key) answers the same invite without its link: the link is
	// shown once and stored only as a hash.
	a.createOnce(response, request, createInviteOperation, key, func(ctx context.Context) (any, error) {
		invite, link, err := a.auth.CreateInvite(ctx, principal(request), input.Email, input.RoleIDs, input.Roles)
		if errors.Is(err, auth.ErrInvalidInviteEmail) {
			writeError(response, http.StatusBadRequest, "invalid_request", "An invite needs an email address.")
			return nil, errAnswered
		}
		if err != nil {
			writeStoreError(response, err)
			return nil, errAnswered
		}
		a.recordFunnel("teammate_invited", principal(request).Workspace.ID, nil)
		return shownOnce{shown: map[string]any{"invite": invite, "url": link}, kept: map[string]any{"invite": invite}}, nil
	})
}

func (a *API) listInvites(response http.ResponseWriter, request *http.Request) {
	items, err := a.auth.ListInvites(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) revokeInvite(response http.ResponseWriter, request *http.Request) {
	ok, err := a.auth.RevokeInvite(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	if !ok {
		writeError(response, http.StatusNotFound, "not_found", "The invite was not found.")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}
