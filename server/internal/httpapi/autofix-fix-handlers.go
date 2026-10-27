package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/autofix/githubsync"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/github"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Sending a conversation to a coding agent: the preview, the send and its retry, and a person's changes
// to a fix (cancel, release, pull request). An issue connector sends as the person: the send answers a
// GitHub authorization address, and the callback hands the person's token, in memory only, to a
// goroutine that creates the issue (githubsync.Service.Dispatch) and revokes it.

// vendorRetention says how long each vendor keeps what a fix sends.
var vendorRetention = map[autofix.Connector]string{
	autofix.ClaudeIssue:  "GitHub keeps the issue until someone deletes it; the Claude Code workflow runs in your repository's Actions.",
	autofix.CopilotIssue: "GitHub keeps the issue until someone deletes it; Copilot works on it under your GitHub plan.",
	autofix.CursorAPI:    "Cursor keeps the agent and what it was sent until it is deleted (no limit by default).",
}

// fixSourceOf builds what a fix of the conversation is made of: what the customer wrote and the notes
// picked (never one an agent token wrote), with every note the sender may pick and how many files stay
// behind.
type fixSources struct {
	conversation domain.Conversation
	customer     []autofix.SourceMessage
	notes        []domain.Message
	files        int
}

func (a *API) fixSources(ctx context.Context, workspaceID, conversationID string) (fixSources, error) {
	conversation, err := a.store.GetConversation(ctx, workspaceID, conversationID)
	if err != nil {
		return fixSources{}, err
	}
	messages, err := a.store.ListMessages(ctx, workspaceID, conversationID)
	if err != nil {
		return fixSources{}, err
	}
	sources := fixSources{conversation: conversation}
	for _, message := range messages {
		switch {
		case message.Kind == "incoming":
			author := conversation.ContactName
			if message.AuthorName != nil && *message.AuthorName != "" {
				author = *message.AuthorName
			}
			if author == "" {
				author = "customer"
			}
			sources.customer = append(sources.customer, autofix.SourceMessage{ID: message.ID, Author: author, At: message.CreatedAt, Body: message.Body})
			sources.files += len(message.Attachments)
		case message.Kind == "note" && message.AgentToken == nil && message.AgentRequestID == nil:
			// A note an agent token wrote, or one a person approved from a token's request, never leaves.
			sources.notes = append(sources.notes, message)
		}
	}
	return sources, nil
}

// pickNotes returns the notes of ids, in the conversation's order, or false when one is not a note the
// sender may pick.
func (s fixSources) pickNotes(ids []string) ([]domain.Message, bool) {
	var picked []domain.Message
	for _, id := range ids {
		if !slices.ContainsFunc(s.notes, func(note domain.Message) bool { return note.ID == id }) {
			return nil, false
		}
	}
	for _, note := range s.notes {
		if slices.Contains(ids, note.ID) {
			picked = append(picked, note)
		}
	}
	return picked, true
}

func noteSources(notes []domain.Message) []autofix.SourceMessage {
	sources := make([]autofix.SourceMessage, 0, len(notes))
	for _, note := range notes {
		author := ""
		if note.AuthorName != nil {
			author = *note.AuthorName
		}
		sources = append(sources, autofix.SourceMessage{ID: note.ID, Author: author, At: note.CreatedAt, Body: note.Body})
	}
	return sources
}

// fixTarget checks the repository and coding agent a fix would go to: a connected repository not paused,
// and an agent turned on with the current consent.
func (a *API) fixTarget(response http.ResponseWriter, ctx context.Context, workspaceID, repoID string, connector autofix.Connector, needOn bool) (autofix.Repo, bool) {
	repo, err := a.store.GetAutofixRepo(ctx, workspaceID, repoID)
	if err != nil {
		writeGitHubError(response, err)
		return autofix.Repo{}, false
	}
	if repo.PausedReason != nil || repo.InstallationID == nil {
		writeError(response, http.StatusConflict, "repo_paused", "This repository takes no fix now (paused: "+pausedText(repo)+"). Settings → Autofix says what to do.")
		return autofix.Repo{}, false
	}
	if !needOn {
		return repo, true
	}
	setting, err := a.store.GetFixConnector(ctx, workspaceID, connector)
	if err != nil {
		writeAutofixError(response, err)
		return autofix.Repo{}, false
	}
	if !setting.Enabled || setting.Consent == nil || setting.Consent.Version != autofix.ConsentVersion {
		writeError(response, http.StatusConflict, "connector_off", "This coding agent is not turned on for the workspace (or its data consent is to an older text). An admin turns it on in Settings → Autofix.")
		return autofix.Repo{}, false
	}
	if connector == autofix.CursorAPI && setting.Credential == nil {
		writeConnectorError(response, autofix.ErrCursorKeyMissing)
		return autofix.Repo{}, false
	}
	return repo, true
}

