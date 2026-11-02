package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func itoa(n int) string { return strconv.Itoa(n) }

// writeRoleError answers the errors of roles and of the rules that guard them, and reports whether err
// was one.
func writeRoleError(response http.ResponseWriter, err error) bool {
	var exceeds *domain.ExceedsYoursError
	var changed *domain.RoleChangedError
	var inUse *domain.RoleInUseError
	var unknown *domain.UnknownRoleNamesError
	var invalid *store.RoleInvalidError
	var missing *auth.MissingPermissionError
	switch {
	case errors.As(err, &exceeds) && exceeds.Member != "":
		writeErrorWith(response, http.StatusForbidden, "member_outranks_you",
			exceeds.Member+" holds permissions you do not hold ("+strings.Join(exceeds.Missing, ", ")+"), so you cannot change them. Ask someone who holds them.",
			map[string]any{"missing_permissions": exceeds.Missing})
	case errors.As(err, &exceeds):
		writeErrorWith(response, http.StatusForbidden, "role_exceeds_yours",
			"You can only give permissions you hold yourself; you do not hold "+strings.Join(exceeds.Missing, ", ")+".",
			map[string]any{"missing_permissions": exceeds.Missing})
	case errors.As(err, &missing):
		writeErrorWith(response, http.StatusForbidden, "permission_missing",
			"Your roles do not include "+missing.Permission+".", map[string]any{"missing_permissions": []string{missing.Permission}, "missing_from": "person"})
	case errors.As(err, &changed):
		message := "This role was changed since you opened it. Reload it and make your change again."
		if changed.UpdatedBy != nil {
			message = "This role was changed by " + *changed.UpdatedBy + " since you opened it. Reload it and make your change again."
		}
		writeErrorWith(response, http.StatusConflict, "role_changed", message, map[string]any{"updated_by": changed.UpdatedBy})
	case errors.As(err, &inUse):
		parts := []string{}
		count := func(names []string, one, many string) {
			if len(names) == 0 {
				return
			}
			word := many
			if len(names) == 1 {
				word = one
			}
			parts = append(parts, itoa(len(names))+" "+word+" ("+strings.Join(names, ", ")+")")
		}
		count(inUse.Members, "person", "people")
		count(inUse.Invites, "pending invite", "pending invites")
		count(inUse.Tokens, "agent token", "agent tokens")
		writeErrorWith(response, http.StatusConflict, "role_in_use",
			"This role is in use by "+strings.Join(parts, ", ")+". Take it off them first: change their roles, revoke the invites and tokens.",
			map[string]any{"in_use": map[string]any{"members": inUse.Members, "invites": inUse.Invites, "tokens": inUse.Tokens}})
	case errors.As(err, &unknown):
		writeError(response, http.StatusBadRequest, "unknown_role",
			"No role is named "+strings.Join(unknown.Unknown, ", ")+". The roles are: "+strings.Join(unknown.Valid, ", ")+".")
	case errors.As(err, &invalid):
		writeErrorWith(response, http.StatusBadRequest, "invalid_request", invalid.Message, map[string]any{"field": invalid.Field})
	case errors.Is(err, domain.ErrRoleNameTaken):
		writeError(response, http.StatusConflict, "role_name_taken", "Another role already has this name. Pick another name.")
	case errors.Is(err, domain.ErrSystemRoleLocked):
		writeError(response, http.StatusConflict, "system_role_locked", "Admin always holds every permission and cannot change; Agent and Admin cannot be deleted, and only Agent can be reset.")
	case errors.Is(err, domain.ErrRoleNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No such role in this workspace.")
	case errors.Is(err, domain.ErrRoleNotYours):
		writeError(response, http.StatusForbidden, "role_not_yours", "A token can only follow one of your own roles. Pick a role you hold, or a list of permissions.")
	case errors.Is(err, domain.ErrNoRoles):
		writeError(response, http.StatusBadRequest, "invalid_request", "Everyone needs at least one role.")
	case errors.Is(err, store.ErrAgentNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No such member in this workspace.")
	default:
		return false
	}
	return true
}
