// Package githubsync keeps Autofix in step with GitHub through the Colliedesk GitHub App: it applies the
// App's webhook events, reconciles every fix in progress and every connected repository every ten minutes
// (a self-hosted server behind a firewall gets no webhook and still follows, only later), and reads a
// repository now before a fix is sent to it. Every move of a fix goes through store.ApplyFixObservation.
package githubsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// ReconcileEvery is how often the reconcile pass reads GitHub.
const ReconcileEvery = 10 * time.Minute

// reconcileBatch bounds the fixes one pass reads per workspace.
const reconcileBatch = 200

// What a webhook delivery did (WebhookResult.status).
const (
	Applied   = "applied"
	Duplicate = "duplicate"
	Stale     = "stale"
	Ignored   = "ignored"
)

// Service is the GitHub side of Autofix.
type Service struct {
	store *store.Store
	app   *github.App
	now   func() time.Time
}

// New makes the service for the App of this server.
func New(st *store.Store, app *github.App) *Service {
	return &Service{store: st, app: app, now: time.Now}
}

// App is the GitHub App the service calls.
func (s *Service) App() *github.App { return s.app }

// CheckRepo implements autofix.RepoChecker: it reads the connected repository on GitHub now with the
// installation's token, records its name and visibility, and pauses it when it is no longer private.
func (s *Service) CheckRepo(ctx context.Context, workspaceID, repoID string) (autofix.RepoCheck, error) {
	repo, err := s.store.GetAutofixRepo(ctx, workspaceID, repoID)
	if err != nil {
		return autofix.RepoCheck{}, err
	}
	if repo.InstallationID == nil || (repo.PausedReason != nil && *repo.PausedReason != autofix.PausedBecamePublic) {
		return autofix.RepoCheck{}, autofix.ErrRepoUnreachable
	}
	installation, err := s.store.GitHubInstallationByID(ctx, workspaceID, *repo.InstallationID)
	if errors.Is(err, autofix.ErrInstallationNotFound) || err == nil && installation.Status != autofix.InstallationActive {
		return autofix.RepoCheck{}, autofix.ErrRepoUnreachable
	}
	if err != nil {
		return autofix.RepoCheck{}, err
	}
	seen, err := s.app.Repository(ctx, *repo.InstallationID, repo.GitHubRepoID)
	if errors.Is(err, github.ErrNotFound) {
		if _, err := s.store.RepoLeftApp(ctx, workspaceID, repo.GitHubRepoID); err != nil {
			return autofix.RepoCheck{}, err
		}
		return autofix.RepoCheck{}, autofix.ErrRepoUnreachable
	}
	if err != nil {
		return autofix.RepoCheck{}, err
	}
	change, err := s.store.RecordRepoSeen(ctx, workspaceID, store.RepoSeen{GitHubRepoID: seen.ID, FullName: seen.FullName, Visibility: seen.Kind(), DefaultBranch: seen.DefaultBranch})
	if err != nil {
		return autofix.RepoCheck{}, err
	}
	return autofix.RepoCheck{Visibility: change.Repo.Visibility, FullName: change.Repo.FullName, DefaultBranch: change.Repo.DefaultBranch}, nil
}

// event is the part of a webhook payload Autofix reads.
type event struct {
	Action       string `json:"action"`
	Installation *struct {
		ID                  int64      `json:"id"`
		RepositorySelection string     `json:"repository_selection"`
		SuspendedAt         *time.Time `json:"suspended_at"`
	} `json:"installation"`
	RepositorySelection string        `json:"repository_selection"`
	RepositoriesAdded   []payloadRepo `json:"repositories_added"`
	RepositoriesRemoved []payloadRepo `json:"repositories_removed"`
	Repository          *payloadRepo  `json:"repository"`
	Issue               *struct {
		Number      int             `json:"number"`
		StateReason string          `json:"state_reason"`
		PullRequest json.RawMessage `json:"pull_request"`
	} `json:"issue"`
	Assignee *user `json:"assignee"`
	Comment  *struct {
		Body string `json:"body"`
		User user   `json:"user"`
	} `json:"comment"`
	PullRequest *struct {
		Number         int    `json:"number"`
		HTMLURL        string `json:"html_url"`
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		MergedBy       *user  `json:"merged_by"`
		Head           struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	Sender user `json:"sender"`
}

type payloadRepo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Visibility    string `json:"visibility"`
	DefaultBranch string `json:"default_branch"`
}

