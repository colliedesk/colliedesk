package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/jackc/pgx/v5"
)

// The issue connectors send a fix as the person: after their GitHub authorization, a goroutine holding
// their token in memory only checks, creates the issue and records it here. Nothing of the token is
// ever stored.

// ErrDispatchStarted: the fix's issue is being created, or was, by an earlier authorization; a second one
// never creates another.
var ErrDispatchStarted = errors.New("this fix is being sent already")

// ErrFixConnectorOff: the fix's coding agent was turned off (or its consent is to an older text) before
// the fix left; it is cancelled connector_disabled.
var ErrFixConnectorOff = errors.New("the coding agent was turned off before the fix left")

// FixDispatch is what sending a fix to GitHub needs: the fix, exactly what it sends and where.
type FixDispatch struct {
	autofix.Fix
	SentBody          string
	DispatchStartedAt *time.Time
	// The connected repository now: its GitHub id and installation (nil once it left the App).
	GitHubRepoID   *int64
	InstallationID *int64
	// CopilotMode is assign_api or assign_on_github for Copilot, "" for the others.
	CopilotMode string
}

// GetFixDispatch reads a fix to send.
func (s *Store) GetFixDispatch(ctx context.Context, workspaceID, id string) (FixDispatch, error) {
	if !savedReplyID.MatchString(id) {
		return FixDispatch{}, autofix.ErrFixNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (FixDispatch, error) { return getFixDispatch(ctx, tx, workspaceID, id, false) })
}

func getFixDispatch(ctx context.Context, tx pgx.Tx, workspaceID, id string, lock bool) (FixDispatch, error) {
	fix, err := getFix(ctx, tx, workspaceID, id, lock)
	if err != nil {
		return FixDispatch{}, err
	}
	work := FixDispatch{Fix: fix}
	var mode *string
	if err := tx.QueryRow(ctx, `SELECT f.sent_body, f.dispatch_started_at, r.github_repo_id, r.installation_id, c.mode
		FROM fix_attempts f LEFT JOIN autofix_repos r ON r.workspace_id = f.workspace_id AND r.id = f.repo_id
		LEFT JOIN autofix_connectors c ON c.workspace_id = f.workspace_id AND c.kind = f.connector
		WHERE f.workspace_id = $1 AND f.id = $2`, workspaceID, id).Scan(&work.SentBody, &work.DispatchStartedAt, &work.GitHubRepoID, &work.InstallationID, &mode); err != nil {
		return FixDispatch{}, fmt.Errorf("read the fix to send: %w", err)
	}
	if mode != nil {
		work.CopilotMode = *mode
	}
	return work, nil
}

