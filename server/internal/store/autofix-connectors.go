package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/jackc/pgx/v5"
)

// cursorKeyContext binds a sealed Cursor key to its workspace: copied into another workspace's row it
// does not open.
func cursorKeyContext(workspaceID string) string { return "autofix:cursor:" + workspaceID }

const connectorColumns = `kind, enabled, mode, consent_version, consent_name, consent_at, credential_sealed IS NOT NULL, credential_set_by_name,
	credential_set_at, last_check_at, last_check_result, last_check_message`

func scanConnector(row pgx.Row) (autofix.ConnectorSetting, error) {
	var setting autofix.ConnectorSetting
	var consentVersion, consentName, checkResult *string
	var consentAt, keySetAt, checkAt *time.Time
	var hasKey bool
	var keySetBy, checkMessage *string
	if err := row.Scan(&setting.Kind, &setting.Enabled, &setting.Mode, &consentVersion, &consentName, &consentAt, &hasKey, &keySetBy, &keySetAt,
		&checkAt, &checkResult, &checkMessage); err != nil {
		return autofix.ConnectorSetting{}, err
	}
	if consentVersion != nil && consentAt != nil {
		name := ""
		if consentName != nil {
			name = *consentName
		}
		setting.Consent = &autofix.ConnectorConsent{Version: *consentVersion, Name: name, At: consentAt.UTC()}
	}
	if hasKey && keySetAt != nil {
		setting.Credential = &autofix.ConnectorKey{SetByName: keySetBy, SetAt: keySetAt.UTC()}
	}
	if checkAt != nil && checkResult != nil {
		setting.LastCheck = &autofix.ConnectorCheck{At: checkAt.UTC(), Result: *checkResult, Message: checkMessage}
	}
	return setting, nil
}

func getConnector(ctx context.Context, tx pgx.Tx, workspaceID string, kind autofix.Connector) (autofix.ConnectorSetting, error) {
	setting, err := scanConnector(tx.QueryRow(ctx, `SELECT `+connectorColumns+` FROM autofix_connectors WHERE workspace_id = $1 AND kind = $2`,
		workspaceID, string(kind)))
	if errors.Is(err, pgx.ErrNoRows) {
		// Never turned on: off, no consent, no key.
		return autofix.ConnectorSetting{Kind: kind}, nil
	}
	if err != nil {
		return autofix.ConnectorSetting{}, fmt.Errorf("read the coding agent: %w", err)
	}
	return setting, nil
}

// ListAutofixConnectors returns the workspace's three coding agents in the settings' order, each off
// until an admin turns it on. Never a key.
func (s *Store) ListAutofixConnectors(ctx context.Context, workspaceID string) ([]autofix.ConnectorSetting, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]autofix.ConnectorSetting, error) {
		settings := make([]autofix.ConnectorSetting, 0, len(autofix.Connectors))
		for _, kind := range autofix.Connectors {
			setting, err := getConnector(ctx, tx, workspaceID, kind)
			if err != nil {
				return nil, err
			}
			settings = append(settings, setting)
		}
		return settings, nil
	})
}

// GetFixConnector returns one coding agent of the workspace.
func (s *Store) GetFixConnector(ctx context.Context, workspaceID string, kind autofix.Connector) (autofix.ConnectorSetting, error) {
	if !kind.Valid() {
		return autofix.ConnectorSetting{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.ConnectorSetting, error) { return getConnector(ctx, tx, workspaceID, kind) })
}

