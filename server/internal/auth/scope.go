package auth

import (
	"strings"
	"time"
)

// What sessions.scope holds for an agent token: role (it follows one role of its person) or custom (a
// fixed list). read, draft, send and admin are the tiers of tokens made before roles (legacy).
const (
	ScopeRole   = "role"
	ScopeCustom = "custom"
)

// agentTokenMaxAge is how long after its creation an agent token stops working, however often it is
// used. Every use slides the expiry by the session lifetime, but never past this limit.
const agentTokenMaxAge = 365 * 24 * time.Hour

// InvalidTokenError is a token request that names no valid label, role or permission.
type InvalidTokenError struct{ Reason string }

func (e *InvalidTokenError) Error() string { return e.Reason }

const maxTokenLabelRunes = 80

// TokenRequest asks for a token that follows one of the person's roles (RoleID, or RoleName for the
// CLI) or holds a fixed list of Permissions; exactly one of them. LegacyScope is the CLI's deprecated
// --scope: read, draft and send become that tier's old list, admin the Admin role.
type TokenRequest struct {
	Label       string
	RoleID      string
	RoleName    string
	Permissions []string
	LegacyScope string
}

// checkTokenRequest returns a valid request with its label trimmed and its permissions sorted.
func checkTokenRequest(request TokenRequest) (TokenRequest, error) {
	request.Label = strings.TrimSpace(request.Label)
	if request.Label == "" || len([]rune(request.Label)) > maxTokenLabelRunes {
		return request, &InvalidTokenError{"A token needs a label of 1 to 80 characters."}
	}
	switch request.LegacyScope {
	case "":
	case "read", "draft", "send":
		if request.Permissions != nil || request.RoleID != "" || request.RoleName != "" {
			return request, &InvalidTokenError{"Send a role or a list of permissions, not a scope as well."}
		}
		request.Permissions = NewPermissionSet(legacyPresets[request.LegacyScope]).Sorted()
	case "admin":
		if request.Permissions != nil || request.RoleID != "" || request.RoleName != "" {
			return request, &InvalidTokenError{"Send a role or a list of permissions, not a scope as well."}
		}
		request.RoleName = "Admin"
	default:
		return request, &InvalidTokenError{"Tiers were replaced by roles: send a role or a list of permissions."}
	}
	byRole := request.RoleID != "" || request.RoleName != ""
	switch {
	case byRole && request.Permissions != nil:
		return request, &InvalidTokenError{"Send a role or a list of permissions, not both."}
	case !byRole && request.Permissions == nil:
		return request, &InvalidTokenError{"Send a role the token follows (role_id) or a list of permissions."}
	case request.Permissions != nil:
		permissions, err := cleanPermissions(request.Permissions)
		if err != nil {
			return request, err
		}
		if len(permissions) == 0 {
			return request, &InvalidTokenError{"A token needs at least one permission."}
		}
		request.Permissions = permissions
	}
	return request, nil
}

// legacyAdminTier reports whether a list holds a permission only the old admin tier had: such a token
// was stored with the admin role ceiling, which sessions.role_ceiling still records for a rollback.
func legacyAdminTier(permissions []string) bool {
	send := NewPermissionSet(legacyPresets["send"])
	for _, name := range permissions {
		if !send[name] {
			return true
		}
	}
	return false
}