func (r payloadRepo) seen() store.RepoSeen {
	return store.RepoSeen{GitHubRepoID: r.ID, FullName: r.FullName, Visibility: github.Repository{Private: r.Private, Visibility: r.Visibility}.Kind(),
		DefaultBranch: r.DefaultBranch}
}

type user struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// delivery is one webhook delivery being applied in its workspace.
type delivery struct {
	workspaceID    string
	installationID int64
	id             string
	actor          autofix.Actor
	// moved is what the delivery did so far.
	moved, stale bool
}

func (d *delivery) record(result autofix.Result) {
	if result == autofix.Ignore {
		d.stale = true
	} else {
		d.moved = true
	}
}

func (d *delivery) status() string {
	switch {
	case d.moved:
		return Applied
	case d.stale:
		return Stale
	}
	return Ignored
}

// HandleEvent applies one verified webhook delivery. It acts only in the workspace that owns the
// installation, and on a fix only through a repository connected there with that installation. An error
// is worth GitHub's redelivery (nothing recorded the delivery); the status says what it did.
func (s *Service) HandleEvent(ctx context.Context, name, deliveryID string, body []byte) (string, error) {
	var payload event
	if err := json.Unmarshal(body, &payload); err != nil {
		return Ignored, nil
	}
	if payload.Installation == nil || payload.Installation.ID <= 0 {
		return Ignored, nil
	}
	workspaceID, found, seen, err := s.store.GitHubWebhookRoute(ctx, payload.Installation.ID, deliveryID)
	if err != nil {
		return "", err
	}
	if seen {
		return Duplicate, nil
	}
	if !found {
		return Ignored, nil
	}
	d := &delivery{workspaceID: workspaceID, installationID: payload.Installation.ID, id: deliveryID,
		actor: autofix.Actor{Kind: autofix.ActorGitHub, ID: payload.Sender.Login, Name: payload.Sender.Login}}
	switch name {
	case "installation":
		err = s.installationEvent(ctx, d, payload)
	case "installation_repositories":
		err = s.installationRepositoriesEvent(ctx, d, payload)
	case "repository":
		err = s.repositoryEvent(ctx, d, payload)
	case "issues":
		err = s.issuesEvent(ctx, d, payload)
	case "issue_comment":
		err = s.issueCommentEvent(ctx, d, payload)
	case "pull_request":
		err = s.pullRequestEvent(ctx, d, payload)
	}
	if err != nil {
		return "", err
	}
	if err := s.store.RecordGitHubDelivery(ctx, deliveryID); err != nil {
		return "", err
	}
	return d.status(), nil
}

