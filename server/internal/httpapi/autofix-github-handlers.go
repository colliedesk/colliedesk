package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	autofixCallbackPath = "/api/v1/autofix/github/callback"
	githubWebhookPath   = "/api/v1/webhooks/github"
	// githubAuthorizationTTL is how long an installation or a fix's authorization started in Colliedesk can
	// be finished on GitHub.
	githubAuthorizationTTL = 10 * time.Minute
	// githubCallTimeout bounds one call to GitHub made while a person waits.
	githubCallTimeout = 20 * time.Second
	// autofixSettingsPage is where the browser comes back after installing the App.
	autofixSettingsPage = "/settings/autofix"
)

// writeGitHubError answers the errors of the GitHub connection; anything else goes to writeAutofixError.
func writeGitHubError(response http.ResponseWriter, err error) {
	var apiErr *github.APIError
	switch {
	case errors.Is(err, autofix.ErrInstallationNotFound):
		writeError(response, http.StatusNotFound, "installation_not_found", autofix.ErrInstallationNotFound.Error()+".")
	case errors.Is(err, autofix.ErrRepoNotFound):
		writeError(response, http.StatusNotFound, "repo_not_found", autofix.ErrRepoNotFound.Error()+".")
	case errors.Is(err, autofix.ErrInstallationBlocked):
		writeError(response, http.StatusConflict, "installation_blocked", "The installation reaches every repository of its account. Change it to Only select repositories on GitHub, then connect the repository.")
	case errors.Is(err, autofix.ErrInstallationSuspended):
		writeError(response, http.StatusConflict, "installation_suspended", "The installation is suspended on GitHub. Unsuspend it there first.")
	case errors.Is(err, autofix.ErrRepoNotPrivate):
		writeError(response, http.StatusConflict, "repo_not_private", "Only a private repository can be connected: the issue of a fix would be public.")
	case errors.Is(err, github.ErrNotFound):
		writeError(response, http.StatusNotFound, "repo_not_found", "The installation cannot reach that repository. Add it to the installation on GitHub first.")
	case errors.Is(err, github.ErrSuspended):
		writeError(response, http.StatusConflict, "installation_suspended", "The installation is suspended on GitHub. Unsuspend it there first.")
	case errors.As(err, &apiErr), errors.Is(err, context.DeadlineExceeded):
		slog.Warn("autofix: GitHub call failed", "error", err)
		writeError(response, http.StatusServiceUnavailable, "github_unavailable", "GitHub did not answer. Nothing was changed; try again.")
	default:
		writeAutofixError(response, err)
	}
}

// requireGitHub answers 503 autofix_unavailable while this server's GitHub App is not set up.
func (a *API) requireGitHub(response http.ResponseWriter) bool {
	if a.githubSync != nil {
		return true
	}
	writeError(response, http.StatusServiceUnavailable, "autofix_unavailable", "Autofix is not set up on this server. GET /api/v1/config names the settings the administrator must add.")
	return false
}

// actorOf is who makes a change: the person (the approver, for an approved agent request), or the agent
// token's owner.
func actorOf(who auth.Principal) autofix.Actor {
	name := who.Agent.DisplayName
	if name == "" {
		name = who.Agent.Email
	}
	actor := autofix.Actor{Kind: autofix.ActorPerson, ID: who.Agent.ID, Name: name}
	if who.IsToken() {
		actor.Kind = autofix.ActorToken
	}
	return actor
}

func (a *API) autofixSettings(ctx context.Context, workspaceID string) (autofix.Settings, error) {
	settings, err := a.store.AutofixSettings(ctx, workspaceID)
	if err != nil {
		return settings, err
	}
	settings.Available = a.githubSync != nil
	if a.config.GitHub.Slug != "" {
		slug := a.config.GitHub.Slug
		settings.AppSlug = &slug
	}
	for i, installation := range settings.Installations {
		settings.Installations[i].SettingsURL = github.InstallationSettingsURL(installation.AccountLogin, installation.AccountType, installation.InstallationID)
	}
	return settings, nil
}

func (a *API) getAutofixSettings(response http.ResponseWriter, request *http.Request) {
	settings, err := a.autofixSettings(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, settings)
}

