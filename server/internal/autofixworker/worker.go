// Package autofixworker runs the Autofix fixes that wait on Colliedesk rather than on a person or on a
// GitHub webhook: it sends a fix to Cursor, reads its run until it ends, and stops it when a person
// cancels. Every move goes through store.ApplyFixObservation (docs/internals/autofix.md).
//
// The worker leases due fixes of every workspace (store.ClaimDueFixes, next_check_at moved a lease
// ahead) and handles each in its own workspace, without a transaction across the vendor call. A process
// that dies mid-check leaves the fix due again after the lease; a second create of the same fix answers
// 409 agent_id_conflict, so it is never sent twice.
package autofixworker

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/cursor"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Timings of the worker. A run is read every 15 seconds for its first 5 minutes, every minute until
// 30 minutes, then every 5 minutes.
const (
	lease          = 2 * time.Minute
	batch          = 20
	maxFailures    = 5
	firstPolls     = 15 * time.Second
	middlePolls    = time.Minute
	latePolls      = 5 * time.Minute
	waitForKey     = 5 * time.Minute
	firstBackoff   = 30 * time.Second
	longestBackoff = 10 * time.Minute
)

// Worker runs the due fixes. Repos reads a repository on GitHub right before a fix leaves for it.
type Worker struct {
	Cursor *Cursor
	Repos  autofix.RepoChecker
}

