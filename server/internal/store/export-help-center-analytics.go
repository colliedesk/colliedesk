package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
	"github.com/jackc/pgx/v5"
)

// exportHelpCenterAnalytics reads the workspace's analytics target for the help-center line, or nil
// before the first save. Only whether a GA4 API secret is stored leaves the database, never its bytes.
func exportHelpCenterAnalytics(ctx context.Context, tx pgx.Tx, workspaceID string) (*export.HelpCenterAnalytics, error) {
	var record export.HelpCenterAnalytics
	err := tx.QueryRow(ctx, `SELECT provider,enabled,ga4_measurement_id,ga4_api_secret_sealed IS NOT NULL,plausible_domain,plausible_base_url,updated_at
		FROM help_center_analytics WHERE workspace_id=$1 AND help_center_id=primary_help_center_id($1)`, workspaceID).Scan(&record.Provider, &record.Enabled, &record.GA4MeasurementID,
		&record.GA4APISecretSet, &record.PlausibleDomain, &record.PlausibleBaseURL, &record.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("export the help center analytics: %w", err)
	}
	record.UpdatedAt = domain.UTC(record.UpdatedAt)
	return &record, nil
}