// connectGitHub starts installing the App: it records who started it and answers the install address.
func (a *API) connectGitHub(response http.ResponseWriter, request *http.Request) {
	if !a.requireGitHub(response) {
		return
	}
	who := principal(request)
	now := a.now()
	started, err := a.store.CreateAutofixOAuthState(request.Context(), who.Workspace.ID, who.Agent.ID, "install", "", now, now.Add(githubAuthorizationTTL))
	if err != nil {
		writeInternalError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]string{"authorize_url": a.githubSync.App().InstallURL(started.State)})
}

// finishGitHubAuthorization is where GitHub sends the browser back, after installing the App or
// authorizing a fix. The route is public so that GitHub's redirect reaches it, but it finishes only for
// the signed-in person who started it in this workspace: otherwise anyone could send a victim a link that
// attaches an installation to the sender's workspace. Every outcome is a redirect the page explains;
// GitHub's own error text is never echoed.
func (a *API) finishGitHubAuthorization(response http.ResponseWriter, request *http.Request) {
	page, outcome := autofixSettingsPage, a.finishGitHubInstall(request)
	key := "github"
	if outcome != "connected" && outcome != "requested" {
		key = "github_error"
	}
	http.Redirect(response, request, page+"?"+key+"="+outcome, http.StatusSeeOther)
}

func (a *API) finishGitHubInstall(request *http.Request) string {
	if a.githubSync == nil {
		return "github_unavailable"
	}
	cookie, err := request.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return "signed_out"
	}
	who, err := a.auth.Authenticate(request.Context(), cookie.Value, true)
	if err != nil || who.IsToken() {
		return "signed_out"
	}
	query := request.URL.Query()
	state := query.Get("state")
	if state == "" {
		if query.Get("setup_action") == "request" {
			// An organization member asked its owners to install the App: nothing to attach yet.
			return "requested"
		}
		return "expired"
	}
	// The state is spent before anything else, so a refused or failed authorization cannot be replayed.
	started, err := a.store.TakeAutofixOAuthState(request.Context(), who.Workspace.ID, state, a.now())
	if errors.Is(err, store.ErrOAuthStateNotFound) {
		return "expired"
	}
	if err != nil {
		slog.Error("autofix: GitHub authorization state", "error", err, "workspace", who.Workspace.ID)
		return "github_unavailable"
	}
	if started.AgentID != who.Agent.ID {
		return "wrong_person"
	}
	if started.Purpose != "install" {
		// Sending a fix as the person comes with the issue connectors.
		return "expired"
	}
	if !auth.HasPermission(who.Agent.Role, auth.PermissionAccessWrite) || (a.limits && who.Access.Locked) {
		return "forbidden"
	}
	if query.Get("setup_action") == "request" {
		return "requested"
	}
	installationID, err := strconv.ParseInt(query.Get("installation_id"), 10, 64)
	if query.Get("error") != "" || query.Get("code") == "" || err != nil || installationID <= 0 {
		return "denied"
	}
	ctx, cancel := context.WithTimeout(request.Context(), githubCallTimeout)
	defer cancel()
	app := a.githubSync.App()
	token, err := app.Exchange(ctx, query.Get("code"), "", a.config.Origin+autofixCallbackPath)
	if errors.Is(err, github.ErrCodeRefused) {
		return "denied"
	}
	if err != nil {
		slog.Warn("autofix: GitHub code exchange failed", "error", err, "workspace", who.Workspace.ID)
		return "github_unavailable"
	}
	// The person's token proves the installation is theirs, then it is revoked at once: it is never
	// stored, and the redirect's installation_id alone could be forged.
	mine, theirs, err := a.installationOfPerson(ctx, token, installationID)
	a.revokeGitHubToken(token, who.Workspace.ID)
	if err != nil {
		slog.Warn("autofix: check the person's GitHub installation", "error", err, "workspace", who.Workspace.ID)
		return "github_unavailable"
	}
	if !theirs {
		return "installation_not_yours"
	}
	actor := actorOf(who)
	if _, err := a.store.SaveGitHubInstallation(request.Context(), who.Workspace.ID, store.NewGitHubInstallation{InstallationID: mine.ID,
		AccountLogin: mine.Account.Login, AccountType: mine.Account.Type, RepositorySelection: mine.RepositorySelection, Suspended: mine.SuspendedAt != nil,
		AgentID: actor.ID, AgentName: actor.Name}); errors.Is(err, autofix.ErrInstallationInUse) {
		return "installation_in_use"
	} else if err != nil {
		slog.Error("autofix: save the GitHub installation", "error", err, "workspace", who.Workspace.ID)
		return "github_unavailable"
	}
	// Repositories connected before the App was removed come back with it; best effort (logged), the
	// reconcile pass does it too.
	a.githubSync.ResumeRepos(ctx, who.Workspace.ID, mine.ID)
	return "connected"
}