func (s *Service) installationEvent(ctx context.Context, d *delivery, payload event) error {
	switch payload.Action {
	case "deleted":
		removed, err := s.store.RemoveGitHubInstallationByID(ctx, d.workspaceID, d.installationID)
		if errors.Is(err, autofix.ErrInstallationNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		d.moved = true
		return s.endFixes(ctx, d, removed.FixIDs, autofix.Cancelled, "repo_disconnected")
	case "suspend", "unsuspend":
		if _, err := s.store.SetGitHubInstallationState(ctx, d.workspaceID, d.installationID, payload.Installation.RepositorySelection,
			payload.Action == "suspend"); err != nil && !errors.Is(err, autofix.ErrInstallationNotFound) {
			return err
		}
		d.moved = true
	}
	return nil
}

func (s *Service) installationRepositoriesEvent(ctx context.Context, d *delivery, payload event) error {
	installation, err := s.store.GitHubInstallationByID(ctx, d.workspaceID, d.installationID)
	if errors.Is(err, autofix.ErrInstallationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	selection := payload.RepositorySelection
	if selection == "" {
		selection = payload.Installation.RepositorySelection
	}
	if selection != "" && selection != installation.RepositorySelection {
		// Narrowed to selected repositories, the installation unblocks by itself (and widened, it blocks).
		if _, err := s.store.SetGitHubInstallationState(ctx, d.workspaceID, d.installationID, selection, installation.Status == autofix.InstallationSuspended); err != nil {
			return err
		}
		d.moved = true
	}
	for _, repo := range payload.RepositoriesRemoved {
		change, err := s.store.RepoLeftApp(ctx, d.workspaceID, repo.ID)
		if errors.Is(err, autofix.ErrRepoNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		d.moved = true
		if err := s.endFixes(ctx, d, change.InProgress, autofix.Cancelled, "repo_disconnected"); err != nil {
			return err
		}
	}
	for _, added := range payload.RepositoriesAdded {
		if _, err := s.store.AutofixRepoByGitHubID(ctx, d.workspaceID, added.ID); errors.Is(err, autofix.ErrRepoNotFound) {
			continue
		} else if err != nil {
			return err
		}
		// The event names no visibility a private repository can be told apart by (internal ones are
		// private too): read it now.
		repo, err := s.app.Repository(ctx, d.installationID, added.ID)
		if err != nil {
			slog.Warn("autofix: read a repository added back to the App", "error", err, "workspace", d.workspaceID)
			continue
		}
		if _, err := s.store.RepoBackInApp(ctx, d.workspaceID, d.installationID, store.RepoSeen{GitHubRepoID: repo.ID, FullName: repo.FullName,
			Visibility: repo.Kind(), DefaultBranch: repo.DefaultBranch}); err != nil && !errors.Is(err, autofix.ErrRepoNotFound) {
			return err
		}
		d.moved = true
	}
	return nil
}

// connectedRepo is the workspace's repository of the event, when it is connected through the event's
// installation; ok is false otherwise and the event is ignored.
func (s *Service) connectedRepo(ctx context.Context, d *delivery, payload event) (autofix.Repo, bool, error) {
	if payload.Repository == nil {
		return autofix.Repo{}, false, nil
	}
	repo, err := s.store.AutofixRepoByGitHubID(ctx, d.workspaceID, payload.Repository.ID)
	if errors.Is(err, autofix.ErrRepoNotFound) {
		return autofix.Repo{}, false, nil
	}
	if err != nil {
		return autofix.Repo{}, false, err
	}
	if repo.InstallationID == nil || *repo.InstallationID != d.installationID {
		return autofix.Repo{}, false, nil
	}
	return repo, true, nil
}

func (s *Service) repositoryEvent(ctx context.Context, d *delivery, payload event) error {
	repo, ok, err := s.connectedRepo(ctx, d, payload)
	if err != nil || !ok {
		return err
	}
	switch payload.Action {
	case "deleted", "transferred":
		change, err := s.store.RepoLeftApp(ctx, d.workspaceID, repo.GitHubRepoID)
		if err != nil {
			return err
		}
		d.moved = true
		return s.endFixes(ctx, d, change.InProgress, autofix.Cancelled, "repo_disconnected")
	case "publicized", "privatized", "renamed", "edited":
		change, err := s.store.RecordRepoSeen(ctx, d.workspaceID, payload.Repository.seen())
		if err != nil {
			return err
		}
		d.moved = true
		// A fix not sent yet never leaves for a public repository; an issue already created stays (it was
		// private when it was made).
		return s.endFixes(ctx, d, change.Sending, autofix.Failed, "repo_public")
	}
	return nil
}

// copilotLogins are the accounts Copilot's coding agent is assigned as.
func isCopilot(u *user) bool {
	if u == nil {
		return false
	}
	login := strings.ToLower(u.Login)
	return login == "copilot" || strings.HasPrefix(login, "copilot-swe-agent")
}

func isClaude(u user) bool { return strings.EqualFold(u.Login, "claude[bot]") && u.Type == "Bot" }

func (s *Service) issuesEvent(ctx context.Context, d *delivery, payload event) error {
	if payload.Issue == nil || len(payload.Issue.PullRequest) > 0 && string(payload.Issue.PullRequest) != "null" {
		return nil
	}
	repo, ok, err := s.connectedRepo(ctx, d, payload)
	if err != nil || !ok {
		return err
	}
	fix, found, err := s.store.FixForIssue(ctx, d.workspaceID, repo.ID, payload.Issue.Number)
	if err != nil || !found {
		return err
	}
	switch payload.Action {
	case "assigned":
		if isCopilot(payload.Assignee) {
			return s.observe(ctx, d, fix, autofix.Observation{State: autofix.Working, Kind: "agent_assigned", Summary: "Assigned to " + payload.Assignee.Login})
		}
	case "closed":
		return s.issueClosed(ctx, d, repo, fix)
	case "reopened":
		return s.observe(ctx, d, fix, autofix.Observation{State: autofix.Sent, Kind: "issue_reopened"})
	}
	return nil
}

// issueClosed follows a closed issue. While the fix waits for its pull request, a merged pull request that
// closed the issue is the merge (its own webhook may come later or was missed); otherwise the issue was
// closed by hand and the fix is cancelled. With a pull request already, the rules only note it.
func (s *Service) issueClosed(ctx context.Context, d *delivery, repo autofix.Repo, fix autofix.Fix) error {
	if (fix.State == autofix.Sent || fix.State == autofix.Working) && fix.IssueNumber != nil {
		pulls, err := s.app.IssuePullRequests(ctx, d.installationID, repo.FullName, *fix.IssueNumber)
		if err != nil && !errors.Is(err, github.ErrNotFound) {
			return err
		}
		for _, pull := range pulls {
			if pull.State == "MERGED" {
				return s.observe(ctx, d, fix, autofix.Observation{State: autofix.Merged, Kind: "pr_merged", PRNumber: pull.Number, PRURL: canonicalPR(fullNameOf(repo, fix), pull.Number),
					Branch: pull.HeadRef, MergeSHA: validSHA(pull.MergeCommitSHA), MergedBy: pull.MergedBy})
			}
		}
	}
	return s.observe(ctx, d, fix, autofix.Observation{State: autofix.Cancelled, Reason: "issue_closed", Kind: "issue_closed"})
}

func (s *Service) issueCommentEvent(ctx context.Context, d *delivery, payload event) error {
	if payload.Action != "created" || payload.Comment == nil || !isClaude(payload.Comment.User) || payload.Issue == nil ||
		len(payload.Issue.PullRequest) > 0 && string(payload.Issue.PullRequest) != "null" {
		return nil
	}
	repo, ok, err := s.connectedRepo(ctx, d, payload)
	if err != nil || !ok {
		return err
	}
	fix, found, err := s.store.FixForIssue(ctx, d.workspaceID, repo.ID, payload.Issue.Number)
	if err != nil || !found {
		return err
	}
	return s.observe(ctx, d, fix, autofix.Observation{State: autofix.Working, Kind: "agent_comment", Summary: firstLine(payload.Comment.Body)})
}

// claudeBranch is the branch claude-code-action works on for issue N.
var claudeBranch = regexp.MustCompile(`^claude/issue-([1-9][0-9]{0,9})-`)

// fixOfPullRequest finds the fix a pull request belongs to: the fix that has it, the fix whose agent works
// on its branch (Cursor), then the issue it closes ("Fixes #N", Copilot and Claude), then Claude's branch
// name. found is false when none.
func (s *Service) fixOfPullRequest(ctx context.Context, d *delivery, repo autofix.Repo, number int, branch string) (autofix.Fix, bool, error) {
	if fix, found, err := s.store.FixForPullRequest(ctx, d.workspaceID, repo.ID, number); err != nil || found {
		return fix, found, err
	}
	if branch != "" {
		if fix, found, err := s.store.FixForBranch(ctx, d.workspaceID, repo.ID, branch); err != nil || found {
			return fix, found, err
		}
	}
	issues, err := s.app.ClosingIssues(ctx, d.installationID, repo.FullName, number)
	if err != nil && !errors.Is(err, github.ErrNotFound) {
		return autofix.Fix{}, false, err
	}
	if match := claudeBranch.FindStringSubmatch(branch); match != nil {
		if n, err := strconv.Atoi(match[1]); err == nil {
			issues = append(issues, n)
		}
	}
	for _, issue := range issues {
		fix, found, err := s.store.FixForIssue(ctx, d.workspaceID, repo.ID, issue)
		if err != nil || found && fix.PRNumber == nil {
			return fix, found, err
		}
	}
	return autofix.Fix{}, false, nil
}

func (s *Service) pullRequestEvent(ctx context.Context, d *delivery, payload event) error {
	pull := payload.PullRequest
	if pull == nil {
		return nil
	}
	switch payload.Action {
	case "opened", "reopened", "ready_for_review", "closed":
	default:
		return nil
	}
	repo, ok, err := s.connectedRepo(ctx, d, payload)
	if err != nil || !ok {
		return err
	}
	fix, found, err := s.fixOfPullRequest(ctx, d, repo, pull.Number, pull.Head.Ref)
	if err != nil || !found {
		return err
	}
	if fix.PRNumber != nil && *fix.PRNumber != pull.Number {
		// Another pull request of the same fix's repository: the linked one decides.
		return nil
	}
	obs := autofix.Observation{PRNumber: pull.Number, PRURL: canonicalPR(repo.FullName, pull.Number), Branch: pull.Head.Ref}
	switch {
	case payload.Action == "closed" && pull.Merged:
		obs.State, obs.Kind, obs.MergeSHA = autofix.Merged, "pr_merged", validSHA(pull.MergeCommitSHA)
		if pull.MergedBy != nil {
			obs.MergedBy = pull.MergedBy.Login
		}
	case payload.Action == "closed":
		obs.State, obs.Reason, obs.Kind = autofix.Failed, "pr_closed", "pr_closed"
	default:
		obs.State, obs.Kind = autofix.PROpen, "pr_opened"
	}
	return s.observe(ctx, d, fix, obs)
}

func (s *Service) observe(ctx context.Context, d *delivery, fix autofix.Fix, obs autofix.Observation) error {
	obs.DeliveryID = d.id
	_, result, err := s.store.ApplyFixObservation(ctx, d.workspaceID, fix.ID, obs, d.actor)
	if err != nil {
		return fmt.Errorf("apply %s to fix %s: %w", obs.Kind, fix.ID, err)
	}
	d.record(result)
	return nil
}

// endFixes moves the fixes to an end the rules allow (a cancel never undoes a person's own, and a fix
// with a pull request only notes an issue's end).
func (s *Service) endFixes(ctx context.Context, d *delivery, ids []string, state autofix.State, reason string) error {
	for _, id := range ids {
		obs := autofix.Observation{State: state, Reason: reason, Kind: reason, DeliveryID: d.id}
		if _, result, err := s.store.ApplyFixObservation(ctx, d.workspaceID, id, obs, d.actor); err != nil && !errors.Is(err, autofix.ErrFixNotFound) {
			return fmt.Errorf("end fix %s: %w", id, err)
		} else if err == nil {
			d.record(result)
		}
	}
	return nil
}

// canonicalPR is a pull request's address written the way the fix keeps it.
func canonicalPR(fullName string, number int) string {
	return "https://github.com/" + fullName + "/pull/" + strconv.Itoa(number)
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validSHA(sha string) string {
	if shaPattern.MatchString(sha) {
		return sha
	}
	return ""
}

// firstLine is a comment's first non-empty line, cut for the timeline.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if len([]rune(line)) > 200 {
				line = string([]rune(line)[:200])
			}
			return line
		}
	}
	return ""
}