// EnableFixConnector turns a coding agent on with the admin's consent to the data it receives: the
// version of the text they read (the current one, or ErrConsentOutdated), who and when.
func (s *Store) EnableFixConnector(ctx context.Context, workspaceID string, kind autofix.Connector, consentVersion string, actor autofix.Actor) (autofix.ConnectorSetting, error) {
	if !kind.Valid() {
		return autofix.ConnectorSetting{}, pgx.ErrNoRows
	}
	if consentVersion != autofix.ConsentVersion {
		return autofix.ConnectorSetting{}, autofix.ErrConsentOutdated
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.ConnectorSetting, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO autofix_connectors (workspace_id, kind, enabled, consent_agent_id, consent_name, consent_at, consent_version)
			VALUES ($1, $2, true, $3, $4, now(), $5)
			ON CONFLICT (workspace_id, kind) DO UPDATE SET enabled = true, consent_agent_id = EXCLUDED.consent_agent_id,
				consent_name = EXCLUDED.consent_name, consent_at = EXCLUDED.consent_at, consent_version = EXCLUDED.consent_version, updated_at = now()`,
			workspaceID, string(kind), optionalID(actor.ID), actor.Name, consentVersion); err != nil {
			return autofix.ConnectorSetting{}, fmt.Errorf("turn the coding agent on: %w", err)
		}
		return getConnector(ctx, tx, workspaceID, kind)
	})
}

// DisableFixConnector turns a coding agent off; the consent on record and Cursor's key stay, and fixes
// already sent go on.
func (s *Store) DisableFixConnector(ctx context.Context, workspaceID string, kind autofix.Connector) (autofix.ConnectorSetting, error) {
	if !kind.Valid() {
		return autofix.ConnectorSetting{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.ConnectorSetting, error) {
		if _, err := tx.Exec(ctx, `UPDATE autofix_connectors SET enabled = false, updated_at = now() WHERE workspace_id = $1 AND kind = $2`,
			workspaceID, string(kind)); err != nil {
			return autofix.ConnectorSetting{}, fmt.Errorf("turn the coding agent off: %w", err)
		}
		return getConnector(ctx, tx, workspaceID, kind)
	})
}

// SetCursorKey seals and stores the workspace's Cursor key (MAILBOX_TOKEN_KEYS, bound to the workspace)
// and forgets the last check, which was of the key before. Not one character of it is kept in clear.
func (s *Store) SetCursorKey(ctx context.Context, workspaceID, key string, actor autofix.Actor) (autofix.ConnectorSetting, error) {
	if !s.sealKeys.Configured() {
		return autofix.ConnectorSetting{}, ErrSealKeysMissing
	}
	keyID, sealed, err := s.sealKeys.Seal([]byte(key), cursorKeyContext(workspaceID))
	if err != nil {
		return autofix.ConnectorSetting{}, err
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.ConnectorSetting, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO autofix_connectors (workspace_id, kind, credential_key_id, credential_sealed, credential_set_by_agent_id,
				credential_set_by_name, credential_set_at)
			VALUES ($1, 'cursor_api', $2, $3, $4, $5, now())
			ON CONFLICT (workspace_id, kind) DO UPDATE SET credential_key_id = EXCLUDED.credential_key_id, credential_sealed = EXCLUDED.credential_sealed,
				credential_set_by_agent_id = EXCLUDED.credential_set_by_agent_id, credential_set_by_name = EXCLUDED.credential_set_by_name,
				credential_set_at = EXCLUDED.credential_set_at, last_check_at = NULL, last_check_result = NULL, last_check_message = NULL, updated_at = now()`,
			workspaceID, keyID, sealed, optionalID(actor.ID), actor.Name); err != nil {
			return autofix.ConnectorSetting{}, fmt.Errorf("store the Cursor key: %w", err)
		}
		return getConnector(ctx, tx, workspaceID, autofix.CursorAPI)
	})
}

// RemoveCursorKey erases the workspace's Cursor key; Cursor takes no new fix until one is set again.
func (s *Store) RemoveCursorKey(ctx context.Context, workspaceID string) (autofix.ConnectorSetting, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.ConnectorSetting, error) {
		if _, err := tx.Exec(ctx, `UPDATE autofix_connectors SET credential_key_id = NULL, credential_sealed = NULL, credential_set_by_agent_id = NULL,
				credential_set_by_name = NULL, credential_set_at = NULL, last_check_at = NULL, last_check_result = NULL, last_check_message = NULL, updated_at = now()
			WHERE workspace_id = $1 AND kind = 'cursor_api'`, workspaceID); err != nil {
			return autofix.ConnectorSetting{}, fmt.Errorf("remove the Cursor key: %w", err)
		}
		return getConnector(ctx, tx, workspaceID, autofix.CursorAPI)
	})
}

