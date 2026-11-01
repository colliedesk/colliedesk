package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

var ErrSetupMove = errors.New("invalid move step or missing prerequisite")

var moveDomainSteps = []string{"custom_domain", "plan", "cname", "search_console"}

// MoveReadiness describes prerequisites without asking DNS, the help center or a billing provider.
type MoveReadiness struct {
	VerificationPresent bool
	Enabled             bool
	Published           bool
	Imported            string
	Verified            bool
	Live                bool
	Paid                bool
	Saved               map[string]string
}

func (s *Store) moveReadiness(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (MoveReadiness, error) {
	var r MoveReadiness
	err := tx.QueryRow(ctx, `SELECT
  COALESCE((SELECT enabled FROM help_centers WHERE workspace_id=$1),false),
  EXISTS(SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND status='published' AND deleted_at IS NULL),
  CASE WHEN EXISTS(SELECT 1 FROM import_jobs WHERE workspace_id=$1 AND kind='import' AND state='finished' AND status='OK') THEN 'done'
   WHEN EXISTS(SELECT 1 FROM import_jobs WHERE workspace_id=$1 AND kind='import' AND state IN ('previewing','previewed','queued','running','retry_wait','waiting_key')) THEN 'running' ELSE 'none' END,
  EXISTS(SELECT 1 FROM help_center_domains WHERE workspace_id=$1 AND verified_at IS NOT NULL),
  EXISTS(SELECT 1 FROM help_center_domains WHERE workspace_id=$1 AND address_checked_at IS NOT NULL AND address_error IS NULL),
  EXISTS(SELECT 1 FROM billing_webhook_events WHERE workspace_id=$1 AND event_type='subscription.active'),
  COALESCE((SELECT move_steps FROM workspace_onboarding WHERE workspace_id=$1),'{}'::jsonb),
  EXISTS(SELECT 1 FROM help_centers WHERE workspace_id=$1 AND google_site_verification IS NOT NULL)`, workspaceID).
		Scan(&r.Enabled, &r.Published, &r.Imported, &r.Verified, &r.Live, &r.Paid, &r.Saved, &r.VerificationPresent)
	if err != nil {
		return r, err
	}
	if !s.limits {
		r.Paid = true
		return r, nil
	}
	access, err := workspaceAccess(ctx, tx, workspaceID, now)
	r.Paid = r.Paid || (!access.Locked && access.Plan != domain.PlanTrial)
	return r, err
}

func (s *Store) MoveReadiness(ctx context.Context, workspaceID string, now time.Time) (MoveReadiness, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (MoveReadiness, error) { return s.moveReadiness(ctx, tx, workspaceID, now) })
}

// rememberMoveStep never overwrites a completed milestone, including when optional steps are skipped.
// Domain checks call it in their existing transaction, so a later removal cannot erase the milestone.
func rememberMoveStep(ctx context.Context, tx pgx.Tx, workspaceID, key, state string) error {
	_, err := tx.Exec(ctx, `UPDATE workspace_onboarding SET move_steps=jsonb_set(move_steps,ARRAY[$2::text],to_jsonb($3::text))
  WHERE workspace_id=$1 AND goal='leave_helpdesk' AND move_steps->>$2 IS DISTINCT FROM 'done'`, workspaceID, key, state)
	return err
}

// SetSetupMoveStep stores only what cannot be inferred: preview checked, the external Search Console
// work confirmed, or choosing no custom domain. Automatic milestones cannot be manually completed.
func (s *Store) SetSetupMoveStep(ctx context.Context, workspaceID, key, state string, now time.Time) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var goal *string
		if err := tx.QueryRow(ctx, `SELECT goal FROM workspace_onboarding WHERE workspace_id=$1 FOR UPDATE`, workspaceID).Scan(&goal); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSetupMove
			}
			return err
		}
		if goal == nil || *goal != "leave_helpdesk" {
			return ErrSetupMove
		}
		if key == "custom_domain" && (state == "skipped" || state == "none") {
			for _, step := range moveDomainSteps {
				if state == "none" {
					if _, err := tx.Exec(ctx, `UPDATE workspace_onboarding SET move_steps=move_steps-$2 WHERE workspace_id=$1 AND move_steps->>$2='skipped'`, workspaceID, step); err != nil {
						return err
					}
				} else if err := rememberMoveStep(ctx, tx, workspaceID, step, state); err != nil {
					return err
				}
			}
			return nil
		}
		if state != "done" || (key != "preview" && key != "search_console") {
			return ErrSetupMove
		}
		r, err := s.moveReadiness(ctx, tx, workspaceID, now)
		if err != nil {
			return err
		}
		if r.Saved[key] == "done" {
			return nil
		}
		if key == "preview" && (!r.Enabled || !r.Published) {
			return fmt.Errorf("%w: turn on the help center and import or publish an article before confirming the preview", ErrSetupMove)
		}
		if key == "search_console" && !r.Live && r.Saved["cname"] != "done" {
			return fmt.Errorf("%w: change the CNAME and press Check now successfully before confirming Search Console", ErrSetupMove)
		}
		return rememberMoveStep(ctx, tx, workspaceID, key, state)
	})
}

func (s *Store) setupMove(ctx context.Context, workspaceID string, now time.Time) (SetupProgress, error) {
	r, err := s.MoveReadiness(ctx, workspaceID, now)
	if err != nil {
		return SetupProgress{}, err
	}
	states := map[string]string{"import": r.Imported}
	for key, done := range map[string]bool{"custom_domain": r.Verified, "plan": r.Paid, "cname": r.Live} {
		if done {
			states[key] = "done"
		}
	}
	progress := SetupProgress{Complete: true}
	for _, key := range []string{"import", "preview", "custom_domain", "plan", "cname", "search_console"} {
		state := states[key]
		if state == "" {
			state = "none"
		}
		if saved := r.Saved[key]; saved == "done" || (saved == "skipped" && state != "done") {
			state = saved
		}
		step := SetupStep{Key: key, State: state}
		if key == "preview" && state != "done" {
			var detail string
			switch {
			case !r.Published:
				detail = "import"
			case !r.Enabled:
				detail = "enable"
			}
			if detail != "" {
				step.Detail = &detail
			}
		}
		if key == "search_console" && state == "none" && r.VerificationPresent {
			detail := "verification_present"
			step.Detail = &detail
		}
		progress.Steps = append(progress.Steps, step)
		if state != "done" && state != "skipped" {
			progress.Complete = false
			if progress.Recommended == nil {
				next := key
				progress.Recommended = &next
			}
		}
	}
	return progress, nil
}
