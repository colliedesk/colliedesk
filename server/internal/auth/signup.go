package auth

import (
	"context"
	"crypto/hmac"
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
	ErrInvalidOnboarding = errors.New("an onboarding answer is not one of its listed values")
	ErrInvalidEmail      = domain.ErrInvalidEmail
	// ErrSignupKeyMissing means the service has no SIGNUP_HASH_KEY, so it cannot record a trial.
	ErrSignupKeyMissing = errors.New("SIGNUP_HASH_KEY is not set")
)

// SignUp is a request to start a cloud trial.
type SignUp struct {
	// Email is the address the identity provider verified.
	Email string
	Name  string
	// ClientIP is the rate-limit key of the person's network, from the trusted proxy chain.
	ClientIP string
	// HostedMailDomain, when set, connects email at support@<slug>.<HostedMailDomain> in the same
	// transaction: settings show that address as ready, so mail sent there must not be refused.
	HostedMailDomain string
	// Onboarding is what the person said about their team; it is stored with the workspace.
	Onboarding domain.Onboarding
}

// SignUpCloud creates a trial workspace with Email as its admin. In one privileged transaction it
// checks that the mailbox never had a trial and that the address is under its daily limit, creates
// the workspace and records the signup, so a failed create burns neither. Only hashes are kept: the
// sha256 of the normalized email (enough to recognise a repeat, not to read the address back) and of
// the client address, which is cleared after 30 days. Both are HMAC-SHA256 under SIGNUP_HASH_KEY:
// without the key an address cannot be tested against the table, and the key cannot be rotated
// without losing the trial history.
func (s *Service) SignUpCloud(ctx context.Context, in SignUp) (domain.Workspace, error) {
	if !in.Onboarding.Valid() {
		return domain.Workspace{}, ErrInvalidOnboarding
	}
	name := strings.TrimSpace(in.Name)
	if length := utf8.RuneCountInString(name); length < minNameLength || length > maxNameLength {
		return domain.Workspace{}, ErrInvalidName
	}
	if _, denied := domain.DeniedBrand(name); denied {
		return domain.Workspace{}, ErrNameNotAllowed
	}
	if len(s.signupKey) == 0 {
		return domain.Workspace{}, ErrSignupKeyMissing
	}
	key, err := domain.TrialEmailKey(in.Email)
	if err != nil {
		return domain.Workspace{}, ErrInvalidEmail
	}
	emailHash, ipHash := s.keyedHash(key), s.keyedHash(in.ClientIP)
	var workspace domain.Workspace
	err = s.db.Privileged(ctx, database.PrivilegeCreateWorkspace, func(tx pgx.Tx) error {
		now := s.now()
		// Cheap refusals first, without the platform-wide lock, so a loop of doomed requests from one
		// account queues behind nothing. The same checks run again under the lock, which decides.
		if err := checkSignupAllowed(ctx, tx, emailHash, ipHash, now); err != nil {
			return err
		}
		// One signup at a time: the checks and the insert must see each other.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('cloud-signup',0))`); err != nil {
			return fmt.Errorf("lock sign-ups: %w", err)
		}
		if err := checkSignupAllowed(ctx, tx, emailHash, ipHash, now); err != nil {
			return err
		}
		if workspace, err = s.createUniqueWorkspaceTx(ctx, tx, name, in.Email); err != nil {
			return err
		}
		if err := connectHostedEmailTx(ctx, tx, workspace, in.HostedMailDomain); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO cloud_signups(email_hash,signup_workspace_id,ip_hash,created_at) VALUES($1,$2,$3,$4)`,
			emailHash, workspace.ID, ipHash, now); err != nil {
			return fmt.Errorf("record sign-up: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO workspace_onboarding(workspace_id,goal,role,source,created_at) VALUES($1,$2,$3,$4,$5)`,
			workspace.ID, in.Onboarding.Goal, in.Onboarding.Role, in.Onboarding.Source, now); err != nil {
			return fmt.Errorf("record onboarding answers: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Workspace{}, err
	}
	return workspace, nil
}

// connectHostedEmailTx gives a new workspace its email channel at the hosted address; nothing when the
// hosted address is off or the slug cannot be a mail subdomain.
func connectHostedEmailTx(ctx context.Context, tx pgx.Tx, workspace domain.Workspace, hostedMailDomain string) error {
	address := email.HostedAddress(workspace.Slug, hostedMailDomain)
	if address == "" {
		return nil
	}
	token, err := email.NewToken()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_channels(workspace_id,support_address,domain_verification_token,sender_name) VALUES($1,$2,$3,$4)`,
		workspace.ID, address, token, domain.DefaultSenderName(workspace.Name, workspace.Slug)); err != nil {
		return fmt.Errorf("connect hosted email: %w", err)
	}
	return nil
}

// checkSignupAllowed refuses a mailbox that already had a trial and a client address that already
// started SignupsPerIP trials in the last SignupIPWindow.
func checkSignupAllowed(ctx context.Context, tx pgx.Tx, emailHash, ipHash string, now time.Time) error {
	var used bool
	var recent int
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cloud_signups WHERE email_hash=$1),
		(SELECT count(*) FROM cloud_signups WHERE ip_hash=$2 AND created_at > $3)`,
		emailHash, ipHash, now.Add(-SignupIPWindow)).Scan(&used, &recent); err != nil {
		return fmt.Errorf("check sign-up limits: %w", err)
	}
	switch {
	case used:
		return ErrTrialAlreadyUsed
	case recent >= SignupsPerIP:
		return ErrSignupRateLimited
	}
	return nil
}

// ClearSignupAddresses drops the client address hash of sign-ups older than 30 days and returns how
// many rows it cleared. The email hash stays: it is what stops a second trial. Run it at start and
// periodically; it does not depend on anyone signing up.
func (s *Service) ClearSignupAddresses(ctx context.Context) (int64, error) {
	var cleared int64
	err := s.db.Privileged(ctx, database.PrivilegeSignupSweep, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE cloud_signups SET ip_hash=NULL WHERE ip_hash IS NOT NULL AND created_at < $1`, s.now().Add(-signupIPRetention))
		if err != nil {
			return fmt.Errorf("clear old sign-up addresses: %w", err)
		}
		cleared = tag.RowsAffected()
		return nil
	})
	return cleared, err
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
	// Three tries: the plain slug, then two with a random suffix. A candidate that cannot be a mail
	// subdomain (reserved name) is replaced by a suffixed one, never inserted.
	var candidates []string
	if email.ValidateHostedSlug(base) == nil {
		candidates = append(candidates, base)
	}
	for len(candidates) < 3 {
		suffix, err := s.slugSuffix()
		if err != nil {
			return domain.Workspace{}, err
		}
		candidate := base + "-" + suffix
		if email.ValidateHostedSlug(candidate) != nil {
			return domain.Workspace{}, ErrSlugUnavailable
		}
		candidates = append(candidates, candidate)
	}
	for _, slug := range candidates {
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

// keyedHash is HMAC-SHA256 of value under the sign-up key, hex-encoded.
func (s *Service) keyedHash(value string) string {
	mac := hmac.New(sha256.New, s.signupKey)
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
