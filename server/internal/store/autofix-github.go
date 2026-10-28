package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The GitHub side of Autofix: the workspace's App installations and connected repositories, the GitHub
// authorizations in flight and the lookups the webhook and the reconcile pass need. GitHub is never
// called from here; the API and the sync service call it outside any transaction.

const (
	installationColumns = `id::text, installation_id, account_login, account_type, repository_selection, status, connected_by_name, connected_at`
	repoColumns         = `id::text, installation_id, github_repo_id, full_name, visibility, visibility_checked_at, default_branch, include_notes,
		default_connector, paused_reason, paused_at, connected_by_name, connected_at`
	// inProgress are the states that hold a conversation's one fix in progress.
	inProgress = `('sending','sent','working','pr_open')`
)

func scanInstallation(row pgx.Row) (autofix.Installation, error) {
	var i autofix.Installation
	if err := row.Scan(&i.ID, &i.InstallationID, &i.AccountLogin, &i.AccountType, &i.RepositorySelection, &i.Status, &i.ConnectedByName, &i.ConnectedAt); err != nil {
		return autofix.Installation{}, err
	}
	i.ConnectedAt = i.ConnectedAt.UTC()
	return i, nil
}

func scanRepo(row pgx.Row) (autofix.Repo, error) {
	var r autofix.Repo
	if err := row.Scan(&r.ID, &r.InstallationID, &r.GitHubRepoID, &r.FullName, &r.Visibility, &r.VisibilityCheckedAt, &r.DefaultBranch, &r.IncludeNotes,
		&r.DefaultConnector, &r.PausedReason, &r.PausedAt, &r.ConnectedByName, &r.ConnectedAt); err != nil {
		return autofix.Repo{}, err
	}
	r.VisibilityCheckedAt, r.ConnectedAt = r.VisibilityCheckedAt.UTC(), r.ConnectedAt.UTC()
	if r.PausedAt != nil {
		at := r.PausedAt.UTC()
		r.PausedAt = &at
	}
	return r, nil
}

func collect[T any](rows pgx.Rows, err error, scan func(pgx.Row) (T, error)) ([]T, error) {
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
	if items == nil {
		items = []T{}
	}
	return items, err
}

// AutofixSettings reads the workspace's installations, repositories and coding agents (every kind, off
// when never turned on). Available and the App's slug are the caller's.
func (s *Store) AutofixSettings(ctx context.Context, workspaceID string) (autofix.Settings, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Settings, error) {
		settings := autofix.Settings{ConsentVersion: autofix.ConsentVersion}
		var err error
		rows, err := tx.Query(ctx, `SELECT `+installationColumns+` FROM autofix_github_installations WHERE workspace_id = $1 ORDER BY connected_at, id`, workspaceID)
		if settings.Installations, err = collect(rows, err, scanInstallation); err != nil {
			return settings, fmt.Errorf("read installations: %w", err)
		}
		rows, err = tx.Query(ctx, `SELECT `+repoColumns+` FROM autofix_repos WHERE workspace_id = $1 ORDER BY lower(full_name), id`, workspaceID)
		if settings.Repos, err = collect(rows, err, scanRepo); err != nil {
			return settings, fmt.Errorf("read repositories: %w", err)
		}
		if settings.Connectors, err = listAutofixConnectors(ctx, tx, workspaceID); err != nil {
			return settings, err
		}
		return settings, nil
	})
}

func listAutofixConnectors(ctx context.Context, tx pgx.Tx, workspaceID string) ([]autofix.ConnectorSetting, error) {
	settings := make([]autofix.ConnectorSetting, 0, len(autofix.Connectors))
	for _, kind := range autofix.Connectors {
		setting, err := getConnector(ctx, tx, workspaceID, kind)
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}
	return settings, nil
}

// NewGitHubInstallation is an installation a person connected, as GitHub described it to them.
type NewGitHubInstallation struct {
	InstallationID      int64
	AccountLogin        string
	AccountType         string
	RepositorySelection string
	Suspended           bool
	AgentID, AgentName  string
}

