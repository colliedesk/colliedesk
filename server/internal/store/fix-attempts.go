package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fixStale and fixGroup compute the screen's flag and group of fix f in SQL, so the list filters by them.
// A stale fix is sent with no sign of its agent for autofix.StaleAfter; it is always the conversation's
// latest (one in progress per conversation), so it needs a person.
const (
	fixStale = `(f.state = 'sent' AND f.state_changed_at < now() - interval '30 minutes')`
	fixGroup = `(CASE WHEN f.state IN ('sending','sent','working','pr_open') AND NOT ` + fixStale + ` THEN 'running'
		WHEN (f.state IN ('failed','merged') OR ` + fixStale + `) AND NOT EXISTS (SELECT 1 FROM fix_attempts n
			WHERE n.workspace_id = f.workspace_id AND n.conversation_id = f.conversation_id AND n.attempt_no > f.attempt_no) THEN 'needs_person'
		ELSE 'done' END)`
	// fixFrom joins the conversation and its customer for the subject and the name the list shows.
	fixFrom = ` FROM fix_attempts f JOIN conversations c ON c.workspace_id = f.workspace_id AND c.id = f.conversation_id
		LEFT JOIN contacts k ON k.workspace_id = c.workspace_id AND k.id = c.contact_id`
	fixColumns = `f.id::text, f.conversation_id::text, c.subject, k.name, f.attempt_no, f.previous_attempt_id::text, f.repo_id::text, f.repo_full_name,
		f.connector, f.state, f.outcome_reason, f.state_changed_at, f.row_version, ` + fixStale + `, ` + fixGroup + `,
		f.sent_title, f.sent_message_ids::text[], f.sent_notes, f.created_by_kind, f.created_by_agent_id::text, f.created_by_name,
		f.approved_request_id::text, f.github_login, f.copilot_assignment, f.issue_number, f.vendor_url, f.branch, f.pr_number, f.pr_url, f.pr_linked_by,
		f.merge_sha, f.merged_at, f.merged_by_login, f.deployed_at, f.deployed_by_name, f.created_at`
)

func scanFix(row pgx.Row) (autofix.Fix, error) {
	var fix autofix.Fix
	err := row.Scan(&fix.ID, &fix.ConversationID, &fix.ConversationSubject, &fix.ContactName, &fix.AttemptNo, &fix.PreviousAttemptID, &fix.RepoID, &fix.RepoFullName,
		&fix.Connector, &fix.State, &fix.OutcomeReason, &fix.StateChangedAt, &fix.RowVersion, &fix.Stale, &fix.Group,
		&fix.SentTitle, &fix.SentMessageIDs, &fix.SentNotes, &fix.CreatedByKind, &fix.CreatedByAgentID, &fix.CreatedByName,
		&fix.ApprovedRequestID, &fix.GitHubLogin, &fix.CopilotAssignment, &fix.IssueNumber, &fix.VendorURL, &fix.Branch, &fix.PRNumber, &fix.PRURL, &fix.PRLinkedBy,
		&fix.MergeSHA, &fix.MergedAt, &fix.MergedByLogin, &fix.DeployedAt, &fix.DeployedByName, &fix.CreatedAt)
	if err != nil {
		return autofix.Fix{}, err
	}
	if fix.IssueNumber != nil {
		address := autofix.IssueURL(fix.RepoFullName, *fix.IssueNumber)
		fix.IssueURL = &address
	}
	if fix.SentMessageIDs == nil {
		fix.SentMessageIDs = []string{}
	}
	fix.StateChangedAt, fix.CreatedAt = fix.StateChangedAt.UTC(), fix.CreatedAt.UTC()
	for _, at := range []**time.Time{&fix.MergedAt, &fix.DeployedAt} {
		if *at != nil {
			utc := (*at).UTC()
			*at = &utc
		}
	}
	return fix, nil
}

// FixFilter narrows the workspace's fixes: one group of the Fixes screen, one repository, one coding
// agent, one page.
type FixFilter struct {
	Group     string
	RepoID    string
	Connector autofix.Connector
	Limit     int
	Cursor    string
}