// Run reconciles at start and every interval until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := s.Reconcile(ctx); err != nil && ctx.Err() == nil {
			slog.Error("autofix: reconcile with GitHub", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Reconcile reads GitHub for every connected installation: whether it is still there, suspended or
// narrowed; whether each connected repository is still reachable and private; and the issue or pull
// request of every fix in progress. A failure of one installation is logged and the pass goes on.
func (s *Service) Reconcile(ctx context.Context) error {
	refs, err := s.store.GitHubInstallationsToReconcile(ctx, s.now())
	if err != nil {
		return err
	}
	workspaces := map[string]bool{}
	for _, ref := range refs {
		if err := s.reconcileInstallation(ctx, ref); err != nil {
			slog.Warn("autofix: reconcile a GitHub installation", "error", err, "workspace", ref.WorkspaceID, "installation", ref.InstallationID)
		}
		workspaces[ref.WorkspaceID] = true
	}
	for workspaceID := range workspaces {
		if err := s.reconcileFixes(ctx, workspaceID); err != nil {
			slog.Warn("autofix: reconcile fixes", "error", err, "workspace", workspaceID)
		}
	}
	return nil
}

func (s *Service) reconcileInstallation(ctx context.Context, ref store.GitHubInstallationRef) error {
	d := &delivery{workspaceID: ref.WorkspaceID, installationID: ref.InstallationID, id: "reconcile", actor: autofix.Actor{Kind: autofix.ActorSystem, Name: "GitHub check"}}
	installation, err := s.app.Installation(ctx, ref.InstallationID)
	if errors.Is(err, github.ErrNotFound) {
		removed, err := s.store.RemoveGitHubInstallationByID(ctx, ref.WorkspaceID, ref.InstallationID)
		if err != nil {
			return err
		}
		return s.endFixes(ctx, d, removed.FixIDs, autofix.Cancelled, "repo_disconnected")
	}
	if err != nil {
		return err
	}
	current, err := s.store.SetGitHubInstallationState(ctx, ref.WorkspaceID, ref.InstallationID, installation.RepositorySelection, installation.SuspendedAt != nil)
	if err != nil {
		return err
	}
	if current.Status == autofix.InstallationSuspended {
		return nil
	}
	reachable, err := s.app.Repositories(ctx, ref.InstallationID)
	if err != nil {
		return err
	}
	byID := map[int64]github.Repository{}
	for _, repo := range reachable {
		byID[repo.ID] = repo
	}
	settings, err := s.store.AutofixSettings(ctx, ref.WorkspaceID)
	if err != nil {
		return err
	}
	for _, repo := range settings.Repos {
		if repo.InstallationID != nil && *repo.InstallationID != ref.InstallationID {
			continue
		}
		seen, ok := byID[repo.GitHubRepoID]
		switch {
		case ok && repo.InstallationID == nil:
			// The App was installed again on the account: the repository comes back with it.
			if _, err := s.store.RepoBackInApp(ctx, ref.WorkspaceID, ref.InstallationID, store.RepoSeen{GitHubRepoID: seen.ID, FullName: seen.FullName,
				Visibility: seen.Kind(), DefaultBranch: seen.DefaultBranch}); err != nil && !errors.Is(err, autofix.ErrRepoNotFound) {
				return err
			}
		case ok && repo.PausedReason != nil && *repo.PausedReason == autofix.PausedRemovedFromApp:
			if _, err := s.store.RepoBackInApp(ctx, ref.WorkspaceID, ref.InstallationID, store.RepoSeen{GitHubRepoID: seen.ID, FullName: seen.FullName,
				Visibility: seen.Kind(), DefaultBranch: seen.DefaultBranch}); err != nil {
				return err
			}
		case ok:
			change, err := s.store.RecordRepoSeen(ctx, ref.WorkspaceID, store.RepoSeen{GitHubRepoID: seen.ID, FullName: seen.FullName, Visibility: seen.Kind(),
				DefaultBranch: seen.DefaultBranch})
			if err != nil {
				return err
			}
			if err := s.endFixes(ctx, d, change.Sending, autofix.Failed, "repo_public"); err != nil {
				return err
			}
		case repo.InstallationID != nil && (repo.PausedReason == nil || *repo.PausedReason != autofix.PausedRemovedFromApp):
			change, err := s.store.RepoLeftApp(ctx, ref.WorkspaceID, repo.GitHubRepoID)
			if err != nil {
				return err
			}
			if err := s.endFixes(ctx, d, change.InProgress, autofix.Cancelled, "repo_disconnected"); err != nil {
				return err
			}
		}
	}
	return nil
}

// reconcileFixes reads the issue or pull request of every fix of the workspace that was sent and has not
// ended. A fix whose repository left the App is cancelled; one on a suspended installation waits.
func (s *Service) reconcileFixes(ctx context.Context, workspaceID string) error {
	items, err := s.store.FixesToReconcile(ctx, workspaceID, reconcileBatch)
	if err != nil {
		return err
	}
	for _, item := range items {
		d := &delivery{workspaceID: workspaceID, id: "reconcile", actor: autofix.Actor{Kind: autofix.ActorSystem, Name: "GitHub check"}}
		if item.InstallationID == nil || item.GitHubRepoID == nil || item.RepoPaused != nil && *item.RepoPaused == autofix.PausedRemovedFromApp {
			if err := s.endFixes(ctx, d, []string{item.Fix.ID}, autofix.Cancelled, "repo_disconnected"); err != nil {
				return err
			}
			continue
		}
		if item.RepoPaused != nil && *item.RepoPaused == autofix.PausedInstallationSuspended {
			continue
		}
		d.installationID = *item.InstallationID
		if err := s.reconcileFix(ctx, d, item); err != nil {
			slog.Warn("autofix: reconcile a fix", "error", err, "workspace", workspaceID, "fix", item.Fix.ID)
		}
	}
	return nil
}

func (s *Service) reconcileFix(ctx context.Context, d *delivery, item store.ReconcileItem) error {
	fix := item.Fix
	repo := autofix.Repo{ID: deref(fix.RepoID), FullName: fix.RepoFullName}
	if fix.PRNumber != nil {
		pull, err := s.app.PullRequest(ctx, d.installationID, fix.RepoFullName, *fix.PRNumber)
		if err != nil {
			return err
		}
		obs := autofix.Observation{PRNumber: pull.Number, PRURL: canonicalPR(fix.RepoFullName, pull.Number), Branch: pull.Head.Ref}
		switch {
		case pull.Merged:
			obs.State, obs.Kind, obs.MergeSHA, obs.MergedBy = autofix.Merged, "pr_merged", validSHA(pull.MergeCommitSHA), pull.MergedByLogin()
		case pull.State == "closed":
			obs.State, obs.Reason, obs.Kind = autofix.Failed, "pr_closed", "pr_closed"
		default:
			obs.State, obs.Kind = autofix.PROpen, "pr_opened"
		}
		return s.observe(ctx, d, fix, obs)
	}
	if item.IssueNumber == nil {
		// Cursor without a pull request yet: its own poll follows it.
		return nil
	}
	issue, err := s.app.Issue(ctx, d.installationID, fix.RepoFullName, *item.IssueNumber)
	if err != nil {
		return err
	}
	if issue.State == "closed" {
		return s.issueClosed(ctx, d, repo, fix)
	}
	pulls, err := s.app.IssuePullRequests(ctx, d.installationID, fix.RepoFullName, *item.IssueNumber)
	if err != nil {
		return err
	}
	for _, pull := range pulls {
		switch pull.State {
		case "MERGED":
			return s.observe(ctx, d, fix, autofix.Observation{State: autofix.Merged, Kind: "pr_merged", PRNumber: pull.Number, PRURL: canonicalPR(fullNameOf(repo, fix), pull.Number),
				Branch: pull.HeadRef, MergeSHA: validSHA(pull.MergeCommitSHA), MergedBy: pull.MergedBy})
		case "OPEN":
			return s.observe(ctx, d, fix, autofix.Observation{State: autofix.PROpen, Kind: "pr_opened", PRNumber: pull.Number, PRURL: canonicalPR(fullNameOf(repo, fix), pull.Number), Branch: pull.HeadRef})
		}
	}
	return nil
}

func deref(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

// fullNameOf is the repository's name now, or the one the fix was sent to.
func fullNameOf(repo autofix.Repo, fix autofix.Fix) string {
	if repo.FullName != "" {
		return repo.FullName
	}
	return fix.RepoFullName
}

// EndFixes ends fixes because of a person's change in Colliedesk (a repository or an installation
// disconnected), after that change committed. The move is the system's, named after the person, so the
// rules treat it like any signal: a fix that ended already keeps its end. Failures are logged.
func EndFixes(ctx context.Context, st *store.Store, workspaceID string, ids []string, state autofix.State, reason string, person autofix.Actor) {
	actor := autofix.Actor{Kind: autofix.ActorSystem, ID: person.ID, Name: person.Name}
	for _, id := range ids {
		obs := autofix.Observation{State: state, Reason: reason, Kind: reason}
		if _, _, err := st.ApplyFixObservation(ctx, workspaceID, id, obs, actor); err != nil && !errors.Is(err, autofix.ErrFixNotFound) {
			slog.Error("autofix: end a fix", "error", err, "workspace", workspaceID, "fix", id, "reason", reason)
		}
	}
}

// ResumeRepos reads a newly connected installation at once, as the reconcile pass would: repositories
// connected before the App was removed come back with it. Failures are logged; the next pass retries.
func (s *Service) ResumeRepos(ctx context.Context, workspaceID string, installationID int64) {
	if err := s.reconcileInstallation(ctx, store.GitHubInstallationRef{WorkspaceID: workspaceID, InstallationID: installationID}); err != nil {
		slog.Warn("autofix: read a connected installation", "error", err, "workspace", workspaceID, "installation", installationID)
	}
}