// OpenCursorKey opens the workspace's Cursor key, or ErrCursorKeyMissing.
func (s *Store) OpenCursorKey(ctx context.Context, workspaceID string) (string, error) {
	type sealedKey struct {
		keyID *string
		value []byte
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (sealedKey, error) {
		var row sealedKey
		err := tx.QueryRow(ctx, `SELECT credential_key_id, credential_sealed FROM autofix_connectors WHERE workspace_id = $1 AND kind = 'cursor_api'`,
			workspaceID).Scan(&row.keyID, &row.value)
		if errors.Is(err, pgx.ErrNoRows) {
			return row, nil
		}
		return row, err
	})
	if err != nil {
		return "", fmt.Errorf("read the Cursor key: %w", err)
	}
	if got.keyID == nil || got.value == nil {
		return "", autofix.ErrCursorKeyMissing
	}
	plain, err := s.sealKeys.Open(*got.keyID, got.value, cursorKeyContext(workspaceID))
	if err != nil {
		return "", fmt.Errorf("open the Cursor key: %w", err)
	}
	return string(plain), nil
}

// RecordConnectorCheck keeps the result of a check (autofix.Check…) and its message.
func (s *Store) RecordConnectorCheck(ctx context.Context, workspaceID string, kind autofix.Connector, result, message string) (autofix.ConnectorSetting, error) {
	if !kind.Valid() {
		return autofix.ConnectorSetting{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (autofix.ConnectorSetting, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO autofix_connectors (workspace_id, kind, last_check_at, last_check_result, last_check_message)
			VALUES ($1, $2, now(), $3, $4)
			ON CONFLICT (workspace_id, kind) DO UPDATE SET last_check_at = now(), last_check_result = EXCLUDED.last_check_result,
				last_check_message = EXCLUDED.last_check_message, updated_at = now()`,
			workspaceID, string(kind), result, optionalText(cutRunes(message, 500))); err != nil {
			return autofix.ConnectorSetting{}, fmt.Errorf("record the check: %w", err)
		}
		return getConnector(ctx, tx, workspaceID, kind)
	})
}

// ConnectedRepoNames lists the full names of the workspace's connected repositories that take fixes
// (not paused).
func (s *Store) ConnectedRepoNames(ctx context.Context, workspaceID string) ([]string, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) {
		rows, err := tx.Query(ctx, `SELECT full_name FROM autofix_repos WHERE workspace_id = $1 AND paused_reason IS NULL ORDER BY full_name`, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list connected repositories: %w", err)
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	})
}

// IssueConnectorFacts is what Colliedesk knows, without calling GitHub, of whether the issue connectors
// can work: how many installations are active and the visibility of each repository taking fixes.
type IssueConnectorFacts struct {
	ActiveInstallations int
	NotPrivate          []string
	Repos               int
}

// AutofixIssueFacts reads them.
func (s *Store) AutofixIssueFacts(ctx context.Context, workspaceID string) (IssueConnectorFacts, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IssueConnectorFacts, error) {
		var facts IssueConnectorFacts
		if err := tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM autofix_github_installations WHERE workspace_id = $1 AND status = 'active'),
				(SELECT count(*) FROM autofix_repos WHERE workspace_id = $1 AND paused_reason IS NULL),
				COALESCE((SELECT array_agg(full_name ORDER BY full_name) FROM autofix_repos WHERE workspace_id = $1 AND paused_reason IS NULL AND visibility <> 'private'), '{}')`,
			workspaceID).Scan(&facts.ActiveInstallations, &facts.Repos, &facts.NotPrivate); err != nil {
			return IssueConnectorFacts{}, fmt.Errorf("read the GitHub connection: %w", err)
		}
		return facts, nil
	})
}
