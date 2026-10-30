// Package trialreminder emails a workspace's admins as its free trial runs out: once three days before
// the trial ends and once on the day it ends (the day in the workspace's timezone).
package trialreminder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
	"github.com/jackc/pgx/v5"
)

// The two reminders. A workspace first seen on its last day gets only the second.
const (
	kindThreeDays = "three_days"
	kindEndDay    = "end_day"
	lead          = 3 * 24 * time.Hour
)

// ErrNoMailer means a pass was asked for on a server that cannot send mail.
var ErrNoMailer = errors.New("trial reminders need outbound email (set SMTP_HOST and MAIL_FROM_DOMAIN)")

// Mailer hands one composed message to the mail relay (the shape of email.SMTPSender).
type Mailer interface {
	Send(ctx context.Context, from string, to []string, message []byte) error
}

// Config wires a Service.
type Config struct {
	DB     *database.DB
	Mailer Mailer
	// From is the platform sender address, for example noreply@MAIL_FROM_DOMAIN. ReplyTo, when set, is
	// where an admin's answer goes: the emails are signed by a person.
	From, ReplyTo string
	// Origin is the web address of the app; each email links to the workspace's own host (HelpCenterDomain).
	Origin           string
	HelpCenterDomain string
	// Now overrides the clock (tests).
	Now    func() time.Time
	Logger *slog.Logger
}

// Service sends the trial reminders.
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

// Run looks for due reminders now and then every interval, until ctx ends.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if sent, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
			s.config.Logger.Error("trial reminders", "error", err)
		} else if sent > 0 {
			s.config.Logger.Info("trial reminders", "sent", sent)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce sends every reminder that is due now and has not been sent, and says how many went out. A
// workspace that pays, or whose trial is over, gets none. A pass that fails for one workspace logs it and
// goes on with the rest.
func (s *Service) RunOnce(ctx context.Context) (int, error) {
	if s.config.Mailer == nil || s.config.From == "" {
		return 0, ErrNoMailer
	}
	now := s.config.Now()
	ids, err := s.candidates(ctx, now)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, id := range ids {
		count, err := s.remind(ctx, id, now)
		sent += count
		if err != nil {
			s.config.Logger.Error("trial reminder", "workspace_id", id, "error", err)
		}
	}
	return sent, nil
}

// candidates are the workspaces whose trial ends within the next three days.
func (s *Service) candidates(ctx context.Context, now time.Time) ([]string, error) {
	var ids []string
	err := s.config.DB.Privileged(ctx, database.PrivilegeTrialReminder, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text FROM workspaces WHERE trial_ends_at > $1 AND trial_ends_at <= $2 ORDER BY trial_ends_at`, now, now.Add(lead))
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	}, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("list trials about to end: %w", err)
	}
	return ids, nil
}

type admin struct{ id, email string }

// reminder is what one workspace's pass found due: the admins this pass claimed.
type reminder struct {
	id, name, slug, timezone, kind string
	trialEnd                       time.Time
	admins                         []admin
}

// remind claims, for each admin of the workspace who has not had this reminder for this trial end, the
// right to send it, then sends. The claim is committed first: two servers claiming at once cannot both win.
func (s *Service) remind(ctx context.Context, workspaceID string, now time.Time) (int, error) {
	var due *reminder
	err := s.config.DB.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		access, err := store.WorkspaceAccess(ctx, tx, workspaceID, now)
		if err != nil || access.Plan != domain.PlanTrial {
			return err
		}
		found := reminder{id: workspaceID, trialEnd: access.TrialEndsAt}
		if err := tx.QueryRow(ctx, `SELECT name, slug, timezone FROM workspaces WHERE id=$1`, workspaceID).Scan(&found.name, &found.slug, &found.timezone); err != nil {
			return err
		}
		if found.kind = dueKind(found.trialEnd, found.timezone, now); found.kind == "" {
			return nil
		}
		rows, err := tx.Query(ctx, `SELECT id::text, lower(email) FROM agents WHERE workspace_id=$1 AND role='admin' AND deactivated_at IS NULL ORDER BY 2`, workspaceID)
		if err != nil {
			return err
		}
		admins, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (admin, error) {
			var a admin
			return a, row.Scan(&a.id, &a.email)
		})
		if err != nil {
			return err
		}
		for _, a := range admins {
			tag, err := tx.Exec(ctx, `INSERT INTO workspace_trial_reminders(workspace_id,agent_id,kind,trial_ends_at,status,updated_at) VALUES($1,$2,$3,$4,'claimed',$5)
				ON CONFLICT DO NOTHING`, workspaceID, a.id, found.kind, found.trialEnd, now)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				found.admins = append(found.admins, a)
			}
		}
		due = &found
		return nil
	})
	if err != nil || due == nil {
		return 0, err
	}
	sent := 0
	for _, a := range due.admins {
		if s.send(ctx, *due, a, now) {
			sent++
		}
	}
	return sent, nil
}

// send mails one admin and settles the claim: sent, or rejected for good when the relay refuses the
// address. A failure that may pass gives the claim back, so the next pass tries again.
func (s *Service) send(ctx context.Context, due reminder, to admin, now time.Time) bool {
	ctx = context.WithoutCancel(ctx)
	status := "sent"
	message, err := s.compose(due, to.email, now)
	switch {
	case err != nil:
		s.config.Logger.Error("trial reminder cannot be composed", "workspace_id", due.id, "error", err)
		status = "rejected"
	default:
		switch err := s.config.Mailer.Send(ctx, s.config.From, []string{to.email}, message); {
		case err == nil:
		case email.IsPermanent(err):
			s.config.Logger.Warn("trial reminder rejected for good", "workspace_id", due.id, "slug", due.slug)
			status = "rejected"
		default:
			s.config.Logger.Error("trial reminder not sent, will retry", "workspace_id", due.id, "slug", due.slug, "error", err)
			s.settle(ctx, due, to, "", now)
			return false
		}
	}
	s.settle(ctx, due, to, status, now)
	return status == "sent"
}

// settle records how a claim ended; an empty status deletes the claim.
func (s *Service) settle(ctx context.Context, due reminder, to admin, status string, now time.Time) {
	err := s.config.DB.InWorkspace(ctx, due.id, func(tx pgx.Tx) error {
		if status == "" {
			_, err := tx.Exec(ctx, `DELETE FROM workspace_trial_reminders WHERE workspace_id=$1 AND agent_id=$2 AND kind=$3 AND trial_ends_at=$4`, due.id, to.id, due.kind, due.trialEnd)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE workspace_trial_reminders SET status=$5, updated_at=$6 WHERE workspace_id=$1 AND agent_id=$2 AND kind=$3 AND trial_ends_at=$4`,
			due.id, to.id, due.kind, due.trialEnd, status, now)
		return err
	})
	if err != nil {
		s.config.Logger.Error("trial reminder claim not settled", "workspace_id", due.id, "error", err)
	}
}