// ListFixes returns one page of the workspace's fixes, newest first, and the cursor of the next page
// ("" on the last one).
func (s *Store) ListFixes(ctx context.Context, workspaceID string, filter FixFilter) ([]autofix.Fix, string, error) {
	query := `SELECT ` + fixColumns + fixFrom + ` WHERE f.workspace_id = $1`
	args := []any{workspaceID, filter.Limit + 1}
	if filter.Group != "" {
		args = append(args, filter.Group)
		query += fmt.Sprintf(` AND %s = $%d`, fixGroup, len(args))
	}
	if filter.RepoID != "" {
		if !savedReplyID.MatchString(filter.RepoID) {
			return []autofix.Fix{}, "", nil
		}
		args = append(args, filter.RepoID)
		query += fmt.Sprintf(` AND f.repo_id = $%d`, len(args))
	}
	if filter.Connector != "" {
		args = append(args, string(filter.Connector))
		query += fmt.Sprintf(` AND f.connector = $%d`, len(args))
	}
	if filter.Cursor != "" {
		at, id, err := decodeTrashCursor(filter.Cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, at, id)
		query += fmt.Sprintf(` AND (f.created_at, f.id) < ($%d, $%d)`, len(args)-1, len(args))
	}
	fixes, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]autofix.Fix, error) {
		rows, err := tx.Query(ctx, query+` ORDER BY f.created_at DESC, f.id DESC LIMIT $2`, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (autofix.Fix, error) { return scanFix(row) })
	})
	if err != nil {
		return nil, "", fmt.Errorf("list fixes: %w", err)
	}
	next := ""
	if len(fixes) > filter.Limit {
		fixes = fixes[:filter.Limit]
		last := fixes[filter.Limit-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt.Format(time.RFC3339Nano) + " " + last.ID))
	}
	return fixes, next, nil
}

// ListConversationFixes returns every fix of a conversation, the latest attempt first.
func (s *Store) ListConversationFixes(ctx context.Context, workspaceID, conversationID string) ([]autofix.Fix, error) {
	if !savedReplyID.MatchString(conversationID) {
		return nil, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]autofix.Fix, error) {
		if _, err := getConversation(ctx, tx, workspaceID, conversationID); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT `+fixColumns+fixFrom+` WHERE f.workspace_id = $1 AND f.conversation_id = $2
			ORDER BY f.attempt_no DESC`, workspaceID, conversationID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (autofix.Fix, error) { return scanFix(row) })
	})
}

// GetFix returns one fix with what was sent and its timeline.
func (s *Store) GetFix(ctx context.Context, workspaceID, id string) (autofix.FixDetail, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.FixDetail{}, autofix.ErrFixNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.FixDetail, error) { return getFixDetail(ctx, tx, workspaceID, id) })
}

func getFix(ctx context.Context, tx pgx.Tx, workspaceID, id string, lock bool) (autofix.Fix, error) {
	query := `SELECT ` + fixColumns + fixFrom + ` WHERE f.workspace_id = $1 AND f.id = $2`
	if lock {
		query += ` FOR UPDATE OF f`
	}
	fix, err := scanFix(tx.QueryRow(ctx, query, workspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Fix{}, autofix.ErrFixNotFound
	}
	if err != nil {
		return autofix.Fix{}, fmt.Errorf("read the fix: %w", err)
	}
	return fix, nil
}

func getFixDetail(ctx context.Context, tx pgx.Tx, workspaceID, id string) (autofix.FixDetail, error) {
	fix, err := getFix(ctx, tx, workspaceID, id, false)
	if err != nil {
		return autofix.FixDetail{}, err
	}
	detail := autofix.FixDetail{Fix: fix}
	if err := tx.QueryRow(ctx, `SELECT sent_body FROM fix_attempts WHERE workspace_id = $1 AND id = $2`, workspaceID, id).Scan(&detail.SentBody); err != nil {
		return autofix.FixDetail{}, fmt.Errorf("read what the fix sent: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT id, kind, from_state, to_state, actor_kind, actor_name, data, created_at
		FROM fix_attempt_events WHERE workspace_id = $1 AND fix_attempt_id = $2 ORDER BY id`, workspaceID, id)
	if err != nil {
		return autofix.FixDetail{}, fmt.Errorf("read the fix's timeline: %w", err)
	}
	detail.Events, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (autofix.Event, error) {
		var event autofix.Event
		err := row.Scan(&event.ID, &event.Kind, &event.FromState, &event.ToState, &event.ActorKind, &event.ActorName, &event.Data, &event.CreatedAt)
		event.CreatedAt = event.CreatedAt.UTC()
		return event, err
	})
	if err != nil {
		return autofix.FixDetail{}, fmt.Errorf("read the fix's timeline: %w", err)
	}
	if detail.Events == nil {
		detail.Events = []autofix.Event{}
	}
	return detail, nil
}

