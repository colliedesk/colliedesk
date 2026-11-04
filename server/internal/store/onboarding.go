package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// OnboardingGoal returns the goal the workspace gave at cloud sign-up, or nil when it gave none or was
// not created by cloud sign-up.
func (s *Store) OnboardingGoal(ctx context.Context, workspaceID string) (*string, error) {
	var goal *string
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT goal FROM workspace_onboarding WHERE workspace_id=$1`, workspaceID).Scan(&goal)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read onboarding goal: %w", err)
	}
	return goal, nil
}

// SignupAttribution returns the post, ad or site page the workspace's cloud sign-up came from; empty when
// it had none or the workspace was not created by cloud sign-up.
func (s *Store) SignupAttribution(ctx context.Context, workspaceID string) (domain.Attribution, error) {
	var found domain.Attribution
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var seen *time.Time
		err := tx.QueryRow(ctx, `SELECT COALESCE(utm_source,''), COALESCE(utm_medium,''), COALESCE(utm_campaign,''), COALESCE(utm_content,''),
			COALESCE(utm_term,''), COALESCE(ttclid,''), COALESCE(fbclid,''), click_id_seen_at, COALESCE(landing_path,'') FROM workspace_onboarding WHERE workspace_id=$1`, workspaceID).
			Scan(&found.UTMSource, &found.UTMMedium, &found.UTMCampaign, &found.UTMContent, &found.UTMTerm, &found.TTCLID, &found.FBCLID, &seen, &found.LandingPath)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if seen != nil {
			found.ClickIDSeenAt = seen.UTC()
		}
		return err
	})
	if err != nil {
		return domain.Attribution{}, fmt.Errorf("read sign-up attribution: %w", err)
	}
	return found, nil
}