func pausedText(repo autofix.Repo) string {
	if repo.PausedReason != nil {
		return *repo.PausedReason
	}
	return "removed_from_app"
}

type fixPreviewInput struct {
	RepoID    string            `json:"repo_id"`
	Connector autofix.Connector `json:"connector"`
	NoteIDs   []string          `json:"note_ids"`
}

func (a *API) previewFix(response http.ResponseWriter, request *http.Request) {
	var input fixPreviewInput
	if err := decodeJSON(request, &input); err != nil || input.RepoID == "" || !input.Connector.Valid() || len(input.NoteIDs) > 50 {
		writeError(response, http.StatusBadRequest, "invalid_request", "repo_id (uuid) and connector (claude_issue, copilot_issue or cursor_api) are required; note_ids holds at most 50 note ids.")
		return
	}
	ctx, workspaceID := request.Context(), principal(request).Workspace.ID
	sources, err := a.fixSources(ctx, workspaceID, request.PathValue("id"))
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	repo, ok := a.fixTarget(response, ctx, workspaceID, input.RepoID, input.Connector, false)
	if !ok {
		return
	}
	if len(input.NoteIDs) > 0 && !repo.IncludeNotes {
		writeError(response, http.StatusConflict, "notes_not_allowed", "This repository does not let internal notes go with a fix. An admin allows it in Settings → Autofix.")
		return
	}
	picked, ok := sources.pickNotes(input.NoteIDs)
	if !ok {
		writeError(response, http.StatusBadRequest, "invalid_request", "note_ids names a message that is not an internal note of this conversation a person wrote.")
		return
	}
	draft := autofix.BuildFix(autofix.FixSource{Subject: sources.conversation.Subject, Customer: sources.customer, Notes: noteSources(picked)})
	notes := make([]map[string]any, 0, len(sources.notes))
	files := sources.files
	for _, note := range sources.notes {
		included := slices.Contains(input.NoteIDs, note.ID)
		if included {
			files += len(note.Attachments)
		}
		notes = append(notes, map[string]any{"message_id": note.ID, "author_name": note.AuthorName, "created_at": note.CreatedAt.UTC(),
			"excerpt": autofix.Excerpt(note.Body), "included": included})
	}
	sendsAs := "person"
	if input.Connector == autofix.CursorAPI {
		sendsAs = "workspace_key"
	}
	writeJSON(response, http.StatusOK, map[string]any{"title": draft.Title, "body": draft.Body, "message_ids": draft.MessageIDs, "notes": notes,
		"notes_allowed": repo.IncludeNotes, "attachments_left_out": files, "removed": draft.Removed,
		"destination": map[string]any{"repo_full_name": repo.FullName, "connector": input.Connector, "sends_as": sendsAs, "vendor_retention": vendorRetention[input.Connector]}})
}

