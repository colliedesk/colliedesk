package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/hcanalytics"
	"github.com/jackc/pgx/v5"
)

func analyticsSecretContext(workspaceID string) string {
	return "help_center_analytics:ga4_api_secret:" + workspaceID
}

const analyticsSettingsSQL = `SELECT h.provider, h.enabled, h.ga4_measurement_id, h.ga4_api_secret_sealed IS NOT NULL, h.plausible_domain,
	h.plausible_base_url, h.last_sent_at, h.last_error, h.last_error_at, h.updated_at, COALESCE(NULLIF(a.display_name,''),a.email)
	FROM help_center_analytics h LEFT JOIN agents a ON a.workspace_id=h.workspace_id AND a.id=h.updated_by_agent_id
	WHERE h.workspace_id=$1`

func readAnalyticsSettings(ctx context.Context, tx pgx.Tx, workspaceID string) (hcanalytics.Settings, error) {
	settings := hcanalytics.Settings{BaseURL: hcanalytics.DefaultPlausibleBase}
	err := tx.QueryRow(ctx, analyticsSettingsSQL, workspaceID).Scan(&settings.Provider, &settings.Enabled, &settings.MeasurementID,
		&settings.APISecretSet, &settings.Domain, &settings.BaseURL, &settings.LastSentAt, &settings.LastError, &settings.LastErrorAt,
		&settings.UpdatedAt, &settings.UpdatedByName)
	if errors.Is(err, pgx.ErrNoRows) {
		return hcanalytics.Settings{BaseURL: hcanalytics.DefaultPlausibleBase}, nil
	}
	if err != nil {
		return hcanalytics.Settings{}, fmt.Errorf("read the help center analytics: %w", err)
	}
	return settings, nil
}

// GetHelpCenterAnalytics returns the workspace's analytics settings; before the first save, no
// provider and Plausible's hosted address.
func (s *Store) GetHelpCenterAnalytics(ctx context.Context, workspaceID string) (hcanalytics.Settings, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (hcanalytics.Settings, error) {
		return readAnalyticsSettings(ctx, tx, workspaceID)
	})
}

// PutHelpCenterAnalytics saves a change already cleaned by hcanalytics.Clean. A non-empty API secret
// is sealed (ErrSealKeysMissing without MAILBOX_TOKEN_KEYS), "" removes it and nil keeps it. GA4 turned
// on without a secret is refused with hcanalytics.SecretMissing and nothing is saved. A save forgets
// the send results, which described the settings before it.
func (s *Store) PutHelpCenterAnalytics(ctx context.Context, workspaceID, actorID string, change hcanalytics.Change) (hcanalytics.Settings, error) {
	keep, keyID, sealed := change.APISecret == nil, (*string)(nil), []byte(nil)
	if !keep && *change.APISecret != "" {
		if !s.sealKeys.Configured() {
			return hcanalytics.Settings{}, ErrSealKeysMissing
		}
		id, value, err := s.sealKeys.Seal([]byte(*change.APISecret), analyticsSecretContext(workspaceID))
		if err != nil {
			return hcanalytics.Settings{}, err
		}
		keyID, sealed = &id, value
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (hcanalytics.Settings, error) {
		var secretSet bool
		err := tx.QueryRow(ctx, `INSERT INTO help_center_analytics AS h (workspace_id, provider, enabled, ga4_measurement_id, ga4_api_secret_sealed,
				ga4_api_secret_key_id, plausible_domain, plausible_base_url, updated_by_agent_id)
			VALUES ($1, $2, $3, NULLIF($4,''), $5, $6, NULLIF($7,''), $8, $9)
			ON CONFLICT (workspace_id) DO UPDATE SET provider=EXCLUDED.provider, enabled=EXCLUDED.enabled,
				ga4_measurement_id=EXCLUDED.ga4_measurement_id,
				ga4_api_secret_sealed=CASE WHEN $10 THEN h.ga4_api_secret_sealed ELSE EXCLUDED.ga4_api_secret_sealed END,
				ga4_api_secret_key_id=CASE WHEN $10 THEN h.ga4_api_secret_key_id ELSE EXCLUDED.ga4_api_secret_key_id END,
				plausible_domain=EXCLUDED.plausible_domain, plausible_base_url=EXCLUDED.plausible_base_url,
				last_sent_at=NULL, last_error=NULL, last_error_at=NULL, updated_at=now(), updated_by_agent_id=EXCLUDED.updated_by_agent_id
			RETURNING h.ga4_api_secret_sealed IS NOT NULL`,
			workspaceID, change.Provider, change.Enabled, change.MeasurementID, sealed, keyID, change.Domain, change.BaseURL,
			optionalID(actorID), keep).Scan(&secretSet)
		if err != nil {
			return hcanalytics.Settings{}, fmt.Errorf("save the help center analytics: %w", err)
		}
		if change.Enabled && change.Provider == hcanalytics.GA4 && !secretSet {
			return hcanalytics.Settings{}, hcanalytics.SecretMissing()
		}
		return readAnalyticsSettings(ctx, tx, workspaceID)
	})
}

