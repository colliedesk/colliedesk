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
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// ErrNoMailer means a notice is due but the server has no way to send mail, so nothing is deleted.
var ErrNoMailer = errors.New("a deletion notice is due but outbound email is not configured (set SMTP_HOST and MAIL_FROM_DOMAIN)")

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// Config wires a Service. Mailer may be nil: notices then fail with ErrNoMailer and no workspace
// is ever deleted without one.
type Config struct {
	DB      *database.DB
	Storage attachments.Storage
	Mailer  Mailer
	// From is the platform sender address of notices, for example noreply@MAIL_FROM_DOMAIN.
	From string
	// Origin is the web address of the app, quoted in notices.
	Origin string
	Now    func() time.Time
	Logger *slog.Logger
}

// Service runs the retention policy of locked workspaces.
type Service struct{ config Config }

// New returns a Service; Now defaults to the system clock and Logger to slog.Default.
func New(config Config) *Service {
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
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

// Result counts what Run did.
type Result struct{ Noticed, Deleted, Reset int }

// state is one workspace as the policy sees it, read inside a privileged transaction.
type state struct {
	Item
	Name string
}

// candidates lists the workspaces that can need an action: locked long enough for a notice to be
// due, or already carrying a notice that may need a reset.
func (s *Service) candidates(ctx context.Context) ([]string, error) {
	cutoff := s.config.Now().AddDate(-domain.RetentionYears, 0, 0).Add(domain.RetentionNoticeLead)
	var ids []string
	err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text FROM workspaces WHERE trial_ends_at <= $1 OR retention_notice_sent_at IS NOT NULL ORDER BY created_at, id`, cutoff)
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

// evaluate reads one workspace and decides. With lock it holds the workspace row until the
// transaction ends, so two runners never act on the same workspace at once.
func (s *Service) evaluate(ctx context.Context, tx pgx.Tx, id string, lock bool, now time.Time) (state, error) {
	query := `SELECT slug,name,trial_ends_at,retention_notice_sent_at FROM workspaces WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	var st state
	var trialEndsAt time.Time
	st.WorkspaceID = id
	if err := tx.QueryRow(ctx, query, id).Scan(&st.Slug, &st.Name, &trialEndsAt, &st.NoticeSentAt); err != nil {
		return state{}, err
	}
	subscriptions, err := store.LoadAccessHistory(ctx, tx, id)
	if err != nil {
		return state{}, err
	}
	since, locked := domain.LockedSince(subscriptions, trialEndsAt, now)
	st.LockedSince = since
	if locked {
		st.NoticeAt, st.DeleteAt = domain.RetentionDates(since)
	}
	st.Action = Decide(locked, since, st.NoticeSentAt, now)
	return st, nil
}

// Plan lists what Run would do now, without sending, recording or deleting anything.
func (s *Service) Plan(ctx context.Context) ([]Item, error) {
	now := s.config.Now()
	ids, err := s.candidates(ctx)
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, id := range ids {
		var st state
		err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) (err error) {
			st, err = s.evaluate(ctx, tx, id, false, now)
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

// Run sends due notices, clears stale ones and deletes the workspaces that are due. A failure on
// one workspace is logged and returned at the end; the others still run. It is safe to run again
// and from two processes at once.
func (s *Service) Run(ctx context.Context) (Result, error) {
	ids, err := s.candidates(ctx)
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
		var done Action
		var st state
		err := s.config.DB.Privileged(ctx, database.PrivilegeRetention, func(tx pgx.Tx) (err error) {
			now := s.config.Now()
			if st, err = s.evaluate(ctx, tx, id, true, now); err != nil {
				return err
			}
			switch st.Action {
			case ActionReset:
				if _, err = tx.Exec(ctx, `UPDATE workspaces SET retention_notice_sent_at=NULL WHERE id=$1`, id); err == nil {
					done = ActionReset
				}
			case ActionNotice:
				done, err = s.notify(ctx, tx, st, now)
			case ActionDelete:
				err = s.remove(ctx, tx, st)
				if err == nil {
					done = ActionDelete
				}
			}
			return err
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			continue
		case err != nil:
			s.config.Logger.Error("retention", "workspace_id", id, "slug", st.Slug, "action", st.Action, "error", err)
			failures = append(failures, fmt.Errorf("workspace %s (%s): %s: %w", id, st.Slug, st.Action, err))
		}
		switch done {
		case ActionNotice:
			result.Noticed++
		case ActionDelete:
			result.Deleted++
		case ActionReset:
			result.Reset++
		}
	}
	return result, errors.Join(failures...)
}