// AutofixStatus counts what the navigation rail shows.
func (s *Store) AutofixStatus(ctx context.Context, workspaceID string) (autofix.Status, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Status, error) {
		var status autofix.Status
		err := tx.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM autofix_repos WHERE workspace_id = $1 AND paused_reason IS NULL),
			(SELECT count(*) FILTER (WHERE g = 'needs_person') FROM (SELECT `+fixGroup+` AS g FROM fix_attempts f
				WHERE f.workspace_id = $1 AND (f.state IN ('failed','merged') OR `+fixStale+`)) n),
			(SELECT count(*) FROM fix_attempts f WHERE f.workspace_id = $1 AND f.state IN ('sending','sent','working','pr_open') AND NOT `+fixStale+`)`,
			workspaceID).Scan(&status.ConnectedRepos, &status.NeedsPerson, &status.Running)
		if err != nil {
			return autofix.Status{}, fmt.Errorf("count fixes: %w", err)
		}
		return status, nil
	})
}

// NewFix is a fix about to leave Colliedesk: what was built (after masking and the person's edits), where
// it goes and who sends it.
type NewFix struct {
	ConversationID string
	RepoID         string
	Connector      autofix.Connector
	SentTitle      string
	SentBody       string
	SentMessageIDs []string
	SentNotes      bool
	// ClientKey is the sender's idempotency key: the same key returns the fix it made.
	ClientKey string
	// PreviousAttemptID names the ended fix this one retries.
	PreviousAttemptID string
	// CreatedBy is the person, or the agent token's owner with TokenID set; RequestID is the agent
	// request a person approved.
	CreatedByAgentID string
	CreatedByName    string
	TokenID          string
	RequestID        string
}

// CreateFix stores a fix in state sending, with its first event. The same client key returns the fix it
// made (created false). A conversation with a fix in progress refuses another (ErrFixInProgress); a
// retry names an ended fix of the same conversation.
func (s *Store) CreateFix(ctx context.Context, workspaceID string, input NewFix) (autofix.Fix, bool, error) {
	if !input.Connector.Valid() || !savedReplyID.MatchString(input.ConversationID) || !savedReplyID.MatchString(input.RepoID) {
		return autofix.Fix{}, false, pgx.ErrNoRows
	}
	type made struct {
		fix     autofix.Fix
		created bool
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (made, error) {
		// The conversation row is locked first, so two sends of one conversation (the same key sent twice at
		// once among them) take turns: the second finds the fix of the first.
		var locked int
		if err := tx.QueryRow(ctx, `SELECT 1 FROM conversations WHERE workspace_id = $1 AND id = $2 FOR UPDATE`, workspaceID, input.ConversationID).Scan(&locked); err != nil {
			return made{}, fmt.Errorf("read the conversation: %w", err)
		}
		if input.ClientKey != "" {
			fix, found, err := fixOfClientKey(ctx, tx, workspaceID, input)
			if err != nil || found {
				return made{fix, false}, err
			}
		}
		var repoName string
		if err := tx.QueryRow(ctx, `SELECT full_name FROM autofix_repos WHERE workspace_id = $1 AND id = $2`, workspaceID, input.RepoID).Scan(&repoName); err != nil {
			return made{}, fmt.Errorf("read the repository: %w", err)
		}
		if input.PreviousAttemptID != "" {
			previous, err := getFix(ctx, tx, workspaceID, input.PreviousAttemptID, false)
			if err != nil {
				return made{}, err
			}
			if previous.ConversationID != input.ConversationID || !autofix.CanRetry(previous.State) {
				return made{}, fmt.Errorf("%w: only a failed or cancelled fix of the same conversation is retried", autofix.ErrMove)
			}
		}
		kind := "person"
		if input.TokenID != "" {
			kind = "token"
		}
		messageIDs := input.SentMessageIDs
		if messageIDs == nil {
			messageIDs = []string{}
		}
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO fix_attempts (workspace_id, conversation_id, attempt_no, previous_attempt_id, repo_id, repo_full_name,
				connector, state, sent_title, sent_body, sent_message_ids, sent_notes, client_key, created_by_kind, created_by_agent_id, created_by_name,
				created_by_token_id, approved_request_id)
			SELECT $1, $2, COALESCE(max(attempt_no), 0) + 1, $3, $4, $5, $6, 'sending', $7, $8, $9::uuid[], $10, $11, $12, $13, $14, $15, $16
			FROM fix_attempts WHERE workspace_id = $1 AND conversation_id = $2
			RETURNING id::text`,
			workspaceID, input.ConversationID, optionalID(input.PreviousAttemptID), input.RepoID, repoName, string(input.Connector),
			input.SentTitle, input.SentBody, messageIDs, input.SentNotes, optionalID(input.ClientKey), kind, optionalID(input.CreatedByAgentID),
			input.CreatedByName, optionalID(input.TokenID), optionalID(input.RequestID)).Scan(&id)
		if inProgressConflict(err) {
			return made{}, autofix.ErrFixInProgress
		}
		if err != nil {
			return made{}, fmt.Errorf("store the fix: %w", err)
		}
		actor := autofix.Actor{Kind: autofix.ActorPerson, ID: input.CreatedByAgentID, Name: input.CreatedByName}
		if input.TokenID != "" {
			actor.Kind = autofix.ActorToken
		}
		data := map[string]any{"connector": input.Connector, "repo_full_name": repoName}
		if input.RequestID != "" {
			data["approved_request_id"] = input.RequestID
		}
		data["state"] = autofix.Sending
		if err := insertFixEvent(ctx, tx, workspaceID, id, "created", "", "", actor, data, ""); err != nil {
			return made{}, err
		}
		fix, err := getFix(ctx, tx, workspaceID, id, false)
		return made{fix, true}, err
	})
	if err != nil {
		return autofix.Fix{}, false, err
	}
	return result.fix, result.created, nil
}

