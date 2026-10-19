package domain

import (
	"encoding/json"
	"errors"
	"time"
)

// Limits of saved views. Names count characters.
const (
	MaxSavedViewName = 80
	// MaxSavedViews bounds the personal views of one person and, separately, the shared views of a workspace.
	MaxSavedViews = 50
	// SavedViewTrashRetention is how long a deleted view can be restored before it is gone.
	SavedViewTrashRetention = 30 * 24 * time.Hour
)

// Who sees a saved view.
const (
	SavedViewPersonal = "personal"
	SavedViewShared   = "shared"
)

// SavedView is a named set of inbox filters. Filters is the ConversationFilters object of the
// contract, already checked against the filters GET /conversations accepts.
type SavedView struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Scope        string          `json:"scope"`
	Filters      json.RawMessage `json:"filters"`
	OwnerAgentID *string         `json:"owner_agent_id"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	DeletedAt    *time.Time      `json:"deleted_at"`
	PurgesAt     *time.Time      `json:"purges_at"`
}

var (
	ErrSavedViewNotFound  = errors.New("saved view not found")
	ErrSavedViewNameTaken = errors.New("a saved view with this name already exists")
	ErrSavedViewLimit     = errors.New("at most 50 personal views per person and 50 shared views per workspace")
	// ErrSavedViewChanged: someone saved the view after the caller read it; nothing changed.
	ErrSavedViewChanged = errors.New("saved view changed since it was read")
)
