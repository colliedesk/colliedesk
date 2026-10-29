package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// TrashWindow is how long the files of a deleted workspace stay in the trash before they are purged.
const TrashWindow = 30 * 24 * time.Hour

// MaxClockSkew is the largest difference between the application and database clocks a pass accepts.
const MaxClockSkew = 5 * time.Minute

var (
	// ErrNoMailer means a notice is due but the server has no way to send mail, so nothing is deleted.
	ErrNoMailer = errors.New("a deletion notice is due but outbound email is not configured (set SMTP_HOST and MAIL_FROM_DOMAIN)")
	// ErrClockSkew means the application and database clocks disagree, so no decision based on time is safe.
	ErrClockSkew = errors.New("the application and database clocks differ by more than 5 minutes; fix the clock before retention runs")
)

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// Storage is the attachment store as retention uses it: a workspace's files are moved to a trash
// and purged after TrashWindow (see attachments.LocalDisk).
type Storage interface {
	// Trash moves the workspace's files aside; found is false when it had no directory.
	Trash(ctx context.Context, workspaceID string, now time.Time) (undo func() error, found bool, err error)
	ListTrash(ctx context.Context) ([]attachments.TrashEntry, error)
	RestoreTrash(ctx context.Context, entry attachments.TrashEntry) error
	RemoveTrash(ctx context.Context, entry attachments.TrashEntry) error
}

// PaidChecker asks the billing provider whether a workspace has a subscription that grants a plan
// now (polar.Client).
type PaidChecker interface {
	HasPaidSubscription(ctx context.Context, externalID string, now time.Time) (bool, error)
}

// Config wires a Service. Mailer may be nil: notices then fail with ErrNoMailer and no workspace
// is ever deleted without one.
type Config struct {
	DB      *database.DB
	Storage Storage
	Mailer  Mailer
	// From is the platform sender address of notices, for example noreply@MAIL_FROM_DOMAIN.
	From string
	// Origin is the web address of the app, quoted in notices.
	Origin string
	// HelpCenterDomain gives each workspace its own host; a notice quotes the workspace's.
	HelpCenterDomain string
	// DeleteEnabled turns deletion on. Off, the pass still sends notices and logs what it would delete.
	DeleteEnabled bool
	// MaxDeletionsPerDay caps deletions in any rolling 24 hours; 0 deletes nothing.
	MaxDeletionsPerDay int
	// Polar is asked before each deletion; without it nothing is deleted.
	Polar PaidChecker
	// Now overrides the decision clock (tests). Left nil, a pass uses the database clock and
	// refuses to run when it differs from SystemNow by more than MaxClockSkew.
	Now       func() time.Time
	SystemNow func() time.Time
	Logger    *slog.Logger
}

// Service runs the retention policy of locked workspaces.
type Service struct{ config Config }

// New returns a Service; SystemNow defaults to the system clock and Logger to slog.Default.
func New(config Config) *Service {
	if config.SystemNow == nil {
		config.SystemNow = func() time.Time { return time.Now().UTC() }
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &Service{config: config}
}

// Item is one workspace the policy would act on.
type Item struct {
	WorkspaceID  string
	Slug         string
	LockedSince  time.Time
	NoticeAt     time.Time
	DeleteAt     time.Time
	NoticeSentAt *time.Time
	Action       Action
}

// Result counts what Run did. Held counts deletions that were due but not done (switch off, daily
// cap reached, or the billing provider could not confirm).
type Result struct{ Noticed, Deleted, Reset, Held int }

// state is one workspace as the policy sees it, read inside a privileged transaction.
type state struct {
	Item
	Name string
}

// clock returns the instant a pass decides with: the injected clock, or the database clock after
// checking it against the application clock.
func (s *Service) clock(ctx context.Context) (time.Time, error) {
	if s.config.Now != nil {
		return s.config.Now(), nil
	}
	var dbNow time.Time
	err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT now()`).Scan(&dbNow)
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("read database clock: %w", err)
	}
	skew := s.config.SystemNow().Sub(dbNow)
	if skew < 0 {
		skew = -skew
	}
	if skew > MaxClockSkew {
		return time.Time{}, ErrClockSkew
	}
	return dbNow.UTC(), nil
}

// Candidates lists the workspaces the policy has to look at: trial ended long enough ago for a
// notice to be due and nothing paying or complimentary in force, or a notice already on record that
// may need a reset. Most paying workspaces never reach the per-workspace evaluation.
func (s *Service) Candidates(ctx context.Context) ([]string, error) {
	now, err := s.clock(ctx)
	if err != nil {
		return nil, err
	}
	return s.candidates(ctx, now)
}

func (s *Service) candidates(ctx context.Context, now time.Time) ([]string, error) {
	cutoff := now.AddDate(-domain.RetentionYears, 0, 0).Add(domain.RetentionNoticeLead)
	var ids []string
	err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT w.id::text FROM workspaces w
			WHERE w.retention_notice_sent_at IS NOT NULL
			   OR EXISTS (SELECT 1 FROM workspace_retention_notices n WHERE n.workspace_id = w.id)
			   OR (w.trial_ends_at <= $1
			       AND NOT EXISTS (SELECT 1 FROM workspace_billing b WHERE b.workspace_id = w.id
			           AND (b.ended_at IS NULL OR b.ended_at > $2)
			           AND (b.status IN ('active','trialing','past_due') OR (b.status = 'canceled' AND b.current_period_end > $2)))
			       AND NOT EXISTS (SELECT 1 FROM workspace_comp_plans c WHERE c.workspace_id = w.id
			           AND c.revoked_at IS NULL AND (c.until IS NULL OR c.until > $2)))
			ORDER BY w.created_at, w.id`, cutoff, now)
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list retention candidates: %w", err)
	}
	return ids, nil
}

