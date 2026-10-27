package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/jackc/pgx/v5"
)

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
