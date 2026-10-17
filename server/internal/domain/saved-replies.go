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