// evaluate reads one workspace and decides. lock is "", "FOR NO KEY UPDATE" (enough to serialise
// runners without blocking a payment webhook) or "FOR UPDATE" (needed before a delete).
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, id, lock string, now time.Time) (state, error) {
	var st state
	var trialEndsAt time.Time
	var lastChange *time.Time
	st.WorkspaceID = id
	err := tx.QueryRow(ctx, `SELECT slug,name,trial_ends_at,retention_notice_sent_at,
		(SELECT max(changed_at) FROM workspace_trial_changes WHERE workspace_id=workspaces.id)
		FROM workspaces WHERE id=$1 `+lock, id).Scan(&st.Slug, &st.Name, &trialEndsAt, &st.NoticeSentAt, &lastChange)
	if err != nil {
		return state{}, err
	}
	subscriptions, err := loadHistory(ctx, tx, id)
	if err != nil {
		return state{}, err
	}
	since, locked := domain.LockedSince(subscriptions, trialEndsAt, now)
	if locked {
		var change time.Time
		if lastChange != nil {
			change = *lastChange
		}
		since = domain.RetentionLockStart(since, change)
		st.LockedSince = since
		st.NoticeAt, st.DeleteAt = domain.RetentionDates(since)
	}
	st.Action = Decide(locked, since, st.NoticeSentAt, now)
	return st, nil
}

// Plan lists what Run would do now, without sending, recording or deleting anything.
func (s *Service) Plan(ctx context.Context) ([]Item, error) {
	now, err := s.clock(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := s.candidates(ctx, now)
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, id := range ids {
		var st state
		err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) (err error) {
			st, err = s.evaluate(ctx, tx, id, "", now)
			return err
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // deleted since the list was read
		}
		if err != nil {
			return nil, fmt.Errorf("plan retention: %w", err)
		}
		if st.Action != ActionNone {
			items = append(items, st.Item)
		}
	}
	return items, nil
}

// Run sends due notices, clears stale ones and deletes the workspaces that are due, then purges
// trash older than TrashWindow. A failure on one workspace is logged and returned at the end; the
// others still run. It is safe to run again and from two processes at once.
func (s *Service) Run(ctx context.Context) (Result, error) {
	now, err := s.clock(ctx)
	if err != nil {
		return Result{}, err
	}
	ids, err := s.candidates(ctx, now)
	if err != nil {
		return Result{}, err
	}
	var result Result
	var failures []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		outcome, err := s.process(ctx, id, now)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // another instance deleted it after the list was read
		}
		if err != nil {
			s.config.Logger.Error("retention", "workspace_id", id, "slug", outcome.slug, "action", outcome.action, "error", err)
			failures = append(failures, fmt.Errorf("workspace %s (%s): %s: %w", id, outcome.slug, outcome.action, err))
		}
		switch outcome.done {
		case ActionNotice:
			result.Noticed++
		case ActionDelete:
			result.Deleted++
		case ActionReset:
			result.Reset++
		}
		if outcome.held {
			result.Held++
		}
	}
	if s.config.Storage != nil {
		if err := s.maintainTrash(ctx, now); err != nil {
			failures = append(failures, err)
		}
	}
	return result, errors.Join(failures...)
}

// outcome is what one workspace's transaction did, known only after it committed.
type outcome struct {
	slug   string
	action Action
	done   Action
	held   bool
}

