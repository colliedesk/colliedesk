package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Priorities response targets are set for, most urgent first.
var targetPriorities = []string{"urgent", "high", "normal", "low"}

// TargetMinutes are a priority's targets in business minutes; nil is no target.
type TargetMinutes struct {
	FirstResponse *int `json:"first_response_minutes"`
	Resolution    *int `json:"resolution_minutes"`
}

// ResponseTargets are a workspace's response targets (docs/user/response-targets.md). Timezone nil is
// the workspace's own.
type ResponseTargets struct {
	Enabled           bool                     `json:"enabled"`
	Timezone          *string                  `json:"timezone"`
	Hours             []domain.BusinessDay     `json:"hours"`
	Holidays          []domain.Holiday         `json:"holidays"`
	Targets           map[string]TargetMinutes `json:"targets"`
	WarnBeforeMinutes int                      `json:"warn_before_minutes"`
	ChannelIDs        []string                 `json:"channel_ids"`
	AlertEmail        bool                     `json:"alert_email"`
	EnabledAt         *time.Time               `json:"enabled_at"`
	UpdatedAt         *time.Time               `json:"updated_at"`
	// MyAlertEmail says whether the reader gets the reminder and overdue emails (their own setting).
	MyAlertEmail *bool `json:"my_alert_email,omitempty"`
}

// ResponseTargetsError says which part of the document is wrong.
type ResponseTargetsError struct{ Field, Message string }

func (e *ResponseTargetsError) Error() string { return e.Field + ": " + e.Message }

func defaultTargets() ResponseTargets {
	targets := ResponseTargets{Hours: []domain.BusinessDay{}, Holidays: []domain.Holiday{}, Targets: map[string]TargetMinutes{}, AlertEmail: true}
	for _, day := range domain.Weekdays {
		targets.Hours = append(targets.Hours, domain.BusinessDay{Day: day})
	}
	for _, priority := range targetPriorities {
		targets.Targets[priority] = TargetMinutes{}
	}
	return targets
}

func readResponseTargets(ctx context.Context, tx pgx.Tx, workspaceID string) (ResponseTargets, error) {
	targets := defaultTargets()
	var hours, holidays, byPriority []byte
	err := tx.QueryRow(ctx, `SELECT enabled,timezone,hours,holidays,targets,warn_before_minutes,channel_ids::text[],alert_email,enabled_at,updated_at
		FROM response_targets WHERE workspace_id=$1`, workspaceID).Scan(&targets.Enabled, &targets.Timezone, &hours, &holidays, &byPriority,
		&targets.WarnBeforeMinutes, &targets.ChannelIDs, &targets.AlertEmail, &targets.EnabledAt, &targets.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return targets, nil
	}
	if err != nil {
		return targets, fmt.Errorf("read response targets: %w", err)
	}
	stored := map[string]TargetMinutes{}
	if err := errors.Join(json.Unmarshal(hours, &targets.Hours), json.Unmarshal(holidays, &targets.Holidays), json.Unmarshal(byPriority, &stored)); err != nil {
		return targets, fmt.Errorf("read response targets: %w", err)
	}
	for priority, minutes := range stored {
		targets.Targets[priority] = minutes
	}
	for _, at := range []*time.Time{targets.EnabledAt, targets.UpdatedAt} {
		if at != nil {
			*at = at.UTC()
		}
	}
	return targets, nil
}

// GetResponseTargets reads the workspace's response targets; never set, they are off with no hours.
func (s *Store) GetResponseTargets(ctx context.Context, workspaceID string) (ResponseTargets, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ResponseTargets, error) {
		return readResponseTargets(ctx, tx, workspaceID)
	})
}

// workspacePolicy is the workspace's response targets as a policy, with the timezone it counts in; ok is
// false while they are off.
func workspacePolicy(ctx context.Context, tx pgx.Tx, workspaceID string) (ResponseTargets, domain.SLAPolicy, bool, error) {
	targets, err := readResponseTargets(ctx, tx, workspaceID)
	if err != nil || !targets.Enabled {
		return targets, domain.SLAPolicy{}, false, err
	}
	policy, err := targetsPolicy(ctx, tx, workspaceID, targets)
	return targets, policy, err == nil, err
}

func targetsPolicy(ctx context.Context, tx pgx.Tx, workspaceID string, targets ResponseTargets) (domain.SLAPolicy, error) {
	zone := ""
	if targets.Timezone != nil {
		zone = *targets.Timezone
	} else if err := tx.QueryRow(ctx, `SELECT timezone FROM workspaces WHERE id=$1`, workspaceID).Scan(&zone); err != nil {
		return domain.SLAPolicy{}, fmt.Errorf("read the workspace timezone: %w", err)
	}
	calendar, err := domain.NewBusinessCalendar(zone, targets.Hours, targets.Holidays)
	if err != nil {
		var wrong *domain.CalendarError
		if errors.As(err, &wrong) {
			return domain.SLAPolicy{}, &ResponseTargetsError{Field: wrong.Field, Message: wrong.Message}
		}
		return domain.SLAPolicy{}, err
	}
	policy := domain.SLAPolicy{Calendar: calendar, Targets: map[string]domain.SLATarget{}, WarnBefore: time.Duration(targets.WarnBeforeMinutes) * time.Minute}
	for priority, minutes := range targets.Targets {
		var target domain.SLATarget
		if minutes.FirstResponse != nil {
			target.FirstResponse = time.Duration(*minutes.FirstResponse) * time.Minute
		}
		if minutes.Resolution != nil {
			target.Resolution = time.Duration(*minutes.Resolution) * time.Minute
		}
		policy.Targets[priority] = target
	}
	return policy, nil
}

