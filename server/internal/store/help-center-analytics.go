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

// analyticsSecretContext binds a sealed GA4 API secret to its help center. The primary keeps the
// context it had before a workspace could have several, so secrets saved then still open.
func analyticsSecretContext(workspaceID, helpCenterID string) string {
	if helpCenterID == "" {
		return "help_center_analytics:ga4_api_secret:" + workspaceID
	}
	return "help_center_analytics:ga4_api_secret:" + workspaceID + ":" + helpCenterID
}

// analyticsCenter resolves help center helpCenterID of the workspace ("" its primary) to its row id,
// and names it "" when it is the primary, so a target is keyed the same whichever way it was asked.
func analyticsCenter(ctx context.Context, tx pgx.Tx, workspaceID, helpCenterID string, ensure bool) (id, key string, err error) {
	id, err = resolveHelpCenter(ctx, tx, workspaceID, helpCenterID, ensure)
	if err != nil || id == "" || helpCenterID == "" {
		return id, "", err
	}
	var primary bool
	if err := tx.QueryRow(ctx, `SELECT primary_help_center FROM help_centers WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&primary); err != nil {
		return "", "", err
	}
	if primary {
		return id, "", nil
	}
	return id, id, nil
}

const analyticsSettingsSQL = `SELECT h.provider, h.enabled, h.ga4_measurement_id, h.ga4_api_secret_sealed IS NOT NULL, h.plausible_domain,
	h.plausible_base_url, h.last_sent_at, h.last_error, h.last_error_at, h.updated_at, COALESCE(NULLIF(a.display_name,''),a.email)
	FROM help_center_analytics h LEFT JOIN agents a ON a.workspace_id=h.workspace_id AND a.id=h.updated_by_agent_id
	WHERE h.workspace_id=$1 AND h.help_center_id=$2`

func readAnalyticsSettings(ctx context.Context, tx pgx.Tx, workspaceID, centerID string) (hcanalytics.Settings, error) {
	settings := hcanalytics.Settings{BaseURL: hcanalytics.DefaultPlausibleBase}
	if centerID == "" {
		return settings, nil
	}
	err := tx.QueryRow(ctx, analyticsSettingsSQL, workspaceID, centerID).Scan(&settings.Provider, &settings.Enabled, &settings.MeasurementID,
		&settings.APISecretSet, &settings.Domain, &settings.BaseURL, &settings.LastSentAt, &settings.LastError, &settings.LastErrorAt,
		&settings.UpdatedAt, &settings.UpdatedByName)
	if errors.Is(err, pgx.ErrNoRows) {
		return hcanalytics.Settings{BaseURL: hcanalytics.DefaultPlausibleBase}, nil
	}
	if err != nil {
		return hcanalytics.Settings{}, fmt.Errorf("read the help center analytics: %w", err)
	}
	for _, at := range []*time.Time{settings.LastSentAt, settings.LastErrorAt, settings.UpdatedAt} {
		if at != nil {
			*at = at.UTC()
		}
	}
	return settings, nil
}

// unopenableSecret is the problem of a target whose sealed GA4 API secret no key of this server opens.
const unopenableSecret = "The saved GA4 API secret cannot be opened on this server: enter it again and save."

// GetHelpCenterAnalytics returns the analytics settings of the workspace's primary help center.
func (s *Store) GetHelpCenterAnalytics(ctx context.Context, workspaceID string) (hcanalytics.Settings, error) {
	return s.GetHelpCenterAnalyticsIn(ctx, workspaceID, "")
}

// GetHelpCenterAnalyticsIn returns the analytics settings of one help center ("" the primary); before
// the first save, no provider and Plausible's hosted address. An id that is not a live help center of
// the workspace gives domain.ErrHelpCenterNotFound.
func (s *Store) GetHelpCenterAnalyticsIn(ctx context.Context, workspaceID, helpCenterID string) (hcanalytics.Settings, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (hcanalytics.Settings, error) {
		centerID, _, err := analyticsCenter(ctx, tx, workspaceID, helpCenterID, false)
		if err != nil {
			return hcanalytics.Settings{}, err
		}
		return readAnalyticsSettings(ctx, tx, workspaceID, centerID)
	})
}

// PutHelpCenterAnalytics saves the analytics of the workspace's primary help center.
func (s *Store) PutHelpCenterAnalytics(ctx context.Context, workspaceID, actorID string, change hcanalytics.Change) (hcanalytics.Settings, error) {
	settings, _, err := s.PutHelpCenterAnalyticsIn(ctx, workspaceID, "", actorID, change)
	return settings, err
}

// PutHelpCenterAnalyticsIn saves a change already cleaned by hcanalytics.Clean for one help center
// ("" the primary, made on the first save) and returns the settings with the key the sender knows the
// help center by ("" for the primary). A non-empty API secret is sealed (ErrSealKeysMissing without
// MAILBOX_TOKEN_KEYS), "" removes it and nil keeps it. GA4 turned on without a secret is refused with
// hcanalytics.SecretMissing and nothing is saved. A save forgets the send results, which described
// the settings before it.
func (s *Store) PutHelpCenterAnalyticsIn(ctx context.Context, workspaceID, helpCenterID, actorID string, change hcanalytics.Change) (hcanalytics.Settings, string, error) {
	keep := change.APISecret == nil
	if !keep && *change.APISecret != "" && !s.sealKeys.Configured() {
		return hcanalytics.Settings{}, "", ErrSealKeysMissing
	}
	var key string
	settings, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (hcanalytics.Settings, error) {
		centerID, centerKey, err := analyticsCenter(ctx, tx, workspaceID, helpCenterID, true)
		if err != nil {
			return hcanalytics.Settings{}, err
		}
		key = centerKey
		keyID, sealed := (*string)(nil), []byte(nil)
		if !keep && *change.APISecret != "" {
			id, value, err := s.sealKeys.Seal([]byte(*change.APISecret), analyticsSecretContext(workspaceID, centerKey))
			if err != nil {
				return hcanalytics.Settings{}, err
			}
			keyID, sealed = &id, value
		}
		var secretSet bool
		err = tx.QueryRow(ctx, `INSERT INTO help_center_analytics AS h (workspace_id, help_center_id, provider, enabled, ga4_measurement_id,
				ga4_api_secret_sealed, ga4_api_secret_key_id, plausible_domain, plausible_base_url, updated_by_agent_id)
			VALUES ($1, $11, $2, $3, NULLIF($4,''), $5, $6, NULLIF($7,''), $8, $9)
			ON CONFLICT (workspace_id, help_center_id) DO UPDATE SET provider=EXCLUDED.provider, enabled=EXCLUDED.enabled,
				ga4_measurement_id=EXCLUDED.ga4_measurement_id,
				ga4_api_secret_sealed=CASE WHEN $10 THEN h.ga4_api_secret_sealed ELSE EXCLUDED.ga4_api_secret_sealed END,
				ga4_api_secret_key_id=CASE WHEN $10 THEN h.ga4_api_secret_key_id ELSE EXCLUDED.ga4_api_secret_key_id END,
				plausible_domain=EXCLUDED.plausible_domain, plausible_base_url=EXCLUDED.plausible_base_url,
				last_sent_at=NULL, last_error=NULL, last_error_at=NULL, updated_at=now(), updated_by_agent_id=EXCLUDED.updated_by_agent_id
			RETURNING h.ga4_api_secret_sealed IS NOT NULL`,
			workspaceID, change.Provider, change.Enabled, change.MeasurementID, sealed, keyID, change.Domain, change.BaseURL,
			optionalID(actorID), keep, centerID).Scan(&secretSet)
		if err != nil {
			return hcanalytics.Settings{}, fmt.Errorf("save the help center analytics: %w", err)
		}
		if change.Enabled && change.Provider == hcanalytics.GA4 && !secretSet {
			return hcanalytics.Settings{}, hcanalytics.SecretMissing()
		}
		return readAnalyticsSettings(ctx, tx, workspaceID, centerID)
	})
	return settings, key, err
}

// HelpCenterAnalyticsTarget returns the saved target of the workspace's primary help center.
func (s *Store) HelpCenterAnalyticsTarget(ctx context.Context, workspaceID string) (*hcanalytics.Target, error) {
	return s.HelpCenterAnalyticsTargetIn(ctx, workspaceID, "")
}

// HelpCenterAnalyticsTargetIn returns one help center's saved target ("" the primary) with its API
// secret opened, enabled or not, or nil before the first save. Its HelpCenterID is "" for the primary
// however it was named. A secret no key of this server opens gives a target with only its Problem
// and Version set; an id that is not a live help center of the workspace, domain.ErrHelpCenterNotFound.
func (s *Store) HelpCenterAnalyticsTargetIn(ctx context.Context, workspaceID, helpCenterID string) (*hcanalytics.Target, error) {
	type row struct {
		target        hcanalytics.Target
		keyID         *string
		sealed        []byte
		measurementID *string
		domain        *string
		updatedAt     time.Time
	}
	got, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (*row, error) {
		centerID, key, err := analyticsCenter(ctx, tx, workspaceID, helpCenterID, false)
		if err != nil || centerID == "" {
			return nil, err
		}
		r := row{target: hcanalytics.Target{WorkspaceID: workspaceID, HelpCenterID: key}}
		err = tx.QueryRow(ctx, `SELECT provider, enabled, ga4_measurement_id, ga4_api_secret_key_id, ga4_api_secret_sealed, plausible_domain,
				plausible_base_url, updated_at FROM help_center_analytics WHERE workspace_id=$1 AND help_center_id=$2`, workspaceID, centerID).Scan(
			&r.target.Provider, &r.target.Enabled, &r.measurementID, &r.keyID, &r.sealed, &r.domain, &r.target.BaseURL, &r.updatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return &r, err
	})
	if err != nil || got == nil {
		return nil, helpCenterStoreError("read the help center analytics target", err)
	}
	target := got.target
	target.Version = got.updatedAt.UnixMicro()
	if got.measurementID != nil {
		target.MeasurementID = *got.measurementID
	}
	if got.domain != nil {
		target.Domain = *got.domain
	}
	if got.keyID != nil && got.sealed != nil {
		plain, err := s.sealKeys.Open(*got.keyID, got.sealed, analyticsSecretContext(workspaceID, target.HelpCenterID))
		if err != nil {
			slog.Warn("help center analytics: the GA4 API secret cannot be opened", "workspace", workspaceID, "help_center", target.HelpCenterID, "error", err)
			return &hcanalytics.Target{WorkspaceID: workspaceID, HelpCenterID: target.HelpCenterID, Enabled: target.Enabled,
				Version: target.Version, Problem: unopenableSecret}, nil
		}
		target.APISecret = string(plain)
	}
	return &target, nil
}

// HelpCenterAnalyticsTargets returns the enabled target of every live help center. One whose target
// cannot be read is logged and listed with only its Problem, so it does not stop the others and the
// sender keeps the target it already has.
func (s *Store) HelpCenterAnalyticsTargets(ctx context.Context) ([]hcanalytics.Target, error) {
	type center struct{ WorkspaceID, HelpCenterID string }
	var centers []center
	err := s.db.Privileged(ctx, database.PrivilegeHelpCenterAnalytics, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT a.workspace_id::text, CASE WHEN c.primary_help_center THEN '' ELSE c.id::text END
			FROM help_center_analytics a JOIN help_centers c ON c.workspace_id=a.workspace_id AND c.id=a.help_center_id
			WHERE a.enabled AND c.deleted_at IS NULL`)
		if err != nil {
			return err
		}
		centers, err = pgx.CollectRows(rows, pgx.RowToStructByPos[center])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list the help centers sending analytics: %w", err)
	}
	targets := make([]hcanalytics.Target, 0, len(centers))
	for _, c := range centers {
		target, err := s.HelpCenterAnalyticsTargetIn(ctx, c.WorkspaceID, c.HelpCenterID)
		if err != nil {
			slog.Warn("help center analytics: a target cannot be read", "workspace", c.WorkspaceID, "help_center", c.HelpCenterID, "error", err)
			targets = append(targets, hcanalytics.Target{WorkspaceID: c.WorkspaceID, HelpCenterID: c.HelpCenterID,
				Problem: "The analytics settings could not be read; the next reload tries again."})
			continue
		}
		if target != nil && target.Enabled {
			targets = append(targets, *target)
		}
	}
	return targets, nil
}

