package domain

import (
	"errors"
	"time"
)

// MaxHelpCenters bounds the help centers of one workspace, the primary included. It is a safety cap
// on rows and pages, not a plan limit.
const MaxHelpCenters = 20

// MaxHelpCenterName bounds the name the app tells help centers apart by.
const MaxHelpCenterName = 100

var (
	// ErrHelpCenterNotFound is a help center id that is not a live help center of the workspace.
	ErrHelpCenterNotFound = errors.New("no help center with this id")
	// ErrHelpCenterLimit refuses a help center beyond MaxHelpCenters.
	ErrHelpCenterLimit = errors.New("a workspace has at most 20 help centers, the primary one included")
	// ErrPrimaryHelpCenter refuses deleting the primary help center, or giving it a handle.
	ErrPrimaryHelpCenter = errors.New("the primary help center cannot be deleted and has no handle")
	// ErrHelpCenterHandleTaken refuses a handle another live help center of the workspace has.
	ErrHelpCenterHandleTaken = errors.New("another help center of this workspace has this handle")
)

// HelpCenterInUseError refuses deleting a help center that live channels serve; Channels names them.
type HelpCenterInUseError struct {
	Channels []ChannelName
}

func (e *HelpCenterInUseError) Error() string {
	return "live channels serve this help center"
}

// HelpCenterSummary is one help center of a workspace as the list shows it. Handle is nil for the
// primary, which is served at the root of the workspace's address.
type HelpCenterSummary struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Handle          *string    `json:"handle"`
	Primary         bool       `json:"primary"`
	Enabled         bool       `json:"enabled"`
	Title           string     `json:"title"`
	Locale          string     `json:"locale"`
	RequireApproval bool       `json:"require_approval"`
	ShowTags        bool       `json:"show_tags"`
	ContentVersion  int64      `json:"content_version"`
	UpdatedAt       *time.Time `json:"updated_at"`
	// URL is the public address; the API sets it. Hostname is the active custom domain, if any.
	URL      string `json:"url"`
	Hostname string `json:"-"`
}

// NewHelpCenter is a new help center: Name, Handle and Locale are required, Title defaults to empty
// (the pages show the workspace name), Enabled and ShowTags to false.
type NewHelpCenter struct {
	Name, Handle, Title, Locale string
	Enabled, ShowTags           bool
}

// HelpCenterPatch changes the fields that are not nil.
type HelpCenterPatch struct {
	Name, Handle, Title, Locale *string
	Enabled, ShowTags           *bool
}

// CleanHelpCenterName trims a help center's name and bounds it.
func CleanHelpCenterName(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	return CleanKBText("name", name, MaxHelpCenterName)
}

// CleanHelpCenterTitle trims a help center's title and bounds it; "" stays "".
func CleanHelpCenterTitle(title string) (string, error) {
	if title == "" {
		return "", nil
	}
	return CleanKBText("title", title, MaxHelpCenterTitle)
}
