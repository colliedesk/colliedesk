package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/jackc/pgx/v5"
)

// Signup abuse controls: one trial per mailbox, and a cap per client address per rolling day.
const (
	SignupsPerIP      = 3
	SignupIPWindow    = 24 * time.Hour
	signupIPRetention = 30 * 24 * time.Hour
	minNameLength     = 2
	maxNameLength     = 60
	maxSlugBase       = 50
)

var (
	ErrInvalidName       = errors.New("workspace name must be 2 to 60 characters")
	ErrNameNotAllowed    = errors.New("workspace name is not allowed")
	ErrTrialAlreadyUsed  = errors.New("this email already started a trial")
	ErrSignupRateLimited = errors.New("too many sign-ups from this address")
	ErrSlugUnavailable   = errors.New("no free workspace address for this name")
)

// SignUp is a request to start a cloud trial.
type SignUp struct {
	// Email is the address the identity provider verified.
	Email string
	Name  string
	// ClientIP is the rate-limit key of the person's network, from the trusted proxy chain.
	ClientIP string
}

// SignUpCloud creates a trial workspace with Email as its admin. In one privileged transaction it
// checks that the mailbox never had a trial and that the address is under its daily limit, creates
// the workspace and records the signup, so a failed create burns neither. Only hashes are kept: the
// sha256 of the normalized email (enough to recognise a repeat, not to read the address back) and of
// the client address, which is cleared after 30 days.
func (s *Service) SignUpCloud(ctx context.Context, in SignUp) (domain.Workspace, error) {
	name := strings.TrimSpace(in.Name)
	if length := utf8.RuneCountInString(name); length < minNameLength || length > maxNameLength {
		return domain.Workspace{}, ErrInvalidName
	}
	if _, denied := domain.DeniedBrand(name); denied {
		return domain.Workspace{}, ErrNameNotAllowed
	}
	key, err := domain.TrialEmailKey(in.Email)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("sign-up email: %w", err)
	}
	emailHash, ipHash := digest(key), digest(in.ClientIP)
	var workspace domain.Workspace
	err = s.db.Privileged(ctx, database.PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		now := s.now()
		// One signup at a time: the checks below and the insert must see each other.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('cloud-signup',0))`); err != nil {
			return fmt.Errorf("lock sign-ups: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE cloud_signups SET ip_hash=NULL WHERE ip_hash IS NOT NULL AND created_at < $1`, now.Add(-signupIPRetention)); err != nil {
			return fmt.Errorf("clear old sign-up addresses: %w", err)
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cloud_signups WHERE email_hash=$1)`, emailHash).Scan(&used); err != nil {
			return fmt.Errorf("check earlier trial: %w", err)
		}
		if used {
			return ErrTrialAlreadyUsed
		}
		var recent int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM cloud_signups WHERE ip_hash=$1 AND created_at > $2`, ipHash, now.Add(-SignupIPWindow)).Scan(&recent); err != nil {
			return fmt.Errorf("count sign-ups of this address: %w", err)
		}
		if recent >= SignupsPerIP {
			return ErrSignupRateLimited
		}
		if workspace, err = s.createUniqueWorkspaceTx(ctx, tx, name, in.Email); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cloud_signups(email_hash,signup_workspace_id,ip_hash,created_at) VALUES($1,$2,$3,$4)`,
			emailHash, workspace.ID, ipHash, now); err != nil {
			return fmt.Errorf("record sign-up: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Workspace{}, err
	}
	return workspace, nil
}

// createUniqueWorkspaceTx tries the plain slug of the name, then twice with a random suffix, and
// gives up with ErrSlugUnavailable. A slug that cannot be a mail subdomain (reserved, too long
// after cleaning) goes straight to the suffixed form.
func (s *Service) createUniqueWorkspaceTx(ctx context.Context, tx pgx.Tx, name, adminEmail string) (domain.Workspace, error) {
	base := slugify(name)
	if len(base) > maxSlugBase {
		base = strings.Trim(base[:maxSlugBase], "-")
	}
	if base == "" {
		base = "workspace"
	}
	candidates := []string{base}
	if email.ValidateHostedSlug(base) != nil {
		candidates = nil
	}
	for len(candidates) < 3 {
		suffix, err := s.slugSuffix()
		if err != nil {
			return domain.Workspace{}, err
		}
		candidates = append(candidates, base+"-"+suffix)
	}
	for _, slug := range candidates[:3] {
		workspace, _, created, err := s.insertWorkspaceTx(ctx, tx, slug, name, adminEmail)
		if err != nil {
			return domain.Workspace{}, err
		}
		if created {
			return workspace, nil
		}
	}
	return domain.Workspace{}, ErrSlugUnavailable
}

func (s *Service) slugSuffix() (string, error) {
	reader := s.random
	if reader == nil {
		reader = rand.Reader
	}
	var raw [2]byte
	if _, err := io.ReadFull(reader, raw[:]); err != nil {
		return "", fmt.Errorf("random slug suffix: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