// fixOfClientKey returns the fix a client key already made. The key names one send: used again for
// another conversation, repository or agent it is refused (ErrClientKeyReused).
func fixOfClientKey(ctx context.Context, tx pgx.Tx, workspaceID string, input NewFix) (autofix.Fix, bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM fix_attempts WHERE workspace_id = $1 AND client_key = $2`, workspaceID, input.ClientKey).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Fix{}, false, nil
	}
	if err != nil {
		return autofix.Fix{}, false, fmt.Errorf("read the fix of this key: %w", err)
	}
	fix, err := getFix(ctx, tx, workspaceID, id, false)
	if err != nil {
		return autofix.Fix{}, false, err
	}
	// A repository disconnected since leaves the fix without repo_id: the key is still that send.
	if fix.ConversationID != input.ConversationID || (fix.RepoID != nil && *fix.RepoID != input.RepoID) || fix.Connector != input.Connector {
		return autofix.Fix{}, false, autofix.ErrClientKeyReused
	}
	return fix, true, nil
}

// inProgressConflict reports the unique index that keeps one fix in progress per conversation.
func inProgressConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "fix_attempts_one_in_progress"
}

// newerFixInProgress reports whether the conversation has another fix in progress, so an ended fix may
// not come back into progress.
func newerFixInProgress(ctx context.Context, tx pgx.Tx, workspaceID string, fix autofix.Fix) (bool, error) {
	var busy bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM fix_attempts WHERE workspace_id = $1 AND conversation_id = $2 AND id <> $3
		AND state IN ('sending','sent','working','pr_open'))`, workspaceID, fix.ConversationID, fix.ID).Scan(&busy); err != nil {
		return false, fmt.Errorf("look for a newer fix: %w", err)
	}
	return busy, nil
}