// RecordHelpCenterAnalyticsResults writes, in one statement for every help center, when a page view
// was last accepted and when and why one was last refused. A result applies only while the settings
// it was gathered for (its Version, their update time) are still the saved ones.
func (s *Store) RecordHelpCenterAnalyticsResults(ctx context.Context, results []hcanalytics.Result) error {
	if len(results) == 0 {
		return nil
	}
	workspaces, centers, versions := make([]string, len(results)), make([]string, len(results)), make([]time.Time, len(results))
	sent, failures, failed := make([]*time.Time, len(results)), make([]*string, len(results)), make([]*time.Time, len(results))
	for index, result := range results {
		workspaces[index], centers[index] = result.WorkspaceID, result.HelpCenterID
		versions[index] = time.UnixMicro(result.Version).UTC()
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
			FROM unnest($1::uuid[], $2::timestamptz[], $3::text[], $4::timestamptz[], $5::timestamptz[], $6::text[])
				AS r(workspace_id, sent_at, error, error_at, version, help_center)
			WHERE h.workspace_id=r.workspace_id
				AND h.help_center_id=COALESCE(NULLIF(r.help_center,'')::uuid, primary_help_center_id(h.workspace_id))
				AND h.updated_at=r.version`, workspaces, sent, failures, failed, versions, centers)
		return err
	})
	if err != nil {
		return fmt.Errorf("record the help center analytics results: %w", err)
	}
	return nil
}
