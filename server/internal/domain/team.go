package domain

import (
	"errors"
	"time"
)

// TeamMember is an agent as the team list shows it: identity, role, state and activity.
type TeamMember struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Roles       []RoleRef `json:"roles"`
	// Manageable: the caller may change this member (holds team:manage_roles and every permission
	// the member holds); worked out per caller.
	Manageable       bool    `json:"manageable"`
	TimezoneOverride *string `json:"timezone_override"`
	// DeactivatedAt is null for an active agent.
	DeactivatedAt *time.Time `json:"deactivated_at"`
	// LastActiveAt is the last time any session of the agent was used; null when none ever was.
	LastActiveAt *time.Time `json:"last_active_at"`
	// OpenConversations counts the open and pending conversations assigned to the agent.
	OpenConversations int `json:"open_conversations"`
}

// Sentinel errors of agent management; the API maps each to its own code.
var (
	// ErrLastAdmin means the change would leave the workspace without an active admin.
	ErrLastAdmin = errors.New("a workspace needs at least one active member with the Admin role")
	// ErrInvalidReassignTarget means the conversations cannot move to the chosen agent: it is
	// unknown, in another workspace, deactivated, or the agent being deactivated.
	ErrInvalidReassignTarget = errors.New("conversations can only move to another active agent of this workspace")
	// ErrAgentDeactivated means the action needs an active agent but the address belongs to a
	// deactivated one. Reactivate the agent first.
	ErrAgentDeactivated = errors.New("this person is a deactivated agent; reactivate them in the team list")
	// ErrAlreadyMember means an invite names someone who is already a member of the workspace. An invite
	// only lets a new person join; a member's roles change in the team list.
	ErrAlreadyMember = errors.New("this person is already a member of this workspace")
)