// Run checks due fixes every interval until ctx ends.
func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("Autofix worker tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick handles the fixes due now and returns how many it leased. Tests call it instead of Run.
func (w *Worker) Tick(ctx context.Context) (int, error) {
	due, err := w.Cursor.Store.ClaimDueFixes(ctx, w.Cursor.now(), []autofix.Connector{autofix.CursorAPI}, batch, lease)
	if err != nil {
		return 0, err
	}
	for _, item := range due {
		if err := w.handle(ctx, item); err != nil && ctx.Err() == nil {
			// The fix stays leased and comes due again after the lease.
			slog.Error("Autofix worker could not check a fix", "workspace", item.WorkspaceID, "fix", item.ID, "error", err)
		}
	}
	return len(due), nil
}

func (w *Worker) handle(ctx context.Context, item store.DueFix) error {
	work, err := w.Cursor.Store.GetFixWork(ctx, item.WorkspaceID, item.ID)
	if errors.Is(err, autofix.ErrFixNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if work.State == autofix.Cancelled && reasonOf(work) == "repo_disconnected" {
		// Its repository was disconnected while the run went on: stop it, as a person's cancel does.
		if err := w.Cursor.Stop(ctx, item.WorkspaceID, item.ID); err != nil {
			slog.Warn("Autofix could not stop the Cursor run of a fix whose repository was disconnected", "workspace", item.WorkspaceID, "fix", item.ID, "error", err)
		}
		return w.Cursor.Store.StopFixChecks(ctx, item.WorkspaceID, item.ID)
	}
	if !work.State.InProgress() {
		return w.Cursor.Store.StopFixChecks(ctx, item.WorkspaceID, item.ID)
	}
	if work.State == autofix.Sending {
		return w.dispatch(ctx, item.WorkspaceID, work)
	}
	return w.poll(ctx, item.WorkspaceID, work)
}

// end moves the fix to failed or cancelled with its reason and a line for the timeline, and takes it
// off the worker's list.
func (w *Worker) end(ctx context.Context, workspaceID string, work store.FixWork, state autofix.State, reason, summary string, actor autofix.Actor) error {
	if _, _, err := w.Cursor.Store.ApplyFixObservation(ctx, workspaceID, work.ID, autofix.Observation{State: state, Reason: reason, Summary: summary}, actor); err != nil {
		return err
	}
	return w.Cursor.Store.StopFixChecks(ctx, workspaceID, work.ID)
}

var systemActor = autofix.Actor{Kind: autofix.ActorSystem, Name: "Colliedesk"}

// later schedules the fix's next check without counting a failure (waiting for the call budget, a
// key, or Cursor's Retry-After).
func (w *Worker) later(ctx context.Context, workspaceID string, work store.FixWork, wait time.Duration) error {
	return w.Cursor.Store.PostponeFixCheck(ctx, workspaceID, work.ID, w.Cursor.now().Add(wait))
}

// retry counts a passing failure (Cursor's 5xx, the network) and schedules the next try with a growing
// wait; the fifth in a row fails the fix provider_unavailable.
func (w *Worker) retry(ctx context.Context, workspaceID string, work store.FixWork, cause error) error {
	wait := firstBackoff << min(work.CheckFailures, 5)
	wait = min(wait, longestBackoff)
	failures, err := w.Cursor.Store.RecordFixCheckFailure(ctx, workspaceID, work.ID, w.Cursor.now().Add(wait))
	if err != nil {
		return err
	}
	slog.Warn("Autofix could not reach Cursor", "workspace", workspaceID, "fix", work.ID, "failures", failures, "error", cause)
	if failures >= maxFailures {
		return w.end(ctx, workspaceID, work, autofix.Failed, "provider_unavailable", "Cursor did not answer after 5 tries.", systemActor)
	}
	return nil
}

// dispatch sends a fix in state sending to Cursor: the repository read on GitHub just before (private,
// still reached), the agent created with the fix's id, the fix moved to sent with the agent's ids.
func (w *Worker) dispatch(ctx context.Context, workspaceID string, work store.FixWork) error {
	if work.RepoID == nil {
		return w.end(ctx, workspaceID, work, autofix.Cancelled, "repo_disconnected", "The repository was disconnected before the fix was sent.", systemActor)
	}
	// Turning Cursor off, or a consent to an older text, withdraws the consent: nothing leaves.
	connector, err := w.Cursor.Store.GetFixConnector(ctx, workspaceID, autofix.CursorAPI)
	if err != nil {
		return err
	}
	if !connector.Enabled || connector.Consent == nil || connector.Consent.Version != autofix.ConsentVersion {
		return w.end(ctx, workspaceID, work, autofix.Cancelled, "connector_disabled", "Cursor was turned off before the fix was sent; nothing was sent.", systemActor)
	}
	key, err := w.Cursor.Store.OpenCursorKey(ctx, workspaceID)
	if errors.Is(err, autofix.ErrCursorKeyMissing) {
		return w.end(ctx, workspaceID, work, autofix.Failed, "vendor_auth", "No Cursor key is set. Set one in Settings → Autofix, then retry.", systemActor)
	}
	if err != nil {
		return err
	}
	if w.Repos == nil {
		// Nothing leaves without the repository read on GitHub first; wait for a server that can.
		return w.later(ctx, workspaceID, work, waitForKey)
	}
	repo, err := w.Repos.CheckRepo(ctx, workspaceID, *work.RepoID)
	switch {
	case errors.Is(err, autofix.ErrRepoNotFound):
		return w.end(ctx, workspaceID, work, autofix.Cancelled, "repo_disconnected", "The repository was disconnected before the fix was sent.", systemActor)
	case errors.Is(err, autofix.ErrRepoUnreachable) && w.suspended(ctx, workspaceID, *work.RepoID):
		// Suspended on GitHub only for a while: the repository resumes when it is unsuspended, so the fix waits.
		return w.later(ctx, workspaceID, work, waitForKey)
	case errors.Is(err, autofix.ErrRepoUnreachable):
		return w.end(ctx, workspaceID, work, autofix.Failed, "no_repo_access", "The Colliedesk GitHub App no longer reaches "+work.RepoFullName+".", systemActor)
	case err != nil:
		return w.retry(ctx, workspaceID, work, err)
	case repo.Visibility != "private":
		return w.end(ctx, workspaceID, work, autofix.Failed, "repo_public", work.RepoFullName+" is not private any more; nothing was sent.", systemActor)
	}
	ok, wait := w.Cursor.budget.take(workspaceID, w.Cursor.now())
	if !ok {
		return w.later(ctx, workspaceID, work, wait)
	}
	if err := w.Cursor.Store.MarkFixDispatchStarted(ctx, workspaceID, work.ID); err != nil {
		return err
	}
	client := w.Cursor.client(key)
	branch := repo.DefaultBranch
	if branch == "" {
		branch = work.DefaultBranch
	}
	created, err := client.CreateAgent(ctx, cursor.NewAgent{ID: agentID(work.ID), Name: work.SentTitle,
		Prompt: work.SentTitle + "\n\n" + work.SentBody, RepoURL: repoURL(repo.FullName), StartingRef: branch})
	var cursorErr *cursor.Error
	if errors.As(err, &cursorErr) && cursorErr.Status == http.StatusConflict && cursorErr.Code == "agent_id_conflict" {
		// Created before (a process that died after the create, a lease that ran out): read it back.
		agent, getErr := client.GetAgent(ctx, agentID(work.ID))
		if getErr != nil {
			return w.vendorError(ctx, workspaceID, work, getErr, true)
		}
		created, err = cursor.Created{Agent: agent, Run: cursor.Run{ID: agent.LatestRunID, AgentID: agent.ID}}, nil
	}
	if err != nil {
		return w.vendorError(ctx, workspaceID, work, err, true)
	}
	if created.Agent.ID == "" || created.Run.ID == "" {
		return w.retry(ctx, workspaceID, work, errors.New("cursor answered the create without an agent or a run"))
	}
	obs := autofix.Observation{State: autofix.Sent, Kind: "dispatched", VendorRef: created.Agent.ID, VendorRunID: created.Run.ID,
		VendorURL: httpsOnly(created.Agent.URL), Summary: "Cursor agent created."}
	moved, _, err := w.Cursor.Store.ApplyFixObservation(ctx, workspaceID, work.ID, obs, cursorActor)
	if err != nil {
		return err
	}
	if !moved.State.InProgress() {
		// A person cancelled the fix while it was being sent: keep what was created, and stop it.
		if err := w.Cursor.Store.RecordFixVendorRef(ctx, workspaceID, work.ID, obs.VendorRef, obs.VendorRunID, obs.VendorURL); err != nil {
			return err
		}
		stopErr := client.CancelRun(ctx, created.Agent.ID, created.Run.ID)
		if stopErr != nil && cursor.StatusOf(stopErr) != http.StatusConflict {
			slog.Warn("Autofix could not stop the Cursor run of a fix cancelled while it was sent", "workspace", workspaceID, "fix", work.ID, "error", stopErr)
		}
		return w.Cursor.Store.StopFixChecks(ctx, workspaceID, work.ID)
	}
	return w.Cursor.Store.ScheduleFixCheck(ctx, workspaceID, work.ID, w.Cursor.now().Add(firstPolls))
}

// suspended reports whether the repository is unreachable only because its installation is suspended
// on GitHub, which ends by itself.
func (w *Worker) suspended(ctx context.Context, workspaceID, repoID string) bool {
	repo, err := w.Cursor.Store.GetAutofixRepo(ctx, workspaceID, repoID)
	if err != nil {
		return false
	}
	if repo.PausedReason != nil && *repo.PausedReason == autofix.PausedInstallationSuspended {
		return true
	}
	if repo.InstallationID == nil {
		return false
	}
	installation, err := w.Cursor.Store.GitHubInstallationByID(ctx, workspaceID, *repo.InstallationID)
	return err == nil && installation.Status == autofix.InstallationSuspended
}

// agentID is the client id of a fix's Cursor agent: bc-<fix id>, so a second create is refused.
func agentID(fixID string) string { return "bc-" + fixID }

func httpsOnly(address string) string {
	if len(address) > 8 && address[:8] == "https://" {
		return address
	}
	return ""
}

// vendorError ends or retries a fix after Cursor refused a call. creating is true for the create,
// where a refusal names the repository or the content.
func (w *Worker) vendorError(ctx context.Context, workspaceID string, work store.FixWork, err error, creating bool) error {
	var cursorErr *cursor.Error
	if !errors.As(err, &cursorErr) {
		return w.retry(ctx, workspaceID, work, err)
	}
	message := cursorErr.Message
	if message != "" {
		message = " Cursor said: " + message
	}
	switch status := cursorErr.Status; {
	case status == http.StatusUnauthorized:
		if _, checkErr := w.Cursor.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckKeyInvalid,
			"Cursor refused the key while sending a fix. Set a new key."); checkErr != nil {
			return checkErr
		}
		return w.end(ctx, workspaceID, work, autofix.Failed, "vendor_auth", "Cursor refused the workspace's key."+message, cursorActor)
	case status == http.StatusTooManyRequests:
		return w.later(ctx, workspaceID, work, cursorErr.RetryAfter)
	case status >= 500:
		return w.retry(ctx, workspaceID, work, err)
	case creating && (status == http.StatusForbidden || status == http.StatusNotFound || cursorErr.Code == "repository_access"):
		return w.end(ctx, workspaceID, work, autofix.Failed, "vendor_repo_access", "Cursor cannot reach "+work.RepoFullName+"."+message, cursorActor)
	case creating:
		return w.end(ctx, workspaceID, work, autofix.Failed, "vendor_rejected", "Cursor refused the fix."+message, cursorActor)
	case status == http.StatusForbidden:
		return w.end(ctx, workspaceID, work, autofix.Failed, "vendor_auth", "Cursor no longer lets the workspace's key read this agent."+message, cursorActor)
	case status == http.StatusNotFound:
		return w.end(ctx, workspaceID, work, autofix.Failed, "agent_error", "The Cursor agent no longer exists.", cursorActor)
	}
	return w.retry(ctx, workspaceID, work, err)
}

// pollEvery is how long until a sent fix's run is read again.
func pollEvery(since time.Duration) time.Duration {
	switch {
	case since < 5*time.Minute:
		return firstPolls
	case since < 30*time.Minute:
		return middlePolls
	}
	return latePolls
}

// poll reads a sent fix's run and moves the fix the way it went.
func (w *Worker) poll(ctx context.Context, workspaceID string, work store.FixWork) error {
	if work.VendorRef == "" || work.VendorRunID == "" {
		// Not one this worker sent (a cursor_api fix moved by hand): nothing to read.
		return w.Cursor.Store.StopFixChecks(ctx, workspaceID, work.ID)
	}
	key, err := w.Cursor.Store.OpenCursorKey(ctx, workspaceID)
	if errors.Is(err, autofix.ErrCursorKeyMissing) {
		// The key was removed while the agent runs: wait for a new one; the run goes on at Cursor.
		return w.later(ctx, workspaceID, work, waitForKey)
	}
	if err != nil {
		return err
	}
	ok, wait := w.Cursor.budget.take(workspaceID, w.Cursor.now())
	if !ok {
		return w.later(ctx, workspaceID, work, wait)
	}
	run, err := w.Cursor.client(key).GetRun(ctx, work.VendorRef, work.VendorRunID)
	if err != nil {
		return w.vendorError(ctx, workspaceID, work, err, false)
	}
	obs, terminal := observe(run, work.RepoFullName, work.RepoName)
	if obs.State != "" {
		if _, _, err := w.Cursor.Store.ApplyFixObservation(ctx, workspaceID, work.ID, obs, cursorActor); err != nil {
			return err
		}
	}
	if terminal {
		// Cursor is done; GitHub's webhooks and the reconcile pass follow the pull request.
		return w.Cursor.Store.StopFixChecks(ctx, workspaceID, work.ID)
	}
	since := time.Duration(0)
	if work.DispatchStartedAt != nil {
		since = w.Cursor.now().Sub(*work.DispatchStartedAt)
	}
	return w.Cursor.Store.ScheduleFixCheck(ctx, workspaceID, work.ID, w.Cursor.now().Add(pollEvery(since)))
}

// observe turns a run into the observation it is (no state when there is nothing to say) and says
// whether the run ended. A pull request counts only when it is one of the fix's repository.
// The repository is known by the name the fix was sent to and by its name now (renamed meanwhile).
func observe(run cursor.Run, names ...string) (autofix.Observation, bool) {
	var obs autofix.Observation
branches:
	for _, branch := range run.Branches {
		if !slices.ContainsFunc(names, func(name string) bool { return name != "" && sameRepo(branch.RepoURL, name) }) {
			continue
		}
		if branch.Branch != "" && len(branch.Branch) <= 255 {
			obs.Branch = branch.Branch
		}
		for _, name := range names {
			if number, address, err := autofix.ParsePRURL(name, branch.PRURL); name != "" && err == nil {
				obs.PRNumber, obs.PRURL = number, address
				break branches
			}
		}
	}
	result := run.Result
	switch run.Status {
	case cursor.RunCreating:
		return autofix.Observation{}, false
	case cursor.RunRunning, cursor.RunWaiting:
		obs.State = autofix.Working
		if obs.PRURL != "" {
			obs.State = autofix.PROpen
		}
		return obs, false
	case cursor.RunFinished:
		if obs.PRURL != "" {
			obs.State, obs.Summary = autofix.PROpen, summary(result)
			return obs, true
		}
		return autofix.Observation{State: autofix.Failed, Reason: "agent_error", Summary: orDefault(summary(result), "Cursor finished without a pull request.")}, true
	case cursor.RunError:
		return autofix.Observation{State: autofix.Failed, Reason: "agent_error", Summary: orDefault(summary(result), "Cursor's run failed.")}, true
	case cursor.RunCancelled:
		return autofix.Observation{State: autofix.Cancelled, Reason: "cancelled_by_person", Summary: "The run was cancelled on Cursor."}, true
	case cursor.RunExpired:
		return autofix.Observation{State: autofix.Failed, Reason: "timed_out", Summary: "Cursor's run expired."}, true
	}
	// A status this version does not know: keep reading.
	return autofix.Observation{}, false
}

// summary is a vendor's reply cut for the timeline.
func summary(text string) string {
	runes := []rune(text)
	if len(runes) > 300 {
		return string(runes[:299]) + "…"
	}
	return text
}

func orDefault(text, fallback string) string {
	if text == "" {
		return fallback
	}
	return text
}

func reasonOf(work store.FixWork) string {
	if work.OutcomeReason == nil {
		return ""
	}
	return *work.OutcomeReason
}
