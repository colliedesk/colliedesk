package domain

import (
	"errors"
	"time"
)

// The two built-in roles: Admin holds every permission and cannot change; Agent starts with what the
// agent role could do before roles and can be changed, never deleted.
const (
	SystemRoleAdmin = "admin"
	SystemRoleAgent = "agent"
)

// RoleRef names a role. System is "admin" or "agent" for a built-in role, nil for one a workspace made.
type RoleRef struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	System *string `json:"system"`
}

// IsAdmin reports whether the role is the built-in Admin role.
func (r RoleRef) IsAdmin() bool { return r.System != nil && *r.System == SystemRoleAdmin }

// RoleMember is a person holding a role, by name.
type RoleMember struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// RoleToken is a live agent token that follows a role.
type RoleToken struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Owner   string `json:"owner"`
	OwnerID string `json:"owner_id"`
}

// Role is a role as the Roles page and GET /api/v1/roles show it. Permissions is every permission for
// Admin. Editable, Deletable and Assignable are worked out for the caller.
type Role struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	System      *string      `json:"system"`
	Permissions []string     `json:"permissions"`
	Editable    bool         `json:"editable"`
	Deletable   bool         `json:"deletable"`
	Assignable  bool         `json:"assignable"`
	Members     []RoleMember `json:"members"`
	Invites     int          `json:"invites"`
	Tokens      []RoleToken  `json:"tokens"`
	// DefaultPermissions is what the built-in Agent role holds until changed; nil for every other role.
	DefaultPermissions []string   `json:"default_permissions"`
	UpdatedBy          *string    `json:"updated_by"`
	DeletedAt          *time.Time `json:"deleted_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// Ref is the role's name card.
func (r Role) Ref() RoleRef { return RoleRef{ID: r.ID, Name: r.Name, System: r.System} }

// TokenGrant is what an agent token was given. Kind is role (it follows Role), custom (a fixed list) or
// legacy (made before roles; LegacyTier is what it was made as). Permissions is the given list (a role
// token: the role's permissions now); a call gets these limited to what the person holds at that moment.
// RoleHeld is false while the person of a role token does not hold every permission of its role (a
// token may follow a role its person does not hold, as long as they hold all it gives).
type TokenGrant struct {
	Kind        string   `json:"kind"`
	Role        *RoleRef `json:"role"`
	RoleHeld    bool     `json:"role_held"`
	Permissions []string `json:"permissions"`
	LegacyTier  *string  `json:"legacy_tier"`
}

// Kinds of agent tokens.
const (
	TokenKindRole   = "role"
	TokenKindCustom = "custom"
	TokenKindLegacy = "legacy"
	// TokenKindOAuth is an AI app's OAuth grant: it acts with everything its person holds now.
	TokenKindOAuth = "oauth"
)

// Errors of roles; the API maps each to its own code.
var (
	ErrRoleNameTaken    = errors.New("another role has this name")
	ErrSystemRoleLocked = errors.New("this built-in role cannot be changed that way")
	ErrRoleNotFound     = errors.New("no such role")
	ErrNoRoles          = errors.New("everyone needs at least one role")
)

// RoleChangedError: the role changed since the editor loaded it.
type RoleChangedError struct{ UpdatedBy *string }

func (e *RoleChangedError) Error() string { return "the role was changed since you opened it" }

// RoleInUseError: a role still held, invited or followed cannot be deleted.
type RoleInUseError struct {
	Members []string
	Invites []string
	Tokens  []string
}

func (e *RoleInUseError) Error() string { return "the role is in use" }

// ExceedsYoursError: the caller tried to give, set or reach permissions they do not hold. Member is set
// when the target is a member who holds more than the caller (member_outranks_you); otherwise it is a
// role or a list beyond the caller (role_exceeds_yours).
type ExceedsYoursError struct {
	Missing []string
	Member  string
}

func (e *ExceedsYoursError) Error() string {
	if e.Member != "" {
		return e.Member + " holds permissions you do not hold"
	}
	return "it holds permissions you do not hold"
}

// UnknownRoleNamesError: role names that match no role of the workspace; Valid lists the names there are.
type UnknownRoleNamesError struct {
	Unknown []string
	Valid   []string
}

func (e *UnknownRoleNamesError) Error() string { return "unknown role name" }
