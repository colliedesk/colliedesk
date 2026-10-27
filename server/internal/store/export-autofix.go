package store

import (
	"context"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportAutofix writes the GitHub installations, connected repositories and coding agents (never a key:
// only whether Cursor's is set, by whom and when), every fix with exactly what it sent, and every event
// of their timelines.
func exportAutofix(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	if err := exportRows(ctx, tx, "export Autofix installations", `SELECT id::text, installation_id, account_login, account_type, repository_selection, status,
			connected_by_agent_id::text, connected_by_name, connected_at
		FROM autofix_github_installations WHERE workspace_id = $1 ORDER BY connected_at, id`, workspaceID, func(rows pgx.Rows) error {
		var r export.AutofixInstallation
		if err := rows.Scan(&r.ID, &r.InstallationID, &r.AccountLogin, &r.AccountType, &r.RepositorySelection, &r.Status, &r.ConnectedByAgentID, &r.ConnectedByName, &r.ConnectedAt); err != nil {
			return err
		}
		r.ConnectedAt = domain.UTC(r.ConnectedAt)
		return sink.AutofixInstallation(r)
	}); err != nil {
		return err
	}
	if err := exportRows(ctx, tx, "export Autofix repositories", `SELECT id::text, installation_id, github_repo_id, full_name, visibility, visibility_checked_at,
			default_branch, include_notes, default_connector, paused_reason, paused_at, connected_by_agent_id::text, connected_by_name, connected_at
		FROM autofix_repos WHERE workspace_id = $1 ORDER BY connected_at, id`, workspaceID, func(rows pgx.Rows) error {
		var r export.AutofixRepo
		if err := rows.Scan(&r.ID, &r.InstallationID, &r.GitHubRepoID, &r.FullName, &r.Visibility, &r.VisibilityCheckedAt, &r.DefaultBranch, &r.IncludeNotes,
			&r.DefaultConnector, &r.PausedReason, &r.PausedAt, &r.ConnectedByAgentID, &r.ConnectedByName, &r.ConnectedAt); err != nil {
			return err
		}
		r.VisibilityCheckedAt, r.PausedAt, r.ConnectedAt = domain.UTC(r.VisibilityCheckedAt), domain.UTCOptional(r.PausedAt), domain.UTC(r.ConnectedAt)
		return sink.AutofixRepo(r)
	}); err != nil {
		return err
	}
	if err := exportRows(ctx, tx, "export Autofix connectors", `SELECT kind, enabled, mode, consent_agent_id::text, consent_name, consent_at, consent_version,
			credential_sealed IS NOT NULL, credential_set_by_agent_id::text, credential_set_by_name, credential_set_at, last_check_at, last_check_result, last_check_message
		FROM autofix_connectors WHERE workspace_id = $1 ORDER BY kind`, workspaceID, func(rows pgx.Rows) error {
		var r export.AutofixConnector
		if err := rows.Scan(&r.Kind, &r.Enabled, &r.Mode, &r.ConsentAgentID, &r.ConsentName, &r.ConsentAt, &r.ConsentVersion, &r.CredentialSet, &r.CredentialSetByAgent,
			&r.CredentialSetByName, &r.CredentialSetAt, &r.LastCheckAt, &r.LastCheckResult, &r.LastCheckMessage); err != nil {
			return err
		}
		r.ConsentAt, r.CredentialSetAt, r.LastCheckAt = domain.UTCOptional(r.ConsentAt), domain.UTCOptional(r.CredentialSetAt), domain.UTCOptional(r.LastCheckAt)
		return sink.AutofixConnector(r)
	}); err != nil {
		return err
	}
	if err := exportRows(ctx, tx, "export fixes", `SELECT id::text, conversation_id::text, attempt_no, previous_attempt_id::text, repo_id::text, repo_full_name, connector,
			state, outcome_reason, state_changed_at, sent_title, sent_body, sent_message_ids::text[], sent_notes, created_by_kind, created_by_agent_id::text,
			created_by_name, created_by_token_id::text, approved_request_id::text, github_login, copilot_assignment, issue_number, vendor_ref, vendor_url, branch, pr_number,
			pr_url, pr_linked_by, merge_sha, merged_at, merged_by_login, deployed_at, deployed_by_agent_id::text, deployed_by_name, created_at
		FROM fix_attempts WHERE workspace_id = $1 ORDER BY created_at, id`, workspaceID, func(rows pgx.Rows) error {
		var r export.Fix
		if err := rows.Scan(&r.ID, &r.ConversationID, &r.AttemptNo, &r.PreviousAttemptID, &r.RepoID, &r.RepoFullName, &r.Connector, &r.State, &r.OutcomeReason,
			&r.StateChangedAt, &r.SentTitle, &r.SentBody, &r.SentMessageIDs, &r.SentNotes, &r.CreatedByKind, &r.CreatedByAgentID, &r.CreatedByName,
			&r.CreatedByTokenID, &r.ApprovedRequestID, &r.GitHubLogin, &r.CopilotAssignment, &r.IssueNumber, &r.VendorRef, &r.VendorURL, &r.Branch, &r.PRNumber, &r.PRURL,
			&r.PRLinkedBy, &r.MergeSHA, &r.MergedAt, &r.MergedByLogin, &r.DeployedAt, &r.DeployedByAgentID, &r.DeployedByName, &r.CreatedAt); err != nil {
			return err
		}
		if r.SentMessageIDs == nil {
			r.SentMessageIDs = []string{}
		}
		r.StateChangedAt, r.MergedAt, r.DeployedAt, r.CreatedAt = domain.UTC(r.StateChangedAt), domain.UTCOptional(r.MergedAt), domain.UTCOptional(r.DeployedAt), domain.UTC(r.CreatedAt)
		return sink.Fix(r)
	}); err != nil {
		return err
	}
	return exportRows(ctx, tx, "export fix events", `SELECT id, fix_attempt_id::text, kind, from_state, to_state, actor_kind, actor_id, actor_name, data, created_at
		FROM fix_attempt_events WHERE workspace_id = $1 ORDER BY id`, workspaceID, func(rows pgx.Rows) error {
		var r export.FixEvent
		var data []byte
		if err := rows.Scan(&r.ID, &r.FixID, &r.Kind, &r.FromState, &r.ToState, &r.ActorKind, &r.ActorID, &r.ActorName, &data, &r.CreatedAt); err != nil {
			return err
		}
		r.Data, r.CreatedAt = data, domain.UTC(r.CreatedAt)
		return sink.FixEvent(r)
	})
}

// exportRows runs one query of the export and hands each row to write.
func exportRows(ctx context.Context, tx pgx.Tx, what, query, workspaceID string, write func(pgx.Rows) error) error {
	rows, err := tx.Query(ctx, query, workspaceID)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := write(rows); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	return rowsError(what, rows)
}