// SaveGitHubInstallation connects an installation to the workspace, or refreshes it when the workspace
// has it already. An installation belongs to one workspace on the server: another workspace's is
// ErrInstallationInUse (the unique key sees across workspaces; nothing of the other row is read).
func (s *Store) SaveGitHubInstallation(ctx context.Context, workspaceID string, in NewGitHubInstallation) (autofix.Installation, error) {
	status := autofix.InstallationStatus(in.RepositorySelection, in.Suspended)
	installation, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Installation, error) {
		installation, err := scanInstallation(tx.QueryRow(ctx, `UPDATE autofix_github_installations SET account_login = $3, account_type = $4,
				repository_selection = $5, status = $6, connected_by_agent_id = $7, connected_by_name = $8, connected_at = now(), updated_at = now()
			WHERE workspace_id = $1 AND installation_id = $2 RETURNING `+installationColumns,
			workspaceID, in.InstallationID, in.AccountLogin, in.AccountType, in.RepositorySelection, status, optionalID(in.AgentID), in.AgentName))
		if errors.Is(err, pgx.ErrNoRows) {
			installation, err = scanInstallation(tx.QueryRow(ctx, `INSERT INTO autofix_github_installations (workspace_id, installation_id, account_login,
					account_type, repository_selection, status, connected_by_agent_id, connected_by_name)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+installationColumns,
				workspaceID, in.InstallationID, in.AccountLogin, in.AccountType, in.RepositorySelection, status, optionalID(in.AgentID), in.AgentName))
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == "autofix_github_installations_installation_id_key" {
			return autofix.Installation{}, autofix.ErrInstallationInUse
		}
		if err != nil {
			return autofix.Installation{}, fmt.Errorf("save the installation: %w", err)
		}
		if err := pauseInstallationRepos(ctx, tx, workspaceID, in.InstallationID, in.Suspended); err != nil {
			return autofix.Installation{}, err
		}
		return installation, nil
	})
	return installation, err
}

// pauseInstallationRepos pauses the installation's repositories while it is suspended and resumes the
// ones its suspension paused once it is not.
func pauseInstallationRepos(ctx context.Context, tx pgx.Tx, workspaceID string, installationID int64, suspended bool) error {
	var err error
	if suspended {
		_, err = tx.Exec(ctx, `UPDATE autofix_repos SET paused_reason = 'installation_suspended', paused_at = now()
			WHERE workspace_id = $1 AND installation_id = $2 AND paused_reason IS NULL`, workspaceID, installationID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE autofix_repos SET paused_reason = NULL, paused_at = NULL
			WHERE workspace_id = $1 AND installation_id = $2 AND paused_reason = 'installation_suspended'`, workspaceID, installationID)
	}
	if err != nil {
		return fmt.Errorf("pause the installation's repositories: %w", err)
	}
	return nil
}

// GetGitHubInstallation reads one installation by its id in Colliedesk.
func (s *Store) GetGitHubInstallation(ctx context.Context, workspaceID, id string) (autofix.Installation, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.Installation{}, autofix.ErrInstallationNotFound
	}
	installation, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Installation, error) {
		return scanInstallation(tx.QueryRow(ctx, `SELECT `+installationColumns+` FROM autofix_github_installations WHERE workspace_id = $1 AND id = $2`, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Installation{}, autofix.ErrInstallationNotFound
	}
	return installation, err
}

// GitHubInstallationByID reads one installation of the workspace by GitHub's id.
func (s *Store) GitHubInstallationByID(ctx context.Context, workspaceID string, installationID int64) (autofix.Installation, error) {
	installation, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Installation, error) {
		return scanInstallation(tx.QueryRow(ctx, `SELECT `+installationColumns+` FROM autofix_github_installations WHERE workspace_id = $1 AND installation_id = $2`,
			workspaceID, installationID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Installation{}, autofix.ErrInstallationNotFound
	}
	return installation, err
}

// SetGitHubInstallationState records what GitHub says of an installation now: its repository selection
// and whether it is suspended. Narrowing it to selected repositories unblocks it; a suspension pauses its
// repositories and lifting it resumes them.
func (s *Store) SetGitHubInstallationState(ctx context.Context, workspaceID string, installationID int64, selection string, suspended bool) (autofix.Installation, error) {
	installation, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Installation, error) {
		installation, err := scanInstallation(tx.QueryRow(ctx, `UPDATE autofix_github_installations SET repository_selection = $3, status = $4, updated_at = now()
			WHERE workspace_id = $1 AND installation_id = $2 RETURNING `+installationColumns,
			workspaceID, installationID, selection, autofix.InstallationStatus(selection, suspended)))
		if err != nil {
			return autofix.Installation{}, err
		}
		return installation, pauseInstallationRepos(ctx, tx, workspaceID, installationID, suspended)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Installation{}, autofix.ErrInstallationNotFound
	}
	return installation, err
}

// RemovedInstallation is what removing an installation left: GitHub's id and the fixes in progress on
// its repositories, which the caller cancels (repo_disconnected) after this commits.
type RemovedInstallation struct {
	InstallationID int64
	FixIDs         []string
}

// RemoveGitHubInstallation removes an installation by its id in Colliedesk; RemoveGitHubInstallationByID
// by GitHub's (the webhook's installation.deleted). Its repositories stay, paused (removed_from_app), so
// their fixes still show where they went.
func (s *Store) RemoveGitHubInstallation(ctx context.Context, workspaceID, id string) (RemovedInstallation, error) {
	if !savedReplyID.MatchString(id) {
		return RemovedInstallation{}, autofix.ErrInstallationNotFound
	}
	return s.removeInstallation(ctx, workspaceID, `id = $2`, id)
}

// RemoveGitHubInstallationByID is RemoveGitHubInstallation by GitHub's id.
func (s *Store) RemoveGitHubInstallationByID(ctx context.Context, workspaceID string, installationID int64) (RemovedInstallation, error) {
	return s.removeInstallation(ctx, workspaceID, `installation_id = $2`, installationID)
}

func (s *Store) removeInstallation(ctx context.Context, workspaceID, where string, key any) (RemovedInstallation, error) {
	removed, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (RemovedInstallation, error) {
		var removed RemovedInstallation
		if err := tx.QueryRow(ctx, `SELECT installation_id FROM autofix_github_installations WHERE workspace_id = $1 AND `+where+` FOR UPDATE`,
			workspaceID, key).Scan(&removed.InstallationID); err != nil {
			return removed, err
		}
		var err error
		if removed.FixIDs, err = pauseRepos(ctx, tx, workspaceID, `installation_id = $2`, removed.InstallationID); err != nil {
			return removed, err
		}
		// The repositories keep their rows; the foreign key clears their installation.
		if _, err := tx.Exec(ctx, `DELETE FROM autofix_github_installations WHERE workspace_id = $1 AND installation_id = $2`, workspaceID, removed.InstallationID); err != nil {
			return removed, fmt.Errorf("remove the installation: %w", err)
		}
		return removed, nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RemovedInstallation{}, autofix.ErrInstallationNotFound
	}
	return removed, err
}

// pauseRepos pauses the matching repositories as removed from the App (keeping an earlier reason) and
// returns the fixes in progress on them.
func pauseRepos(ctx context.Context, tx pgx.Tx, workspaceID, where string, key any) ([]string, error) {
	if _, err := tx.Exec(ctx, `UPDATE autofix_repos SET paused_reason = COALESCE(paused_reason, 'removed_from_app'), paused_at = COALESCE(paused_at, now())
		WHERE workspace_id = $1 AND `+where, workspaceID, key); err != nil {
		return nil, fmt.Errorf("pause repositories: %w", err)
	}
	return fixesInProgress(ctx, tx, workspaceID, `repo_id IN (SELECT id FROM autofix_repos WHERE workspace_id = $1 AND `+where+`)`, key)
}

func fixesInProgress(ctx context.Context, tx pgx.Tx, workspaceID, where string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT id::text FROM fix_attempts WHERE workspace_id = $1 AND state IN `+inProgress+` AND `+where+` ORDER BY created_at`,
		append([]any{workspaceID}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("read fixes in progress: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read fixes in progress: %w", err)
	}
	return ids, nil
}

// RepoSeen is a repository as GitHub shows it now.
type RepoSeen struct {
	GitHubRepoID  int64
	FullName      string
	Visibility    string
	DefaultBranch string
}

// NewAutofixRepo is a repository a person connects.
type NewAutofixRepo struct {
	InstallationID   string // the installation's id in Colliedesk
	Repo             RepoSeen
	DefaultConnector *autofix.Connector
	AgentID          string
	AgentName        string
}

// ConnectAutofixRepo connects a private repository of an active installation, or connects again one
// already there (a paused one resumes with what GitHub shows now).
func (s *Store) ConnectAutofixRepo(ctx context.Context, workspaceID string, in NewAutofixRepo) (autofix.Repo, error) {
	if !savedReplyID.MatchString(in.InstallationID) {
		return autofix.Repo{}, autofix.ErrInstallationNotFound
	}
	if in.Repo.Visibility != "private" {
		return autofix.Repo{}, autofix.ErrRepoNotPrivate
	}
	var connector *string
	if in.DefaultConnector != nil {
		value := string(*in.DefaultConnector)
		connector = &value
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Repo, error) {
		var installationID int64
		var status string
		err := tx.QueryRow(ctx, `SELECT installation_id, status FROM autofix_github_installations WHERE workspace_id = $1 AND id = $2 FOR SHARE`,
			workspaceID, in.InstallationID).Scan(&installationID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return autofix.Repo{}, autofix.ErrInstallationNotFound
		}
		if err != nil {
			return autofix.Repo{}, fmt.Errorf("read the installation: %w", err)
		}
		switch status {
		case autofix.InstallationBlocked:
			return autofix.Repo{}, autofix.ErrInstallationBlocked
		case autofix.InstallationSuspended:
			return autofix.Repo{}, autofix.ErrInstallationSuspended
		}
		repo, err := scanRepo(tx.QueryRow(ctx, `INSERT INTO autofix_repos (workspace_id, installation_id, github_repo_id, full_name, visibility, default_branch,
				default_connector, connected_by_agent_id, connected_by_name)
			VALUES ($1, $2, $3, $4, 'private', $5, $6, $7, $8)
			ON CONFLICT (workspace_id, github_repo_id) DO UPDATE SET installation_id = EXCLUDED.installation_id, full_name = EXCLUDED.full_name,
				visibility = 'private', visibility_checked_at = now(), default_branch = EXCLUDED.default_branch,
				default_connector = COALESCE(EXCLUDED.default_connector, autofix_repos.default_connector), paused_reason = NULL, paused_at = NULL
			RETURNING `+repoColumns,
			workspaceID, installationID, in.Repo.GitHubRepoID, in.Repo.FullName, in.Repo.DefaultBranch, connector, optionalID(in.AgentID), in.AgentName))
		if err != nil {
			return autofix.Repo{}, fmt.Errorf("connect the repository: %w", err)
		}
		return repo, nil
	})
}

// GetAutofixRepo reads one connected repository.
func (s *Store) GetAutofixRepo(ctx context.Context, workspaceID, id string) (autofix.Repo, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.Repo{}, autofix.ErrRepoNotFound
	}
	repo, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Repo, error) {
		return scanRepo(tx.QueryRow(ctx, `SELECT `+repoColumns+` FROM autofix_repos WHERE workspace_id = $1 AND id = $2`, workspaceID, id))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Repo{}, autofix.ErrRepoNotFound
	}
	return repo, err
}

// AutofixRepoByGitHubID reads the connected repository with GitHub's id, ErrRepoNotFound when the
// workspace has not connected it.
func (s *Store) AutofixRepoByGitHubID(ctx context.Context, workspaceID string, githubRepoID int64) (autofix.Repo, error) {
	repo, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Repo, error) {
		return scanRepo(tx.QueryRow(ctx, `SELECT `+repoColumns+` FROM autofix_repos WHERE workspace_id = $1 AND github_repo_id = $2`, workspaceID, githubRepoID))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Repo{}, autofix.ErrRepoNotFound
	}
	return repo, err
}

// ConnectedRepoIDs maps GitHub's repository ids to the workspace's connected repositories.
func (s *Store) ConnectedRepoIDs(ctx context.Context, workspaceID string) (map[int64]string, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (map[int64]string, error) {
		rows, err := tx.Query(ctx, `SELECT github_repo_id, id::text FROM autofix_repos WHERE workspace_id = $1`, workspaceID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		ids := map[int64]string{}
		for rows.Next() {
			var github int64
			var id string
			if err := rows.Scan(&github, &id); err != nil {
				return nil, err
			}
			ids[github] = id
		}
		return ids, rowsError("read connected repositories", rows)
	})
}

// updateRepo changes one repository and returns it.
func (s *Store) updateRepo(ctx context.Context, workspaceID, id, set string, args ...any) (autofix.Repo, error) {
	if !savedReplyID.MatchString(id) {
		return autofix.Repo{}, autofix.ErrRepoNotFound
	}
	repo, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Repo, error) {
		return scanRepo(tx.QueryRow(ctx, `UPDATE autofix_repos SET `+set+` WHERE workspace_id = $1 AND id = $2 RETURNING `+repoColumns,
			append([]any{workspaceID, id}, args...)...))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Repo{}, autofix.ErrRepoNotFound
	}
	return repo, err
}

// SetRepoDefaultConnector changes the coding agent a new fix on the repository suggests (nil for none).
func (s *Store) SetRepoDefaultConnector(ctx context.Context, workspaceID, id string, connector *autofix.Connector) (autofix.Repo, error) {
	var value *string
	if connector != nil {
		text := string(*connector)
		value = &text
	}
	return s.updateRepo(ctx, workspaceID, id, `default_connector = $3`, value)
}

// SetRepoIncludeNotes lets fixes on the repository carry internal notes the sender picks, or stops it.
func (s *Store) SetRepoIncludeNotes(ctx context.Context, workspaceID, id string, include bool) (autofix.Repo, error) {
	return s.updateRepo(ctx, workspaceID, id, `include_notes = $3`, include)
}

// DisconnectAutofixRepo removes a connected repository. Its fixes keep the name they were sent to; the
// ones in progress are returned for the caller to cancel (repo_disconnected) after this commits.
func (s *Store) DisconnectAutofixRepo(ctx context.Context, workspaceID, id string) ([]string, error) {
	if !savedReplyID.MatchString(id) {
		return nil, autofix.ErrRepoNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		var found string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM autofix_repos WHERE workspace_id = $1 AND id = $2 FOR UPDATE`, workspaceID, id).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, autofix.ErrRepoNotFound
			}
			return nil, fmt.Errorf("read the repository: %w", err)
		}
		fixes, err := fixesInProgress(ctx, tx, workspaceID, `repo_id = $2`, id)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM autofix_repos WHERE workspace_id = $1 AND id = $2`, workspaceID, id); err != nil {
			return nil, fmt.Errorf("disconnect the repository: %w", err)
		}
		return fixes, nil
	})
}

// RepoChange is what recording GitHub's view of a repository did: the repository, and the fixes the
// caller must end because of it (InProgress when it left the App, Sending when it turned public).
type RepoChange struct {
	Repo       autofix.Repo
	InProgress []string
	Sending    []string
}

// RecordRepoSeen stores what GitHub shows of a connected repository now: its name, default branch and
// visibility. A repository no longer private is paused (became_public) and its fixes still sending are
// returned; one private again resumes from that pause.
func (s *Store) RecordRepoSeen(ctx context.Context, workspaceID string, seen RepoSeen) (RepoChange, error) {
	change, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (RepoChange, error) {
		var change RepoChange
		repo, err := scanRepo(tx.QueryRow(ctx, `UPDATE autofix_repos SET full_name = COALESCE(NULLIF($3, ''), full_name), visibility = $4,
				default_branch = COALESCE(NULLIF($5, ''), default_branch), visibility_checked_at = now(),
				paused_reason = CASE WHEN $4 <> 'private' THEN COALESCE(paused_reason, 'became_public')
					WHEN paused_reason = 'became_public' THEN NULL ELSE paused_reason END,
				paused_at = CASE WHEN $4 <> 'private' THEN COALESCE(paused_at, now())
					WHEN paused_reason = 'became_public' THEN NULL ELSE paused_at END
			WHERE workspace_id = $1 AND github_repo_id = $2 RETURNING `+repoColumns,
			workspaceID, seen.GitHubRepoID, seen.FullName, seen.Visibility, seen.DefaultBranch))
		if err != nil {
			return change, err
		}
		change.Repo = repo
		if seen.Visibility != "private" {
			change.Sending, err = fixesInProgressInState(ctx, tx, workspaceID, repo.ID, "sending")
		}
		return change, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RepoChange{}, autofix.ErrRepoNotFound
	}
	return change, err
}

func fixesInProgressInState(ctx context.Context, tx pgx.Tx, workspaceID, repoID, state string) ([]string, error) {
	return fixesInProgress(ctx, tx, workspaceID, `repo_id = $2 AND state = $3`, repoID, state)
}

// RepoLeftApp pauses a connected repository GitHub says the App no longer reaches (removed from the
// installation's selection, deleted or transferred) and returns its fixes in progress, which the caller
// cancels (repo_disconnected). ErrRepoNotFound when the workspace has not connected it.
func (s *Store) RepoLeftApp(ctx context.Context, workspaceID string, githubRepoID int64) (RepoChange, error) {
	change, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (RepoChange, error) {
		var change RepoChange
		var err error
		if change.InProgress, err = pauseRepos(ctx, tx, workspaceID, `github_repo_id = $2`, githubRepoID); err != nil {
			return change, err
		}
		change.Repo, err = scanRepo(tx.QueryRow(ctx, `SELECT `+repoColumns+` FROM autofix_repos WHERE workspace_id = $1 AND github_repo_id = $2`, workspaceID, githubRepoID))
		return change, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RepoChange{}, autofix.ErrRepoNotFound
	}
	return change, err
}

// RepoBackInApp resumes a connected repository the installation reaches again (added back to its
// selection, or the App installed again): it takes the installation, and resumes from removed_from_app
// when it is private (turning public pauses it as such). Repositories not connected are skipped.
func (s *Store) RepoBackInApp(ctx context.Context, workspaceID string, installationID int64, seen RepoSeen) (autofix.Repo, error) {
	repo, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Repo, error) {
		return scanRepo(tx.QueryRow(ctx, `UPDATE autofix_repos SET installation_id = $3, full_name = COALESCE(NULLIF($4, ''), full_name),
				visibility = $5, visibility_checked_at = now(), default_branch = COALESCE(NULLIF($6, ''), default_branch),
				paused_reason = CASE WHEN $5 <> 'private' THEN 'became_public' WHEN paused_reason IN ('removed_from_app', 'became_public') THEN NULL ELSE paused_reason END,
				paused_at = CASE WHEN $5 <> 'private' THEN COALESCE(paused_at, now()) WHEN paused_reason IN ('removed_from_app', 'became_public') THEN NULL ELSE paused_at END
			WHERE workspace_id = $1 AND github_repo_id = $2 AND (installation_id IS NULL OR installation_id = $3) RETURNING `+repoColumns,
			workspaceID, seen.GitHubRepoID, installationID, seen.FullName, seen.Visibility, seen.DefaultBranch))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Repo{}, autofix.ErrRepoNotFound
	}
	return repo, err
}

// FixForIssue finds the fix that created issue number on a connected repository; FixForPullRequest the
// fix with that pull request; FixForBranch the fix whose agent works on that branch. The latest attempt
// wins; ok is false when none.
func (s *Store) FixForIssue(ctx context.Context, workspaceID, repoID string, number int) (autofix.Fix, bool, error) {
	return s.fixWhere(ctx, workspaceID, `f.repo_id = $2 AND f.issue_number = $3`, repoID, number)
}

// FixForPullRequest: see FixForIssue.
func (s *Store) FixForPullRequest(ctx context.Context, workspaceID, repoID string, number int) (autofix.Fix, bool, error) {
	return s.fixWhere(ctx, workspaceID, `f.repo_id = $2 AND f.pr_number = $3`, repoID, number)
}

// FixForBranch: see FixForIssue.
func (s *Store) FixForBranch(ctx context.Context, workspaceID, repoID, branch string) (autofix.Fix, bool, error) {
	return s.fixWhere(ctx, workspaceID, `f.repo_id = $2 AND f.branch = $3`, repoID, branch)
}

func (s *Store) fixWhere(ctx context.Context, workspaceID, where string, args ...any) (autofix.Fix, bool, error) {
	fix, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.Fix, error) {
		return scanFix(tx.QueryRow(ctx, `SELECT `+fixColumns+fixFrom+` WHERE f.workspace_id = $1 AND `+where+`
			ORDER BY f.created_at DESC LIMIT 1`, append([]any{workspaceID}, args...)...))
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return autofix.Fix{}, false, nil
	}
	if err != nil {
		return autofix.Fix{}, false, fmt.Errorf("find the fix: %w", err)
	}
	return fix, true, nil
}

// ReconcileItem is a fix the reconcile pass reads from GitHub: one sent and not ended, with its
// repository as connected now.
type ReconcileItem struct {
	Fix            autofix.Fix
	IssueNumber    *int
	GitHubRepoID   *int64
	InstallationID *int64
	RepoPaused     *string
}

// FixesToReconcile lists the workspace's fixes past sending and still in progress, oldest first.
func (s *Store) FixesToReconcile(ctx context.Context, workspaceID string, limit int) ([]ReconcileItem, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]ReconcileItem, error) {
		rows, err := tx.Query(ctx, `SELECT f.id::text, f.issue_number, r.github_repo_id, r.installation_id, r.paused_reason
			FROM fix_attempts f LEFT JOIN autofix_repos r ON r.workspace_id = f.workspace_id AND r.id = f.repo_id
			WHERE f.workspace_id = $1 AND f.state IN ('sent','working','pr_open') ORDER BY f.state_changed_at LIMIT $2`, workspaceID, limit)
		if err != nil {
			return nil, err
		}
		items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReconcileItem, error) {
			var item ReconcileItem
			return item, row.Scan(&item.Fix.ID, &item.IssueNumber, &item.GitHubRepoID, &item.InstallationID, &item.RepoPaused)
		})
		if err != nil {
			return nil, err
		}
		for i := range items {
			if items[i].Fix, err = getFix(ctx, tx, workspaceID, items[i].Fix.ID, false); err != nil {
				return nil, err
			}
		}
		return items, nil
	})
}

// AutofixOAuthState is a GitHub authorization in flight: who started it, for what, and its PKCE verifier.
type AutofixOAuthState struct {
	State    string
	AgentID  string
	Purpose  string // install or dispatch
	FixID    string
	Verifier string
}

// CreateAutofixOAuthState starts a GitHub authorization for the agent: installing the App (purpose
// install) or sending fix fixID as them (dispatch). A random state and PKCE verifier; only the state's
// hash is stored. Expired ones of the workspace are removed on the way, and a person keeps at most a few
// in flight.
func (s *Store) CreateAutofixOAuthState(ctx context.Context, workspaceID, agentID, purpose, fixID string, now, expires time.Time) (AutofixOAuthState, error) {
	state, err := randomURLToken(32)
	if err != nil {
		return AutofixOAuthState{}, err
	}
	verifier, err := randomURLToken(64)
	if err != nil {
		return AutofixOAuthState{}, err
	}
	hash := oauthStateHash(state)
	_, err = scoped(ctx, s, workspaceID, func(tx pgx.Tx) (struct{}, error) {
		if _, err := tx.Exec(ctx, `DELETE FROM autofix_oauth_states WHERE workspace_id = $1 AND expires_at <= $2`, workspaceID, now); err != nil {
			return struct{}{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO autofix_oauth_states (state_hash, workspace_id, agent_id, purpose, fix_attempt_id, code_verifier, expires_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, hash, workspaceID, agentID, purpose, optionalID(fixID), verifier, expires, now); err != nil {
			return struct{}{}, err
		}
		_, err := tx.Exec(ctx, `DELETE FROM autofix_oauth_states WHERE workspace_id = $1 AND agent_id = $2 AND state_hash <> $3 AND state_hash NOT IN (
				SELECT state_hash FROM autofix_oauth_states WHERE workspace_id = $1 AND agent_id = $2 AND state_hash <> $3
				ORDER BY created_at DESC LIMIT $4)`, workspaceID, agentID, hash, maxOAuthStates-1)
		return struct{}{}, err
	})
	if err != nil {
		return AutofixOAuthState{}, fmt.Errorf("record the GitHub authorization state: %w", err)
	}
	return AutofixOAuthState{State: state, AgentID: agentID, Purpose: purpose, FixID: fixID, Verifier: verifier}, nil
}

// TakeAutofixOAuthState uses up a GitHub authorization state of this workspace: ErrOAuthStateNotFound
// when it is unknown, used or expired (an expired one is removed too).
func (s *Store) TakeAutofixOAuthState(ctx context.Context, workspaceID, state string, now time.Time) (AutofixOAuthState, error) {
	type taken struct {
		state AutofixOAuthState
		live  bool
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (taken, error) {
		var result taken
		err := tx.QueryRow(ctx, `DELETE FROM autofix_oauth_states WHERE workspace_id = $1 AND state_hash = $2
			RETURNING agent_id::text, purpose, COALESCE(fix_attempt_id::text, ''), code_verifier, expires_at > $3`,
			workspaceID, oauthStateHash(state), now).Scan(&result.state.AgentID, &result.state.Purpose, &result.state.FixID, &result.state.Verifier, &result.live)
		return result, err
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !result.live) {
		return AutofixOAuthState{}, ErrOAuthStateNotFound
	}
	if err != nil {
		return AutofixOAuthState{}, fmt.Errorf("take the GitHub authorization state: %w", err)
	}
	result.state.State = state
	return result.state, nil
}
