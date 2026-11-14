package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

const (
	// TrialOfferMaxValidity is the longest an offer link may stay valid, and its default.
	TrialOfferMaxValidity = 30 * 24 * time.Hour
	// TrialOfferLength is how long the trial runs from the moment an admin accepts an offer.
	TrialOfferLength = 14 * 24 * time.Hour
)

// The states of a trial offer.
const (
	TrialOfferOpen     = "open"
	TrialOfferAccepted = "accepted"
	TrialOfferExpired  = "expired"
	TrialOfferRevoked  = "revoked"
	// TrialOfferHasPlan refuses an open offer of a workspace that has a plan in effect.
	TrialOfferHasPlan = "has_plan"
)

var (
	// ErrTrialOfferNotFound: no offer has this token (or id).
	ErrTrialOfferNotFound = errors.New("no trial offer has this link")
	// ErrTrialOfferOtherWorkspace: the offer exists, for a workspace other than the caller's.
	ErrTrialOfferOtherWorkspace = errors.New("this trial offer is for another workspace")
)

// TrialOfferRefusedError refuses to accept or revoke an offer: Reason is its state (accepted, expired,
// revoked) or has_plan; TrialEndsAt is the workspace's trial end, which did not change.
type TrialOfferRefusedError struct {
	Reason      string
	TrialEndsAt time.Time
}

func (e *TrialOfferRefusedError) Error() string {
	return "the trial offer is " + e.Reason
}

// TrialOffer is one offer link an operator made, without its token.
type TrialOffer struct {
	ID         string
	State      string
	CreatedBy  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt *time.Time
	AcceptedBy *string
	RevokedAt  *time.Time
	RevokedBy  *string
}

// TrialOfferView is an offer with what its workspace's trial looks like now.
type TrialOfferView struct {
	TrialOffer
	WorkspaceName string
	TrialEndsAt   time.Time
	Plan          string
	// ExtendsTo is the trial end accepting at now would give: never earlier than TrialEndsAt.
	ExtendsTo time.Time
}

// TrialOfferAcceptance is the answer of an accepted offer.
type TrialOfferAcceptance struct {
	View        TrialOfferView
	PreviousEnd time.Time
	Extended    bool
}

const trialOfferColumns = `id::text, created_by, created_at, expires_at, accepted_at, accepted_by, revoked_at, revoked_by`

func scanTrialOffer(row pgx.Row, now time.Time) (TrialOffer, error) {
	var offer TrialOffer
	if err := row.Scan(&offer.ID, &offer.CreatedBy, &offer.CreatedAt, &offer.ExpiresAt, &offer.AcceptedAt, &offer.AcceptedBy, &offer.RevokedAt, &offer.RevokedBy); err != nil {
		return TrialOffer{}, err
	}
	offer.CreatedAt, offer.ExpiresAt = offer.CreatedAt.UTC(), offer.ExpiresAt.UTC()
	offer.AcceptedAt, offer.RevokedAt = domain.UTCOptional(offer.AcceptedAt), domain.UTCOptional(offer.RevokedAt)
	switch {
	case offer.AcceptedAt != nil:
		offer.State = TrialOfferAccepted
	case offer.RevokedAt != nil:
		offer.State = TrialOfferRevoked
	case !now.Before(offer.ExpiresAt):
		offer.State = TrialOfferExpired
	default:
		offer.State = TrialOfferOpen
	}
	return offer, nil
}

func trialOfferHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateTrialOffer makes an offer link for the workspace, valid for valid (at most
// TrialOfferMaxValidity), and returns it with its token. Only the token's hash is stored.
func (s *Store) CreateTrialOffer(ctx context.Context, workspaceID, by string, now time.Time, valid time.Duration) (TrialOffer, string, error) {
	if by == "" {
		return TrialOffer{}, "", errors.New("a trial offer needs the name of who makes it")
	}
	if valid <= 0 || valid > TrialOfferMaxValidity {
		return TrialOffer{}, "", fmt.Errorf("a trial offer is valid for more than 0 and at most 30 days (720h), not %s", valid)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return TrialOffer{}, "", fmt.Errorf("generate trial offer token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	offer, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (TrialOffer, error) {
		return scanTrialOffer(tx.QueryRow(ctx, `INSERT INTO trial_offers(workspace_id,token_hash,created_by,created_at,expires_at)
			VALUES($1,$2,$3,$4,$5) RETURNING `+trialOfferColumns, workspaceID, trialOfferHash(token), by, now, now.Add(valid)), now)
	})
	if err != nil {
		return TrialOffer{}, "", fmt.Errorf("create trial offer: %w", err)
	}
	return offer, token, nil
}

// TrialOffers lists the workspace's offers, oldest first, with their state at now.
func (s *Store) TrialOffers(ctx context.Context, workspaceID string, now time.Time) ([]TrialOffer, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]TrialOffer, error) {
		rows, err := tx.Query(ctx, `SELECT `+trialOfferColumns+` FROM trial_offers WHERE workspace_id=$1 ORDER BY created_at, id`, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list trial offers: %w", err)
		}
		defer rows.Close()
		offers := []TrialOffer{}
		for rows.Next() {
			offer, err := scanTrialOffer(rows, now)
			if err != nil {
				return nil, err
			}
			offers = append(offers, offer)
		}
		return offers, rows.Err()
	})
}

var offerIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// RevokeTrialOffer revokes an open offer by its id, wherever its workspace: the operator names only the
// id. An offer no longer open is refused with *TrialOfferRefusedError naming its state.
func (s *Store) RevokeTrialOffer(ctx context.Context, offerID, by string, now time.Time) (TrialOffer, error) {
	if by == "" {
		return TrialOffer{}, errors.New("revoking a trial offer needs the name of who does it")
	}
	if !offerIDPattern.MatchString(offerID) {
		return TrialOffer{}, ErrTrialOfferNotFound
	}
	workspaceID, err := s.trialOfferWorkspace(ctx, `id=$1`, offerID)
	if err != nil {
		return TrialOffer{}, err
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (TrialOffer, error) {
		offer, err := scanTrialOffer(tx.QueryRow(ctx, `SELECT `+trialOfferColumns+` FROM trial_offers WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, offerID), now)
		if err != nil {
			return TrialOffer{}, fmt.Errorf("revoke trial offer: %w", err)
		}
		if offer.State != TrialOfferOpen {
			return TrialOffer{}, &TrialOfferRefusedError{Reason: offer.State}
		}
		return scanTrialOffer(tx.QueryRow(ctx, `UPDATE trial_offers SET revoked_at=$3, revoked_by=$4 WHERE workspace_id=$1 AND id=$2 RETURNING `+trialOfferColumns,
			workspaceID, offerID, now, by), now)
	})
}

// trialOfferWorkspace finds the workspace of the offer the condition matches ($1), across workspaces.
// Only the workspace id leaves the transaction.
func (s *Store) trialOfferWorkspace(ctx context.Context, condition string, value any) (string, error) {
	var workspaceID string
	err := s.db.Privileged(ctx, database.PrivilegeTrialOffer, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT workspace_id::text FROM trial_offers WHERE `+condition, value).Scan(&workspaceID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTrialOfferNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find trial offer: %w", err)
	}
	return workspaceID, nil
}