// maxTargetMinutes bounds a target: a year of business minutes.
const maxTargetMinutes = 525600

// SetResponseTargets replaces the workspace's response targets. Turning them on starts measuring the
// conversations opened from then; a change of any kind counts the running clocks again.
func (s *Store) SetResponseTargets(ctx context.Context, workspaceID, agentID string, input ResponseTargets) (ResponseTargets, error) {
	if input.Timezone != nil && *input.Timezone == "" {
		input.Timezone = nil
	}
	if input.Hours == nil {
		input.Hours = []domain.BusinessDay{}
	}
	if input.Holidays == nil {
		input.Holidays = []domain.Holiday{}
	}
	for index, holiday := range input.Holidays {
		if len([]rune(holiday.Name)) > 100 {
			return ResponseTargets{}, &ResponseTargetsError{Field: fmt.Sprintf("holidays.%d.name", index), Message: "Use 100 characters at most."}
		}
	}
	if len(input.Holidays) > 500 {
		return ResponseTargets{}, &ResponseTargetsError{Field: "holidays", Message: "Use 500 holidays at most."}
	}
	targets := map[string]TargetMinutes{}
	for priority, minutes := range input.Targets {
		known := false
		for _, name := range targetPriorities {
			known = known || name == priority
		}
		if !known {
			return ResponseTargets{}, &ResponseTargetsError{Field: "targets." + priority, Message: "Use urgent, high, normal or low."}
		}
		for field, value := range map[string]*int{"first_response_minutes": minutes.FirstResponse, "resolution_minutes": minutes.Resolution} {
			if value != nil && (*value < 1 || *value > maxTargetMinutes) {
				return ResponseTargets{}, &ResponseTargetsError{Field: "targets." + priority + "." + field, Message: "Use 1 business minute or more, or no target."}
			}
		}
		targets[priority] = minutes
	}
	if input.WarnBeforeMinutes < 0 || input.WarnBeforeMinutes > 10080 {
		return ResponseTargets{}, &ResponseTargetsError{Field: "warn_before_minutes", Message: "Use 0 to 10080 minutes."}
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ResponseTargets, error) {
		policy, err := targetsPolicy(ctx, tx, workspaceID, input)
		if err != nil {
			return ResponseTargets{}, err
		}
		// On, a clock must be able to run: an open day, and business time within reach of now.
		if input.Enabled {
			open := false
			for _, day := range input.Hours {
				open = open || day.Open
			}
			if !open {
				return ResponseTargets{}, &ResponseTargetsError{Field: "hours", Message: "Open at least one day to measure response targets."}
			}
			if !policy.Calendar.Open(s.now()) {
				return ResponseTargets{}, &ResponseTargetsError{Field: "holidays", Message: "The holidays close every open day for the next two years: no target could ever be due."}
			}
		}
		for index, id := range input.ChannelIDs {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channels WHERE workspace_id=$1 AND id::text=$2 AND deleted_at IS NULL)`, workspaceID, id).Scan(&exists); err != nil {
				return ResponseTargets{}, fmt.Errorf("find channel: %w", err)
			}
			if !exists {
				return ResponseTargets{}, &ResponseTargetsError{Field: fmt.Sprintf("channel_ids.%d", index), Message: "This workspace has no such channel."}
			}
		}
		hours, _ := json.Marshal(input.Hours)
		holidays, _ := json.Marshal(input.Holidays)
		byPriority, _ := json.Marshal(targets)
		// alerts_since: emails turned (back) on send nothing for what was reached while they were off.
		if _, err := tx.Exec(ctx, `INSERT INTO response_targets(workspace_id,enabled,timezone,hours,holidays,targets,warn_before_minutes,channel_ids,alert_email,enabled_at,updated_at,updated_by_agent_id,
				alerts_since)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8::uuid[],$9,CASE WHEN $2 THEN now() END,now(),NULLIF($10,'')::uuid,CASE WHEN $2 AND $9 THEN now() END)
			ON CONFLICT (workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled,timezone=EXCLUDED.timezone,hours=EXCLUDED.hours,holidays=EXCLUDED.holidays,
				targets=EXCLUDED.targets,warn_before_minutes=EXCLUDED.warn_before_minutes,channel_ids=EXCLUDED.channel_ids,alert_email=EXCLUDED.alert_email,
				enabled_at=CASE WHEN EXCLUDED.enabled AND NOT response_targets.enabled THEN now() WHEN EXCLUDED.enabled THEN response_targets.enabled_at END,
				alerts_since=CASE WHEN EXCLUDED.enabled AND EXCLUDED.alert_email AND NOT (response_targets.enabled AND response_targets.alert_email) THEN now()
					ELSE response_targets.alerts_since END,
				updated_at=now(),updated_by_agent_id=EXCLUDED.updated_by_agent_id`,
			workspaceID, input.Enabled, input.Timezone, hours, holidays, byPriority, input.WarnBeforeMinutes, input.ChannelIDs, input.AlertEmail, agentID); err != nil {
			return ResponseTargets{}, fmt.Errorf("save response targets: %w", err)
		}
		if err := targetsChanged(ctx, tx, workspaceID); err != nil {
			return ResponseTargets{}, err
		}
		return readResponseTargets(ctx, tx, workspaceID)
	})
}

// targetsChanged is where the conversations' clocks hear of a change of the targets (their next commit
// counts them again).
var targetsChanged = func(context.Context, pgx.Tx, string) error { return nil }