// StartFixDispatch marks the moment the fix's issue is about to be created (dispatch_started_at), so a
// process that dies on the way leaves a fix the expiry looks for on GitHub instead of sending again. It
// refuses a fix no longer sending (ErrMove), one started already (ErrDispatchStarted), and ends a fix
// whose coding agent is off or whose consent is to an older text (cancelled connector_disabled,
// ErrFixConnectorOff): what was consented to is what may leave.
func (s *Store) StartFixDispatch(ctx context.Context, workspaceID, id string, now time.Time) (FixDispatch, error) {
	if !savedReplyID.MatchString(id) {
		return FixDispatch{}, autofix.ErrFixNotFound
	}
	type started struct {
		work FixDispatch
		off  bool
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (started, error) {
		work, err := getFixDispatch(ctx, tx, workspaceID, id, true)
		if err != nil {
			return started{}, err
		}
		if work.State != autofix.Sending {
			return started{}, fmt.Errorf("%w: the fix is %s", autofix.ErrMove, work.State)
		}
		if work.DispatchStartedAt != nil {
			return started{}, ErrDispatchStarted
		}
		connector, err := getConnector(ctx, tx, workspaceID, work.Connector)
		if err != nil {
			return started{}, err
		}
		if !connector.Enabled || connector.Consent == nil || connector.Consent.Version != autofix.ConsentVersion {
			_, err := applyFixObservation(ctx, tx, workspaceID, id, autofix.Observation{State: autofix.Cancelled, Reason: "connector_disabled", Kind: "connector_disabled"},
				autofix.Actor{Kind: autofix.ActorSystem, Name: "Colliedesk"})
			return started{off: true}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE fix_attempts SET dispatch_started_at = $3 WHERE workspace_id = $1 AND id = $2`, workspaceID, id, now); err != nil {
			return started{}, fmt.Errorf("start sending the fix: %w", err)
		}
		work.DispatchStartedAt = &now
		return started{work: work}, nil
	})
	if err != nil {
		return FixDispatch{}, err
	}
	if result.off {
		return FixDispatch{}, ErrFixConnectorOff
	}
	return result.work, nil
}

// IssueCreated is the issue GitHub created for a fix, and who created it.
type IssueCreated struct {
	Number       int
	NodeID       string
	URL          string
	GitHubUserID int64
	GitHubLogin  string
	// CopilotAssignment is assigned or on_github for Copilot, "" for Claude.
	CopilotAssignment string
	// Summary is a line for the timeline (the @claude comment failed, Copilot left to assign on GitHub).
	Summary string
}

// RecordFixIssue records the issue of a fix and moves it to sent. The issue is stored even when the fix
// ended meanwhile (a person cancelled it while it was being created), so its address shows and the
// caller can close it; the move is then only noted. It returns the fix and what the move did.
func (s *Store) RecordFixIssue(ctx context.Context, workspaceID, id string, issue IssueCreated, actor autofix.Actor) (autofix.Fix, autofix.Result, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.Fix{}, autofix.Ignore, autofix.ErrFixNotFound
	}
	out, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (applied, error) {
		fix, err := getFix(ctx, tx, workspaceID, id, true)
		if err != nil {
			return applied{}, err
		}
		if fix.IssueNumber == nil {
			var userID *int64
			var login *string
			if issue.GitHubLogin != "" && issue.GitHubUserID > 0 {
				userID, login = &issue.GitHubUserID, &issue.GitHubLogin
			}
			if _, err := tx.Exec(ctx, `UPDATE fix_attempts SET issue_number = $3, issue_node_id = $4, vendor_url = $5, github_user_id = $6, github_login = $7,
					copilot_assignment = $8 WHERE workspace_id = $1 AND id = $2`,
				workspaceID, id, issue.Number, optionalText(issue.NodeID), optionalText(issue.URL), userID, login, optionalText(issue.CopilotAssignment)); err != nil {
				return applied{}, fmt.Errorf("record the fix's issue: %w", err)
			}
		}
		return applyFixObservation(ctx, tx, workspaceID, id, autofix.Observation{State: autofix.Sent, Kind: "issue_created", IssueNumber: issue.Number,
			IssueNodeID: issue.NodeID, VendorURL: issue.URL, Summary: issue.Summary}, actor)
	})
	if err != nil {
		return autofix.Fix{}, autofix.Ignore, err
	}
	return out.fix, out.result, nil
}

// SendingIssueFix is a fix of an issue connector still sending: when it was created, and whether its
// issue was being created.
type SendingIssueFix struct {
	ID                string
	Connector         autofix.Connector
	CreatedAt         time.Time
	DispatchStartedAt *time.Time
	RepoFullName      string
	GitHubRepoID      *int64
	InstallationID    *int64
}

// SendingIssueFixes lists the workspace's fixes of the issue connectors still sending and created before
// the time given, oldest first.
func (s *Store) SendingIssueFixes(ctx context.Context, workspaceID string, before time.Time, limit int) ([]SendingIssueFix, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]SendingIssueFix, error) {
		rows, err := tx.Query(ctx, `SELECT f.id::text, f.connector, f.created_at, f.dispatch_started_at, COALESCE(r.full_name, f.repo_full_name), r.github_repo_id, r.installation_id
			FROM fix_attempts f LEFT JOIN autofix_repos r ON r.workspace_id = f.workspace_id AND r.id = f.repo_id
			WHERE f.workspace_id = $1 AND f.state = 'sending' AND f.connector IN ('claude_issue','copilot_issue') AND f.created_at < $2
			ORDER BY f.created_at LIMIT $3`, workspaceID, before, limit)
		if err != nil {
			return nil, fmt.Errorf("list fixes still sending: %w", err)
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (SendingIssueFix, error) {
			var item SendingIssueFix
			err := row.Scan(&item.ID, &item.Connector, &item.CreatedAt, &item.DispatchStartedAt, &item.RepoFullName, &item.GitHubRepoID, &item.InstallationID)
			return item, err
		})
	})
}
