package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of saved replies. They count characters, not bytes.
const (
	MaxSavedReplies    = 500
	MaxSavedReplyTitle = 200
	MaxSavedReplyBody  = 20000
	// MaxSavedReplyPage bounds one page of versions or of the trash; DefaultSavedReplyPage is the default.
	MaxSavedReplyPage     = 200
	DefaultSavedReplyPage = 50
	// SavedReplyTrashRetention is how long a deleted reply stays in the trash, restorable, before the
	// server deletes it and its history for good.
	SavedReplyTrashRetention = 30 * 24 * time.Hour
)

// Kinds of saved reply versions.
const (
	SavedReplyBaseline  = "baseline"
	SavedReplyCreated   = "create"
	SavedReplyEdited    = "edit"
	SavedReplyDeleted   = "delete"
	SavedReplyRestored  = "restore"
	SavedReplyUndeleted = "undelete"
)

// SavedReply is text the whole workspace shares and any agent inserts into the reply box. The body
// may hold the variables {{customer.name}} and {{agent.name}}; the web app fills them in.
type SavedReply struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	CreatedByAgentID *string   `json:"created_by_agent_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	// LastChange is the newest version: who made it and through which token or approved request.
	LastChange *SavedReplyChange `json:"last_change"`
}

// TrashedSavedReply is a deleted saved reply, restorable until PurgesAt.
type TrashedSavedReply struct {
	SavedReply
	DeletedAt time.Time         `json:"deleted_at"`
	DeletedBy *SavedReplyPerson `json:"deleted_by"`
	PurgesAt  time.Time         `json:"purges_at"`
}

// SavedReplyPerson names the person behind a change; AgentID is nil once they were removed.
type SavedReplyPerson struct {
	AgentID *string `json:"agent_id"`
	Name    string  `json:"name"`
}

// SavedReplyToken is the agent token that made or asked for a change. The label outlives the token.
type SavedReplyToken struct {
	ID    *string `json:"id"`
	Label string  `json:"label"`
}

// SavedReplyChange is who made one version of a saved reply, and how.
type SavedReplyChange struct {
	Version             int               `json:"version"`
	Kind                string            `json:"kind"`
	RestoredFromVersion *int              `json:"restored_from_version"`
	ChangedBy           *SavedReplyPerson `json:"changed_by"`
	Token               *SavedReplyToken  `json:"token"`
	AgentRequestID      *string           `json:"agent_request_id"`
	CreatedAt           time.Time         `json:"created_at"`
}

// SavedReplyVersion is one version of a saved reply with the title and text it set.
type SavedReplyVersion struct {
	SavedReplyChange
	Title string `json:"title"`
	Body  string `json:"body"`
}

// SavedReplyState is a saved reply as it is now, in or out of the trash.
type SavedReplyState struct {
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

var (
	// ErrDuplicateTitle means another saved reply of the workspace has the same title, ignoring case.
	ErrDuplicateTitle = errors.New("a saved reply with this title already exists")
	// ErrSavedReplyLimit means the workspace already holds MaxSavedReplies saved replies.
	ErrSavedReplyLimit = fmt.Errorf("a workspace holds at most %d saved replies; delete one first", MaxSavedReplies)
	// ErrSavedReplyChanged means the reply was saved by someone else after the client read it.
	ErrSavedReplyChanged = errors.New("this saved reply was changed by someone else; reload to see their version")
	// ErrSavedReplyNotFound means the id names no saved reply in the workspace.
	ErrSavedReplyNotFound = errors.New("saved reply not found")
	// ErrSavedReplyVersionNotFound means the saved reply has no version with that number.
	ErrSavedReplyVersionNotFound = errors.New("saved reply version not found")
)

// InvalidSavedReplyError says which part of a saved reply the API refuses.
type InvalidSavedReplyError struct{ Message string }

func (e *InvalidSavedReplyError) Error() string { return e.Message }

// CleanSavedReplyTitle trims the title and checks its length.
func CleanSavedReplyTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", &InvalidSavedReplyError{"A saved reply needs a title."}
	case utf8.RuneCountInString(title) > MaxSavedReplyTitle:
		return "", &InvalidSavedReplyError{fmt.Sprintf("A title holds at most %d characters.", MaxSavedReplyTitle)}
	}
	return title, nil
}

// CheckSavedReplyBody refuses an empty body or one that is too long. The body is stored as written.
func CheckSavedReplyBody(body string) error {
	switch {
	case strings.TrimSpace(body) == "":
		return &InvalidSavedReplyError{"A saved reply needs text."}
	case utf8.RuneCountInString(body) > MaxSavedReplyBody:
		return &InvalidSavedReplyError{fmt.Sprintf("The text holds at most %d characters.", MaxSavedReplyBody)}
	}
	return nil
}
