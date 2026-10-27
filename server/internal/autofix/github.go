package autofix

import (
	"errors"
	"time"
)

// Installation statuses. An installation that reaches every repository of its account stays blocked
// until it is narrowed to selected ones on GitHub; the webhook unblocks it by itself.
const (
	InstallationActive    = "active"
	InstallationBlocked   = "blocked_all_repositories"
	InstallationSuspended = "suspended"
)

// Why a repository is paused: it takes no new fix until the cause goes away.
const (
	PausedBecamePublic          = "became_public"
	PausedRemovedFromApp        = "removed_from_app"
	PausedInstallationSuspended = "installation_suspended"
)

// Errors of the GitHub connection the store and the API share.
var (
	ErrInstallationNotFound  = errors.New("no GitHub installation of this workspace has that id")
	ErrInstallationInUse     = errors.New("this GitHub installation is connected to another workspace")
	ErrInstallationBlocked   = errors.New("the installation reaches every repository of its account; choose Only select repositories on GitHub first")
	ErrInstallationSuspended = errors.New("the installation is suspended on GitHub")
	ErrRepoNotFound          = errors.New("no connected repository of this workspace has that id")
	ErrRepoNotPrivate        = errors.New("only a private repository can be connected: a fix's issue would be public")
)

// InstallationStatus is the status of an installation with this repository selection.
func InstallationStatus(selection string, suspended bool) string {
	switch {
	case suspended:
		return InstallationSuspended
	case selection != "selected":
		return InstallationBlocked
	}
	return InstallationActive
}

// Installation is a GitHub App installation of the workspace as the API shows it.
type Installation struct {
	ID                  string    `json:"id"`
	InstallationID      int64     `json:"installation_id"`
	AccountLogin        string    `json:"account_login"`
	AccountType         string    `json:"account_type"`
	RepositorySelection string    `json:"repository_selection"`
	Status              string    `json:"status"`
	ConnectedByName     string    `json:"connected_by_name"`
	ConnectedAt         time.Time `json:"connected_at"`
	SettingsURL         string    `json:"settings_url"`
}

// Repo is a connected repository as the API shows it.
type Repo struct {
	ID                  string     `json:"id"`
	InstallationID      *int64     `json:"installation_id"`
	GitHubRepoID        int64      `json:"github_repo_id"`
	FullName            string     `json:"full_name"`
	Visibility          string     `json:"visibility"`
	VisibilityCheckedAt time.Time  `json:"visibility_checked_at"`
	DefaultBranch       string     `json:"default_branch"`
	IncludeNotes        bool       `json:"include_notes"`
	DefaultConnector    *Connector `json:"default_connector"`
	PausedReason        *string    `json:"paused_reason"`
	PausedAt            *time.Time `json:"paused_at"`
	ConnectedByName     string     `json:"connected_by_name"`
	ConnectedAt         time.Time  `json:"connected_at"`
}

// GitHubRepository is a repository an installation can reach, read from GitHub, and the connected
// repository it is, if any.
type GitHubRepository struct {
	GitHubRepoID    int64   `json:"github_repo_id"`
	FullName        string  `json:"full_name"`
	Visibility      string  `json:"visibility"`
	ConnectedRepoID *string `json:"connected_repo_id"`
}

// Settings is everything Settings → Autofix shows.
type Settings struct {
	Available      bool               `json:"available"`
	ConsentVersion string             `json:"consent_version"`
	AppSlug        *string            `json:"app_slug"`
	Installations  []Installation     `json:"installations"`
	Repos          []Repo             `json:"repos"`
	Connectors     []ConnectorSetting `json:"connectors"`
}