// HelpCenterAnalyticsTarget returns the workspace's saved target with its API secret opened, enabled
// or not, or nil before the first save.
func (s *Store) HelpCenterAnalyticsTarget(ctx context.Context, workspaceID string) (*hcanalytics.Target, error) {
	type row struct {
		target        hcanalytics.Target
		keyID         *string
		sealed        []byte
		measurementID *string
		domain        *string
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*row, error) {
		r := row{target: hcanalytics.Target{WorkspaceID: workspaceID}}
		err := tx.QueryRow(ctx, `SELECT provider, enabled, ga4_measurement_id, ga4_api_secret_key_id, ga4_api_secret_sealed, plausible_domain,
				plausible_base_url FROM help_center_analytics WHERE workspace_id=$1`, workspaceID).Scan(&r.target.Provider, &r.target.Enabled,
			&r.measurementID, &r.keyID, &r.sealed, &r.domain, &r.target.BaseURL)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return &r, err
	})
	if err != nil || got == nil {
		if err != nil {
			err = fmt.Errorf("read the help center analytics target: %w", err)
		}
		return nil, err
	}
	target := got.target
	if got.measurementID != nil {
		target.MeasurementID = *got.measurementID
	}
	if got.domain != nil {
		target.Domain = *got.domain
	}
	if got.keyID != nil && got.sealed != nil {
		plain, err := s.sealKeys.Open(*got.keyID, got.sealed, analyticsSecretContext(workspaceID))
		if err != nil {
			return nil, fmt.Errorf("open the GA4 API secret: %w", err)
		}
		target.APISecret = string(plain)
	}
	return &target, nil
}

// HelpCenterAnalyticsTargets returns the enabled target of every workspace. A workspace whose target
// cannot be read is logged and left out, so it does not stop the others.
func (s *Store) HelpCenterAnalyticsTargets(ctx context.Context) ([]hcanalytics.Target, error) {
	var workspaces []string
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterAnalytics, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT workspace_id::text FROM help_center_analytics WHERE enabled`)
		if err != nil {
			return err
		}
		workspaces, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list the workspaces sending help center analytics: %w", err)
	}
	targets := make([]hcanalytics.Target, 0, len(workspaces))
	for _, workspaceID := range workspaces {
		target, err := s.HelpCenterAnalyticsTarget(ctx, workspaceID)
		if err != nil {
			slog.Warn("help center analytics: a target cannot be read", "workspace", workspaceID, "error", err)
			continue
		}
		if target != nil && target.Enabled {
			targets = append(targets, *target)
		}
	}
	return targets, nil
}

// RecordHelpCenterAnalyticsResults writes, in one statement for every workspace, when a page view was
// last accepted and when and why one was last refused.
func (s *Store) RecordHelpCenterAnalyticsResults(ctx context.Context, results []hcanalytics.Result) error {
	if len(results) == 0 {
		return nil
	}
	workspaces, sent, failures, failed := make([]string, len(results)), make([]*time.Time, len(results)), make([]*string, len(results)), make([]*time.Time, len(results))
	for index, result := range results {
		workspaces[index] = result.WorkspaceID
		if !result.SentAt.IsZero() {
			sent[index] = &result.SentAt
		}
		if result.Error != "" {
			failures[index], failed[index] = &result.Error, &result.ErrorAt
		}
	}
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterAnalytics, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE help_center_analytics h SET last_sent_at=COALESCE(GREATEST(h.last_sent_at, r.sent_at), h.last_sent_at),
				last_error=COALESCE(r.error, h.last_error), last_error_at=COALESCE(r.error_at, h.last_error_at)
			FROM unnest($1::uuid[], $2::timestamptz[], $3::text[], $4::timestamptz[]) AS r(workspace_id, sent_at, error, error_at)
			WHERE h.workspace_id=r.workspace_id`, workspaces, sent, failures, failed)
		return err
	})
	if err != nil {
		return fmt.Errorf("record the help center analytics results: %w", err)
	}
	return nil
}
