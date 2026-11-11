package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// missingTrialOffer says why the caller's workspace has no offer with this token: another workspace has
// it (ErrTrialOfferOtherWorkspace, without naming that workspace), or none does.
func (s *Store) missingTrialOffer(ctx context.Context, token string) error {
	if _, err := s.trialOfferWorkspace(ctx, `token_hash=$1`, trialOfferHash(token)); err != nil {
		return err
	}
	return ErrTrialOfferOtherWorkspace
}

// TrialOfferByToken reads the offer of the link in the caller's workspace. Reading changes nothing.
func (s *Store) TrialOfferByToken(ctx context.Context, workspaceID, token string, now time.Time) (TrialOfferView, error) {
	view, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (TrialOfferView, error) {
		return trialOfferView(ctx, tx, workspaceID, token, now, false)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TrialOfferView{}, s.missingTrialOffer(ctx, token)
	}
	return view, err
}

func trialOfferView(ctx context.Context, tx pgx.Tx, workspaceID, token string, now time.Time, forUpdate bool) (TrialOfferView, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	offer, err := scanTrialOffer(tx.QueryRow(ctx, `SELECT `+trialOfferColumns+` FROM trial_offers WHERE workspace_id=$1 AND token_hash=$2`+lock,
		workspaceID, trialOfferHash(token)), now)
	if err != nil {
		return TrialOfferView{}, err
	}
	view := TrialOfferView{TrialOffer: offer}
	if err := tx.QueryRow(ctx, `SELECT name, trial_ends_at FROM workspaces WHERE id=$1`+lock, workspaceID).Scan(&view.WorkspaceName, &view.TrialEndsAt); err != nil {
		return TrialOfferView{}, fmt.Errorf("read the workspace of the trial offer: %w", err)
	}
	subscriptions, err := LoadSubscriptions(ctx, tx, workspaceID)
	if err != nil {
		return TrialOfferView{}, fmt.Errorf("load subscriptions: %w", err)
	}
	view.TrialEndsAt = view.TrialEndsAt.UTC()
	view.Plan = domain.NewAccess(subscriptions, view.TrialEndsAt, now).Plan
	view.ExtendsTo = now.Add(TrialOfferLength).UTC()
	if view.TrialEndsAt.After(view.ExtendsTo) {
		view.ExtendsTo = view.TrialEndsAt
	}
	return view, nil
}

// HasPlan reports whether the workspace of the view has a plan in effect: neither the trial nor none.
func (v TrialOfferView) HasPlan() bool {
	return v.Plan != domain.PlanTrial && v.Plan != domain.PlanNone
}

// AcceptTrialOffer spends an open offer of the caller's workspace: the trial then ends TrialOfferLength
// after now, unless it already ends later, which is kept. The change is recorded in
// workspace_trial_changes with the offer and who accepted (by). A workspace with a plan, or an offer no
// longer open, is refused with *TrialOfferRefusedError and nothing changes.
func (s *Store) AcceptTrialOffer(ctx context.Context, workspaceID, token, agentID, by string, now time.Time) (TrialOfferAcceptance, error) {
	acceptance, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (TrialOfferAcceptance, error) {
		view, err := trialOfferView(ctx, tx, workspaceID, token, now, true)
		if err != nil {
			return TrialOfferAcceptance{}, err
		}
		switch {
		case view.State != TrialOfferOpen:
			return TrialOfferAcceptance{}, &TrialOfferRefusedError{Reason: view.State, TrialEndsAt: view.TrialEndsAt}
		case view.HasPlan():
			return TrialOfferAcceptance{}, &TrialOfferRefusedError{Reason: TrialOfferHasPlan, TrialEndsAt: view.TrialEndsAt}
		}
		acceptance := TrialOfferAcceptance{PreviousEnd: view.TrialEndsAt}
		if view.ExtendsTo.After(view.TrialEndsAt) {
			if _, err := setTrialEndTx(ctx, tx, workspaceID, view.ExtendsTo, "trial offer "+view.ID+" accepted by "+by, now); err != nil {
				return TrialOfferAcceptance{}, err
			}
			acceptance.Extended = true
		}
		if _, err := tx.Exec(ctx, `UPDATE trial_offers SET accepted_at=$3, accepted_by_agent_id=$4, accepted_by=$5 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, view.ID, now, agentID, by); err != nil {
			return TrialOfferAcceptance{}, fmt.Errorf("spend trial offer: %w", err)
		}
		acceptance.View, err = trialOfferView(ctx, tx, workspaceID, token, now, false)
		return acceptance, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TrialOfferAcceptance{}, s.missingTrialOffer(ctx, token)
	}
	return acceptance, err
}