func insertFixEvent(ctx context.Context, tx pgx.Tx, workspaceID, fixID, kind string, from, to autofix.State, actor autofix.Actor, data map[string]any, deliveryID string) error {
	if data == nil {
		data = map[string]any{}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode the fix event: %w", err)
	}
	// A move names both ends; an event that moves nothing (created, a signal only noted) names neither.
	var fromState, toState *string
	if from != "" && to != "" {
		fromValue, toValue := string(from), string(to)
		fromState, toState = &fromValue, &toValue
	}
	if _, err := tx.Exec(ctx, `INSERT INTO fix_attempt_events (workspace_id, fix_attempt_id, kind, from_state, to_state, actor_kind, actor_id, actor_name, data, delivery_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		workspaceID, fixID, kind, fromState, toState, string(actor.Kind), optionalText(actor.ID), optionalText(actor.Name), encoded, optionalText(deliveryID)); err != nil {
		return fmt.Errorf("record the fix event: %w", err)
	}
	return nil
}

func optionalText(text string) *string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &text
}

func fixCurrent(fix autofix.Fix) autofix.Current {
	c := autofix.Current{State: fix.State, HasPR: fix.PRURL != nil}
	if fix.OutcomeReason != nil {
		c.Reason = *fix.OutcomeReason
	}
	if fix.PRLinkedBy != nil {
		c.PRLinkedBy = *fix.PRLinkedBy
	}
	return c
}

// ApplyFixObservation is the one way a signal from GitHub, Cursor, the reconcile pass or the worker's
// clock reaches a fix: under the fix's row lock it applies the rules (autofix.Decide), stores what the
// signal learned, records the event and bumps row_version. It returns the fix and what it did.
func (s *Store) ApplyFixObservation(ctx context.Context, workspaceID, id string, obs autofix.Observation, actor autofix.Actor) (autofix.Fix, autofix.Result, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.Fix{}, autofix.Ignore, autofix.ErrFixNotFound
	}
	type applied struct {
		fix    autofix.Fix
		result autofix.Result
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (applied, error) {
		fix, err := getFix(ctx, tx, workspaceID, id, true)
		if err != nil {
			return applied{}, err
		}
		result, err := autofix.Decide(fixCurrent(fix), obs, actor)
		if err != nil {
			return applied{}, err
		}
		if result == autofix.Move && obs.State.InProgress() && !fix.State.InProgress() {
			// Back in progress (an issue or a pull request reopened) only while the conversation has no
			// newer fix in progress; otherwise the signal is only recorded.
			busy, err := newerFixInProgress(ctx, tx, workspaceID, fix)
			if err != nil {
				return applied{}, err
			}
			if busy {
				result = autofix.Note
			}
		}
		data := observationData(obs)
		switch result {
		case autofix.Ignore:
			return applied{fix, result}, nil
		case autofix.Note:
			kind := obs.Kind
			if kind == "" {
				kind = "signal_noted"
			}
			data["state"] = obs.State
			if err := insertFixEvent(ctx, tx, workspaceID, id, kind, "", "", actor, data, obs.DeliveryID); err != nil {
				return applied{}, err
			}
			return applied{fix, result}, nil
		}
		prLinkedBy := ""
		if obs.PRURL != "" && fix.PRURL == nil {
			prLinkedBy = "auto"
		}
		if _, err := tx.Exec(ctx, `UPDATE fix_attempts SET state = $3, outcome_reason = $4, state_changed_at = now(), row_version = row_version + 1,
				issue_number = COALESCE($5, issue_number), issue_node_id = COALESCE($6, issue_node_id), vendor_ref = COALESCE($7, vendor_ref),
				vendor_run_id = COALESCE($8, vendor_run_id), vendor_url = COALESCE($9, vendor_url), branch = COALESCE($10, branch),
				pr_number = COALESCE($11, pr_number), pr_url = COALESCE($12, pr_url), pr_linked_by = COALESCE(pr_linked_by, $13),
				merge_sha = COALESCE($14, merge_sha), merged_by_login = COALESCE($15, merged_by_login),
				merged_at = CASE WHEN $3 = 'merged' THEN COALESCE(merged_at, now()) ELSE merged_at END
			WHERE workspace_id = $1 AND id = $2`,
			workspaceID, id, string(obs.State), optionalText(obs.Reason), optionalNumber(obs.IssueNumber), optionalText(obs.IssueNodeID),
			optionalText(obs.VendorRef), optionalText(obs.VendorRunID), optionalText(obs.VendorURL), optionalText(obs.Branch),
			optionalNumber(obs.PRNumber), optionalText(obs.PRURL), optionalText(prLinkedBy), optionalText(obs.MergeSHA), optionalText(obs.MergedBy)); err != nil {
			return applied{}, fmt.Errorf("move the fix: %w", err)
		}
		kind := obs.Kind
		if kind == "" {
			kind = "state_changed"
		}
		if err := insertFixEvent(ctx, tx, workspaceID, id, kind, fix.State, obs.State, actor, data, obs.DeliveryID); err != nil {
			return applied{}, err
		}
		moved, err := getFix(ctx, tx, workspaceID, id, false)
		return applied{moved, result}, err
	})
	if err != nil {
		return autofix.Fix{}, autofix.Ignore, err
	}
	return out.fix, out.result, nil
}

func observationData(obs autofix.Observation) map[string]any {
	data := map[string]any{}
	for key, value := range map[string]string{"reason": obs.Reason, "summary": obs.Summary, "pr_url": obs.PRURL, "vendor_url": obs.VendorURL,
		"branch": obs.Branch, "merge_sha": obs.MergeSHA, "merged_by": obs.MergedBy} {
		if value != "" {
			data[key] = value
		}
	}
	for key, value := range map[string]int{"issue_number": obs.IssueNumber, "pr_number": obs.PRNumber} {
		if value != 0 {
			data[key] = value
		}
	}
	return data
}

func optionalNumber(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// FixAction is a person's change to a fix. PRURL is the pull request to link (link_pr). RowVersion,
// when set, is the version the person read: a fix changed since is refused (ErrFixChanged).
type FixAction struct {
	Action     autofix.Action
	PRURL      string
	RowVersion int
}

// ActOnFix applies a person's change (or an agent token's, within its permissions) to a fix: cancel,
// link or unlink a pull request, mark or unmark the release. The vendor side (closing the issue,
// cancelling the Cursor run) is the caller's, after this commits.
func (s *Store) ActOnFix(ctx context.Context, workspaceID, id string, change FixAction, actor autofix.Actor) (autofix.Fix, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.Fix{}, autofix.ErrFixNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Fix, error) {
		fix, err := getFix(ctx, tx, workspaceID, id, true)
		if err != nil {
			return autofix.Fix{}, err
		}
		if change.RowVersion != 0 && change.RowVersion != fix.RowVersion {
			return autofix.Fix{}, autofix.ErrFixChanged
		}
		to, reason, err := autofix.Act(change.Action, fixCurrent(fix))
		if err != nil {
			return autofix.Fix{}, err
		}
		if to.InProgress() && !fix.State.InProgress() {
			busy, err := newerFixInProgress(ctx, tx, workspaceID, fix)
			if err != nil {
				return autofix.Fix{}, err
			}
			if busy {
				return autofix.Fix{}, autofix.ErrFixInProgress
			}
		}
		data := map[string]any{}
		set := `state = $3, outcome_reason = $4, state_changed_at = now(), row_version = row_version + 1`
		args := []any{workspaceID, id, string(to), optionalText(reason)}
		switch change.Action {
		case autofix.ActionLinkPR:
			number, address, err := autofix.ParsePRURL(fix.RepoFullName, change.PRURL)
			if err != nil {
				return autofix.Fix{}, err
			}
			set += `, pr_number = $5, pr_url = $6, pr_linked_by = 'person'`
			args = append(args, number, address)
			data["pr_url"] = address
		case autofix.ActionUnlinkPR:
			set += `, pr_number = NULL, pr_url = NULL, pr_linked_by = NULL`
			data["pr_url"] = *fix.PRURL
		case autofix.ActionDeployed:
			set += `, deployed_at = now(), deployed_by_agent_id = $5, deployed_by_name = $6`
			args = append(args, optionalID(actor.ID), actor.Name)
		case autofix.ActionUndeployed:
			set += `, deployed_at = NULL, deployed_by_agent_id = NULL, deployed_by_name = NULL`
		}
		if reason != "" {
			data["reason"] = reason
		}
		if _, err := tx.Exec(ctx, `UPDATE fix_attempts SET `+set+` WHERE workspace_id = $1 AND id = $2`, args...); inProgressConflict(err) {
			return autofix.Fix{}, autofix.ErrFixInProgress
		} else if err != nil {
			return autofix.Fix{}, fmt.Errorf("change the fix: %w", err)
		}
		if err := insertFixEvent(ctx, tx, workspaceID, id, string(change.Action), fix.State, to, actor, data, ""); err != nil {
			return autofix.Fix{}, err
		}
		return getFix(ctx, tx, workspaceID, id, false)
	})
}