// installationOfPerson reports whether the person behind userToken may connect the installation. GitHub
// lists an organization's installation to every member who reaches one of its repositories, so the list
// alone is not enough: the person must administer every repository the installation reaches (as the App
// sees them, compared with what the person sees). An organization owner does; an installation with no
// repository proves nothing. A list too long to read refuses too.
func (a *API) installationOfPerson(ctx context.Context, userToken string, installationID int64) (*github.Installation, bool, error) {
	app := a.githubSync.App()
	installations, err := app.UserInstallations(ctx, userToken)
	if errors.Is(err, github.ErrTooMany) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var mine *github.Installation
	for i := range installations {
		if installations[i].ID == installationID {
			mine = &installations[i]
		}
	}
	if mine == nil {
		return nil, false, nil
	}
	administered, err := app.UserInstallationRepositories(ctx, userToken, installationID)
	if errors.Is(err, github.ErrTooMany) || errors.Is(err, github.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	admin := map[int64]bool{}
	for _, repo := range administered {
		admin[repo.ID] = repo.Permissions.Admin
	}
	reached, err := app.Repositories(ctx, installationID)
	if errors.Is(err, github.ErrTooMany) {
		return nil, false, nil
	}
	if err != nil {
		// A suspended installation reads nothing as the App, so it cannot be checked: unsuspend it first.
		return nil, false, err
	}
	if len(reached) == 0 {
		return nil, false, nil
	}
	for _, repo := range reached {
		if !admin[repo.ID] {
			return nil, false, nil
		}
	}
	return mine, true, nil
}

// revokeGitHubToken deletes a person's GitHub token; best effort, logged without the token (it expires
// within eight hours anyway).
func (a *API) revokeGitHubToken(token, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), githubCallTimeout)
	defer cancel()
	if err := a.githubSync.App().Revoke(ctx, token); err != nil {
		slog.Warn("autofix: revoke a GitHub user token", "error", err, "workspace", workspaceID)
	}
}

// disconnectGitHub uninstalls the App from the account on GitHub, then removes the installation; its
// repositories stay, paused, and their fixes in progress are cancelled.
func (a *API) disconnectGitHub(response http.ResponseWriter, request *http.Request) {
	if !a.requireGitHub(response) {
		return
	}
	who := principal(request)
	installation, err := a.store.GetGitHubInstallation(request.Context(), who.Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), githubCallTimeout)
	defer cancel()
	if err := a.githubSync.App().Uninstall(ctx, installation.InstallationID); err != nil {
		writeGitHubError(response, err)
		return
	}
	removed, err := a.store.RemoveGitHubInstallation(request.Context(), who.Workspace.ID, installation.ID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	githubsync.EndFixes(request.Context(), a.store, who.Workspace.ID, removed.FixIDs, autofix.Cancelled, "repo_disconnected", actorOf(who))
	a.writeAutofixSettings(response, request)
}

func (a *API) writeAutofixSettings(response http.ResponseWriter, request *http.Request) {
	settings, err := a.autofixSettings(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, settings)
}