type fixSendInput struct {
	RepoID     string            `json:"repo_id"`
	Connector  autofix.Connector `json:"connector"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	MessageIDs []string          `json:"message_ids"`
	NoteIDs    []string          `json:"note_ids"`
	ClientKey  string            `json:"client_key"`
}

func (a *API) sendFix(response http.ResponseWriter, request *http.Request) {
	a.sendFixOf(response, request, request.PathValue("id"), "")
}

// retryFix sends an ended fix again as a new attempt of its conversation.
func (a *API) retryFix(response http.ResponseWriter, request *http.Request) {
	previous, err := a.store.GetFix(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	if !autofix.CanRetry(previous.State) {
		writeError(response, http.StatusConflict, "fix_not_ended", "This fix is still in progress. Cancel it first, or wait for it to end.")
		return
	}
	a.sendFixOf(response, request, previous.ConversationID, previous.ID)
}

func (a *API) sendFixOf(response http.ResponseWriter, request *http.Request, conversationID, previousID string) {
	var input fixSendInput
	if err := decodeJSON(request, &input); err != nil || input.RepoID == "" || !input.Connector.Valid() || input.ClientKey == "" ||
		utf8.RuneCountInString(input.ClientKey) > 100 || len(input.MessageIDs) > 500 || len(input.NoteIDs) > 50 {
		writeError(response, http.StatusBadRequest, "invalid_request", "repo_id, connector, title, body and client_key are required (build title and body with the preview).")
		return
	}
	if !a.requireGitHub(response) {
		return
	}
	title, _ := autofix.Mask(input.Title)
	body, _ := autofix.Mask(input.Body)
	if utf8.RuneCountInString(title) < 1 || utf8.RuneCountInString(title) > autofix.MaxTitleRunes || utf8.RuneCountInString(body) < 1 || utf8.RuneCountInString(body) > autofix.MaxBodyRunes {
		writeError(response, http.StatusBadRequest, "invalid_request", "After masking, the title must be 1 to 256 characters and the body 1 to 65000.")
		return
	}
	ctx, who := request.Context(), principal(request)
	sources, err := a.fixSources(ctx, who.Workspace.ID, conversationID)
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	repo, ok := a.fixTarget(response, ctx, who.Workspace.ID, input.RepoID, input.Connector, true)
	if !ok {
		return
	}
	if len(input.NoteIDs) > 0 && !repo.IncludeNotes {
		writeError(response, http.StatusConflict, "notes_not_allowed", "This repository does not let internal notes go with a fix. An admin allows it in Settings → Autofix.")
		return
	}
	if _, ok := sources.pickNotes(input.NoteIDs); !ok {
		writeError(response, http.StatusBadRequest, "invalid_request", "note_ids names a message that is not an internal note of this conversation a person wrote.")
		return
	}
	for _, id := range input.MessageIDs {
		if !slices.ContainsFunc(sources.customer, func(message autofix.SourceMessage) bool { return message.ID == id }) {
			writeError(response, http.StatusBadRequest, "invalid_request", "message_ids names a message that is not one the customer wrote in this conversation.")
			return
		}
	}
	actor := actorOf(who)
	created := store.NewFix{ConversationID: conversationID, RepoID: repo.ID, Connector: input.Connector, SentTitle: title, SentBody: body,
		SentMessageIDs: input.MessageIDs, SentNotes: len(input.NoteIDs) > 0, ClientKey: input.ClientKey, PreviousAttemptID: previousID,
		CreatedByAgentID: actor.ID, CreatedByName: actor.Name}
	if who.IsToken() {
		created.TokenID = who.SessionID
	}
	if replay, ok := approvalOf(ctx); ok {
		// The person who approved sends it as themselves; the fix names the request and its token.
		created.RequestID, created.TokenID = replay.requestID, replay.tokenID
	}
	fix, made, err := a.store.CreateFix(ctx, who.Workspace.ID, created)
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	if fix.CreatedByAgentID == nil || *fix.CreatedByAgentID != who.Agent.ID {
		writeAutofixError(response, autofix.ErrClientKeyReused)
		return
	}
	status := http.StatusOK
	if made {
		status = http.StatusCreated
	}
	var authorizeURL *string
	if fix.Connector == autofix.CursorAPI {
		// Cursor needs no person's authorization: the worker sends it with the workspace's key.
		if made {
			if err := a.store.ScheduleFixCheck(ctx, who.Workspace.ID, fix.ID, a.now()); err != nil {
				writeInternalError(response, err)
				return
			}
		}
		writeJSON(response, status, map[string]any{"fix": fix, "authorize_url": authorizeURL})
		return
	}
	if work, err := a.store.GetFixDispatch(ctx, who.Workspace.ID, fix.ID); err != nil {
		writeAutofixError(response, err)
		return
	} else if work.State == autofix.Sending && work.DispatchStartedAt == nil {
		// Also for the same key sent again while it waits: the person may come back from GitHub.
		now := a.now()
		started, err := a.store.CreateAutofixOAuthState(ctx, who.Workspace.ID, who.Agent.ID, "dispatch", fix.ID, now, now.Add(githubAuthorizationTTL))
		if err != nil {
			writeInternalError(response, err)
			return
		}
		address := a.githubSync.App().AuthorizeURL(a.config.Origin+autofixCallbackPath, started.State, started.Verifier)
		authorizeURL = &address
	}
	writeJSON(response, status, map[string]any{"fix": fix, "authorize_url": authorizeURL})
}

// finishFixAuthorization is the callback of a fix's GitHub authorization, after its state was taken by
// the person who started it: the code becomes the person's token, a goroutine sends the fix with it, and
// the browser goes back to the conversation at once (it shows Sending). It returns where the browser goes.
func (a *API) finishFixAuthorization(request *http.Request, who auth.Principal, started store.AutofixOAuthState) string {
	ctx := request.Context()
	fix, err := a.store.GetFix(ctx, who.Workspace.ID, started.FixID)
	if err != nil {
		return autofixSettingsPage + "?github_error=expired"
	}
	page := "/conversations/" + url.PathEscape(fix.ConversationID)
	failed := func(reason string) string { return page + "?fix_error=" + reason }
	if started.AgentID != who.Agent.ID {
		return failed("wrong_person")
	}
	if !auth.HasPermission(who.Agent.Role, auth.PermissionConversationsWrite) || (a.limits && who.Access.Locked) {
		return failed("forbidden")
	}
	query := request.URL.Query()
	if query.Get("error") != "" {
		// The person said no on GitHub: nothing left Colliedesk.
		if _, _, err := a.store.ApplyFixObservation(ctx, who.Workspace.ID, fix.ID, autofix.Observation{State: autofix.Cancelled, Reason: "authorization_denied",
			Kind: "authorization_denied"}, autofix.Actor{Kind: autofix.ActorSystem, ID: who.Agent.ID, Name: actorOf(who).Name}); err != nil {
			slog.Error("autofix: end a fix whose authorization was denied", "error", err, "workspace", who.Workspace.ID, "fix", fix.ID)
		}
		return failed("denied")
	}
	if fix.State != autofix.Sending {
		return failed("expired")
	}
	if query.Get("code") == "" {
		return failed("denied")
	}
	exchange, cancel := context.WithTimeout(ctx, githubCallTimeout)
	defer cancel()
	token, err := a.githubSync.App().Exchange(exchange, query.Get("code"), started.Verifier, a.config.Origin+autofixCallbackPath)
	if errors.Is(err, github.ErrCodeRefused) {
		return failed("denied")
	}
	if err != nil {
		slog.Warn("autofix: GitHub code exchange failed", "error", err, "workspace", who.Workspace.ID)
		return failed("github_unavailable")
	}
	a.dispatchFix(who.Workspace.ID, fix.ID, token)
	return page + "?fix=" + url.QueryEscape(fix.ID)
}

// dispatchFix sends the fix in a goroutine of this process, detached from the request, with the person's
// token held in its memory only and revoked when it ends, whatever happens. A process that dies on the
// way leaves the fix sending; the reconcile pass ends it or finds its issue.
func (a *API) dispatchFix(workspaceID, fixID, token string) {
	a.dispatches.Add(1)
	go func() {
		defer a.dispatches.Done()
		defer a.revokeGitHubToken(token, workspaceID)
		ctx, cancel := context.WithTimeout(context.Background(), githubsync.DispatchTimeout)
		defer cancel()
		if err := a.githubSync.Dispatch(ctx, workspaceID, fixID, token); err != nil {
			slog.Warn("autofix: send a fix to GitHub", "error", err, "workspace", workspaceID, "fix", fixID)
		}
	}()
}

// WaitForAutofixSends waits until every fix being sent to GitHub by this process is done, so their
// tokens are revoked: at a graceful stop (bounded by githubsync.DispatchTimeout) and in tests.
func (s *Server) WaitForAutofixSends() { s.api.dispatches.Wait() }

type fixChange struct {
	RowVersion int    `json:"row_version"`
	URL        string `json:"url"`
}

// changeFix applies a person's change to a fix. A body is optional except for linking a pull request.
func (a *API) changeFix(response http.ResponseWriter, request *http.Request, action autofix.Action) {
	var input fixChange
	if request.ContentLength != 0 {
		if err := decodeJSON(request, &input); err != nil || input.RowVersion < 0 || (action != autofix.ActionLinkPR && input.URL != "") {
			writeError(response, http.StatusBadRequest, "invalid_request", "The body is {\"row_version\": <the version read>} (and url, to link a pull request).")
			return
		}
	}
	if action == autofix.ActionLinkPR && input.URL == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "url is required: https://github.com/<owner>/<repo>/pull/<number>.")
		return
	}
	ctx, who := request.Context(), principal(request)
	fix, err := a.store.ActOnFix(ctx, who.Workspace.ID, request.PathValue("id"), store.FixAction{Action: action, PRURL: input.URL, RowVersion: input.RowVersion}, actorOf(who))
	if errors.Is(err, autofix.ErrMove) && (action == autofix.ActionDeployed || action == autofix.ActionUndeployed) {
		// Saying a release again, or taking back one already taken back, is the state asked for.
		if current, readErr := a.store.GetFix(ctx, who.Workspace.ID, request.PathValue("id")); readErr == nil &&
			(action == autofix.ActionDeployed && current.State == autofix.Deployed || action == autofix.ActionUndeployed && current.State == autofix.Merged) {
			writeJSON(response, http.StatusOK, current.Fix)
			return
		}
	}
	if err != nil {
		writeAutofixError(response, err)
		return
	}
	if action == autofix.ActionCancel && fix.Connector == autofix.CursorAPI {
		// After the cancel committed: Cursor's run is cancelled. A fix not sent yet has none; the worker
		// stops whatever it was creating.
		stopping, cancel := context.WithTimeout(ctx, cursorCallTimeout)
		defer cancel()
		if err := a.cursor.Cancel(stopping, who.Workspace.ID, fix.ID); err != nil {
			slog.Warn("autofix: cancel the Cursor run of a cancelled fix", "error", err, "workspace", who.Workspace.ID, "fix", fix.ID)
		}
	}
	if action == autofix.ActionCancel && fix.IssueNumber != nil && a.githubSync != nil {
		// After the cancel committed: the issue is closed with a comment. The agent may still run; the
		// fix links the issue to stop it there.
		closing, cancel := context.WithTimeout(ctx, githubCallTimeout)
		defer cancel()
		if err := a.githubSync.CloseFixIssue(closing, who.Workspace.ID, fix); err != nil {
			slog.Warn("autofix: close the issue of a cancelled fix", "error", err, "workspace", who.Workspace.ID, "fix", fix.ID)
			// The cancel stands; the timeline says the issue is still open, so the person closes it on GitHub.
			data := map[string]any{"summary": "GitHub did not close the issue: close it there to stop the agent."}
			if fix.IssueURL != nil {
				data["issue_url"] = *fix.IssueURL
			}
			if err := a.store.NoteFixEvent(ctx, who.Workspace.ID, fix.ID, "issue_close_failed", data, autofix.Actor{Kind: autofix.ActorSystem, Name: "Colliedesk"}); err != nil {
				slog.Error("autofix: note an issue left open", "error", err, "workspace", who.Workspace.ID, "fix", fix.ID)
			}
		}
	}
	writeJSON(response, http.StatusOK, fix)
}

func (a *API) cancelFix(response http.ResponseWriter, request *http.Request) {
	a.changeFix(response, request, autofix.ActionCancel)
}

func (a *API) markFixDeployed(response http.ResponseWriter, request *http.Request) {
	a.changeFix(response, request, autofix.ActionDeployed)
}

func (a *API) unmarkFixDeployed(response http.ResponseWriter, request *http.Request) {
	a.changeFix(response, request, autofix.ActionUndeployed)
}

func (a *API) linkFixPullRequest(response http.ResponseWriter, request *http.Request) {
	a.changeFix(response, request, autofix.ActionLinkPR)
}

func (a *API) unlinkFixPullRequest(response http.ResponseWriter, request *http.Request) {
	a.changeFix(response, request, autofix.ActionUnlinkPR)
}
