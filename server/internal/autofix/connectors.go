package autofix

import (
	"context"
	"errors"
	"time"
)

// ConsentVersion names the data consent text an admin agrees to when turning a coding agent on (what
// leaves Colliedesk, where it goes, how long the vendor keeps it). A new text gets a new version, and an
// agent turned on with an older one asks again.
const ConsentVersion = "2026-10-27"

// Errors of the connector settings.
var (
	ErrConsentOutdated  = errors.New("the consent version is not the current text")
	ErrCursorKeyMissing = errors.New("no Cursor key is set for this workspace")
	// ErrRepoUnreachable: the App no longer reaches the repository (removed from the installation, the
	// installation suspended or gone, the repository paused or deleted).
	ErrRepoUnreachable = errors.New("the GitHub App no longer reaches this repository")
)

// Check results kept on a connector (last_check.result).
const (
	CheckOK                   = "ok"
	CheckKeyInvalid           = "key_invalid"
	CheckNoRepoAccess         = "no_repo_access"
	CheckInstallationInactive = "installation_inactive"
	CheckRepoNotPrivate       = "repo_not_private"
	CheckProviderUnavailable  = "provider_unavailable"
)

// ConnectorSetting is a coding agent of the workspace as the settings show it: never a character of a
// key, only who set it and when.
type ConnectorSetting struct {
	Kind    Connector `json:"kind"`
	Enabled bool      `json:"enabled"`
	// Mode is Copilot's way of being assigned (assign_api, assign_on_github); nil for the others.
	Mode       *string           `json:"mode"`
	Consent    *ConnectorConsent `json:"consent"`
	Credential *ConnectorKey     `json:"credential"`
	LastCheck  *ConnectorCheck   `json:"last_check"`
}

// ConnectorConsent is the data consent on record.
type ConnectorConsent struct {
	Version string    `json:"version"`
	Name    string    `json:"name"`
	At      time.Time `json:"at"`
}

// ConnectorKey says a key is set, by whom and when.
type ConnectorKey struct {
	SetByName *string   `json:"set_by_name"`
	SetAt     time.Time `json:"set_at"`
}

// ConnectorCheck is the last check's result.
type ConnectorCheck struct {
	At      time.Time `json:"at"`
	Result  string    `json:"result"`
	Message *string   `json:"message"`
}

// RepoCheck is what GitHub says of a connected repository now (never a cached answer).
type RepoCheck struct {
	Visibility    string
	FullName      string
	DefaultBranch string
}

// RepoChecker reads a connected repository on GitHub with the installation's token, right before a fix
// leaves for it. A repository that is no longer private is paused by the checker (became_public) and
// answered with its real visibility; one the App cannot reach any more answers ErrRepoUnreachable. The
// GitHub connection implements it; the worker only reads the answer.
type RepoChecker interface {
	CheckRepo(ctx context.Context, workspaceID, repoID string) (RepoCheck, error)
}