// listGitHubRepositories reads, from GitHub now, the repositories the installation can reach.
func (a *API) listGitHubRepositories(response http.ResponseWriter, request *http.Request) {
	if !a.requireGitHub(response) {
		return
	}
	workspaceID := principal(request).Workspace.ID
	installation, err := a.store.GetGitHubInstallation(request.Context(), workspaceID, request.PathValue("id"))
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	if installation.Status == autofix.InstallationSuspended {
		writeGitHubError(response, autofix.ErrInstallationSuspended)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), githubCallTimeout)
	defer cancel()
	repos, err := a.githubSync.App().Repositories(ctx, installation.InstallationID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	connected, err := a.store.ConnectedRepoIDs(request.Context(), workspaceID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	items := make([]autofix.GitHubRepository, 0, len(repos))
	for _, repo := range repos {
		item := autofix.GitHubRepository{GitHubRepoID: repo.ID, FullName: repo.FullName, Visibility: repo.Kind()}
		if id, ok := connected[repo.ID]; ok {
			item.ConnectedRepoID = &id
		}
		items = append(items, item)
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

// connectRepo connects a private repository of an active installation, after reading it from GitHub now.
func (a *API) connectRepo(response http.ResponseWriter, request *http.Request) {
	if !a.requireGitHub(response) {
		return
	}
	var body struct {
		InstallationID   string             `json:"installation_id"`
		GitHubRepoID     int64              `json:"github_repo_id"`
		DefaultConnector *autofix.Connector `json:"default_connector"`
	}
	if err := decodeJSON(request, &body); err != nil || body.GitHubRepoID <= 0 || body.InstallationID == "" ||
		body.DefaultConnector != nil && !body.DefaultConnector.Valid() {
		writeError(response, http.StatusBadRequest, "invalid_request", "installation_id (uuid) and github_repo_id (a positive integer) are required; default_connector is claude_issue, copilot_issue or cursor_api.")
		return
	}
	who := principal(request)
	installation, err := a.store.GetGitHubInstallation(request.Context(), who.Workspace.ID, body.InstallationID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	switch installation.Status {
	case autofix.InstallationBlocked:
		writeGitHubError(response, autofix.ErrInstallationBlocked)
		return
	case autofix.InstallationSuspended:
		writeGitHubError(response, autofix.ErrInstallationSuspended)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), githubCallTimeout)
	defer cancel()
	seen, err := a.githubSync.App().Repository(ctx, installation.InstallationID, body.GitHubRepoID)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	actor := actorOf(who)
	repo, err := a.store.ConnectAutofixRepo(request.Context(), who.Workspace.ID, store.NewAutofixRepo{InstallationID: installation.ID,
		Repo:             store.RepoSeen{GitHubRepoID: seen.ID, FullName: seen.FullName, Visibility: seen.Kind(), DefaultBranch: seen.DefaultBranch},
		DefaultConnector: body.DefaultConnector, AgentID: actor.ID, AgentName: actor.Name})
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, repo)
}

func (a *API) updateRepo(response http.ResponseWriter, request *http.Request) {
	var body struct {
		DefaultConnector *autofix.Connector `json:"default_connector"`
	}
	var raw map[string]any
	if err := decodeJSON(request, &raw); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be {\"default_connector\": null | \"claude_issue\" | \"copilot_issue\" | \"cursor_api\"}.")
		return
	}
	value, present := raw["default_connector"]
	if len(raw) != 1 || !present {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be {\"default_connector\": null | \"claude_issue\" | \"copilot_issue\" | \"cursor_api\"}.")
		return
	}
	if value != nil {
		text, ok := value.(string)
		connector := autofix.Connector(text)
		if !ok || !connector.Valid() {
			writeError(response, http.StatusBadRequest, "invalid_request", "default_connector is null, claude_issue, copilot_issue or cursor_api.")
			return
		}
		body.DefaultConnector = &connector
	}
	repo, err := a.store.SetRepoDefaultConnector(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), body.DefaultConnector)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, repo)
}

// disconnectRepo removes a connected repository and cancels its fixes in progress.
func (a *API) disconnectRepo(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	fixes, err := a.store.DisconnectAutofixRepo(request.Context(), who.Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	githubsync.EndFixes(request.Context(), a.store, who.Workspace.ID, fixes, autofix.Cancelled, "repo_disconnected", actorOf(who))
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) includeNotesInFixes(response http.ResponseWriter, request *http.Request) {
	a.setRepoNotes(response, request, true)
}

func (a *API) excludeNotesFromFixes(response http.ResponseWriter, request *http.Request) {
	a.setRepoNotes(response, request, false)
}

func (a *API) setRepoNotes(response http.ResponseWriter, request *http.Request, include bool) {
	repo, err := a.store.SetRepoIncludeNotes(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), include)
	if err != nil {
		writeGitHubError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, repo)
}