func (s *Service) compose(due reminder, to string, now time.Time) ([]byte, error) {
	from := s.config.From
	id, err := email.NewMessageID(from[strings.LastIndex(from, "@")+1:])
	if err != nil {
		return nil, err
	}
	billing := workspacehost.Origin(s.config.Origin, s.config.HelpCenterDomain, due.slug) + "/settings/billing"
	subject, body := text(due.kind, due.name, due.trialEnd, due.timezone, billing)
	return email.Compose(email.Outgoing{FromName: "Colliedesk", FromAddress: from, ReplyTo: s.config.ReplyTo, ToAddress: to, Subject: subject, Body: body,
		MessageID: id, Date: now.Truncate(time.Second), Standalone: true})
}

// dueKind says which reminder the workspace is due at now, or "" for none. The three-day reminder is due
// from three days before the trial ends; the last one from midnight at the start of the day it ends in the
// workspace's timezone. Once the trial is over nothing is due.
func dueKind(trialEnd time.Time, timezone string, now time.Time) string {
	if !now.Before(trialEnd) {
		return ""
	}
	loc := location(timezone)
	year, month, day := trialEnd.In(loc).Date()
	switch {
	case !now.Before(time.Date(year, month, day, 0, 0, 0, 0, loc)):
		return kindEndDay
	case !now.Before(trialEnd.Add(-lead)):
		return kindThreeDays
	}
	return ""
}

func location(timezone string) *time.Location {
	if loc, err := time.LoadLocation(timezone); err == nil {
		return loc
	}
	return time.UTC
}