// process runs one workspace in its own transaction. Files moved to the trash by a delete that does
// not commit are moved back.
func (s *Service) process(ctx context.Context, id string, now time.Time) (out outcome, err error) {
	var undo func() error
	var soft error
	err = s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		st, err := s.evaluate(ctx, tx, id, "FOR NO KEY UPDATE", now)
		if err != nil {
			return err
		}
		if st.Action == ActionDelete {
			if st, err = s.evaluate(ctx, tx, id, "FOR UPDATE", now); err != nil {
				return err
			}
		}
		out.slug, out.action = st.Slug, st.Action
		switch st.Action {
		case ActionReset:
			if _, err = tx.Exec(ctx, `UPDATE workspaces SET retention_notice_sent_at=NULL WHERE id=$1`, id); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `DELETE FROM workspace_retention_notices WHERE workspace_id=$1`, id); err != nil {
				return err
			}
			out.done = ActionReset
		case ActionNotice:
			out.done, soft, err = s.notify(ctx, tx, st, now)
		case ActionDelete:
			out.done, out.held, undo, err = s.remove(ctx, tx, st, now)
		}
		return err
	})
	if err != nil && undo != nil {
		if restoreErr := s.restoreUnlessDeleted(ctx, id, undo); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		}
	}
	if err != nil {
		out.done, out.held = ActionNone, false
	}
	return out, errors.Join(err, soft)
}

// restoreUnlessDeleted moves a failed delete's files back, but only while the workspace row exists.
// When the commit reached the database and the client only saw an error, the workspace is gone and
// its files stay in the trash to be purged; moving them back would leave files nobody cleans.
// When the database cannot be asked, nothing moves; maintainTrash restores them later if the row
// is there.
func (s *Service) restoreUnlessDeleted(ctx context.Context, workspaceID string, undo func() error) error {
	var exists bool
	detached := context.WithoutCancel(ctx) // a stop signal must not leave the files in the trash
	err := s.config.DB.Privileged(detached, database.PrivilegeRetention, func(tx pgx.Tx) error {
		return tx.QueryRow(detached, `SELECT EXISTS(SELECT 1 FROM workspaces WHERE id=$1)`, workspaceID).Scan(&exists)
	})
	switch {
	case err != nil:
		return fmt.Errorf("could not tell whether workspace %s was deleted; its files stay in the trash: %w", workspaceID, err)
	case !exists:
		s.config.Logger.Warn("delete reported an error but the workspace is gone; files stay in the trash for purge", "workspace_id", workspaceID)
		return nil
	}
	return undo()
}

// reconcileAfter is how old a trash entry must be before it is treated as left by a crashed delete.
// The real guard against undoing a delete that is still running is the workspace row lock: a pass
// waits for it before it reaches the trash. The hour only covers a delete that has renamed the
// files but not yet taken its lock.
const reconcileAfter = time.Hour

// maintainTrash puts back the files of a workspace that still exists (a delete that crashed after
// the rename and before the commit) and purges the entries of deleted workspaces after TrashWindow.
// An entry of an existing workspace is never purged, even when it cannot be put back.
func (s *Service) maintainTrash(ctx context.Context, now time.Time) error {
	entries, err := s.config.Storage.ListTrash(ctx)
	if err != nil || len(entries) == 0 {
		return err
	}
	ids := make([]string, len(entries))
	for index, entry := range entries {
		ids[index] = entry.WorkspaceID
	}
	existing := map[string]bool{}
	err = s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text FROM workspaces WHERE id = ANY($1::uuid[])`, ids)
		if err != nil {
			return err
		}
		found, err := pgx.CollectRows(rows, pgx.RowTo[string])
		for _, id := range found {
			existing[id] = true
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("check trash against workspaces: %w", err)
	}
	var failures []error
	for _, entry := range entries {
		switch {
		case existing[entry.WorkspaceID]:
			if now.Sub(entry.TrashedAt) < reconcileAfter {
				continue
			}
			if err := s.config.Storage.RestoreTrash(ctx, entry); err != nil {
				s.config.Logger.Error("trash entry of an existing workspace could not be restored; kept", "workspace_id", entry.WorkspaceID, "error", err)
				failures = append(failures, err)
				continue
			}
			s.config.Logger.Warn("files of a workspace that still exists were found in the trash and restored", "workspace_id", entry.WorkspaceID)
		case entry.TrashedAt.Before(now.Add(-TrashWindow)):
			if err := s.config.Storage.RemoveTrash(ctx, entry); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}
