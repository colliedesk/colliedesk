package store

import (
	"context"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// The setup's AI drafts and reply targets steps (docs/user/setup-guide.md).

// ReplyTargetPreset is a set of reply targets the setup offers, in business minutes. A day is one business day
// of the hours the preset applies with (ReplyTargetHours).
type ReplyTargetPreset struct {
	Key     string                   `json:"key"`
	Name    string                   `json:"name"`
	Targets map[string]TargetMinutes `json:"targets"`
}

// ReplyTargetHours are the business hours a preset applies with: the workspace's own when Settings has an open
// day (Kept), which a preset never replaces, else Monday to Friday, 09:00 to 18:00. DayMinutes is one business
// day of them.
type ReplyTargetHours struct {
	Kept       bool                 `json:"kept"`
	DayMinutes int                  `json:"day_minutes"`
	Hours      []domain.BusinessDay `json:"hours"`
}

// ReplyTargetSuggestion is the preset that fits the team, and what it is based on: history (the median first
// reply of the imported conversations opened in the last 30 days), no_history or import_running.
type ReplyTargetSuggestion struct {
	Preset                  string `json:"preset"`
	Basis                   string `json:"basis"`
	MedianFirstReplyMinutes *int   `json:"median_first_reply_minutes"`
}

func presetTargets(minutes ...int) map[string]TargetMinutes {
	targets := map[string]TargetMinutes{}
	for i, priority := range targetPriorities {
		first, resolve := minutes[2*i], minutes[2*i+1]
		targets[priority] = TargetMinutes{FirstResponse: &first, Resolution: &resolve}
	}
	return targets
}

// ReplyTargetPresets are the setup's presets, fastest first, with day minutes to a business day. Settings
// suggests no targets of its own: these exist only in the setup.
func ReplyTargetPresets(day int) []ReplyTargetPreset {
	return []ReplyTargetPreset{
		{Key: "within_the_hour", Name: "Within the hour", Targets: presetTargets(15, 120, 30, 240, 60, day, 240, 2*day)},
		{Key: "same_day", Name: "Same day", Targets: presetTargets(60, 240, 120, day, 480, 3*day, day, 5*day)},
		{Key: "next_business_day", Name: "Next business day", Targets: presetTargets(240, day, day, 2*day, day, 3*day, 2*day, 5*day)},
	}
}

// replyTargetHours are the hours a preset applies with, read from the workspace's response targets.
func replyTargetHours(stored []domain.BusinessDay) ReplyTargetHours {
	for _, day := range stored {
		if day.Open {
			return ReplyTargetHours{Kept: true, DayMinutes: businessDayMinutes(stored), Hours: stored}
		}
	}
	hours := make([]domain.BusinessDay, 0, len(domain.Weekdays))
	for _, day := range domain.Weekdays {
		open := day != "sat" && day != "sun"
		item := domain.BusinessDay{Day: day, Open: open}
		if open {
			item.From, item.To = "09:00", "18:00"
		}
		hours = append(hours, item)
	}
	return ReplyTargetHours{DayMinutes: 9 * 60, Hours: hours}
}

// businessDayMinutes is one business day of the hours: the length most open days have, the longest on a tie.
// Without an open day of positive length it is 540, the length of 09:00 to 18:00.
func businessDayMinutes(hours []domain.BusinessDay) int {
	count := map[int]int{}
	best := 0
	for _, day := range hours {
		if !day.Open {
			continue
		}
		length := clockMinutes(day.To) - clockMinutes(day.From)
		if length <= 0 {
			continue
		}
		count[length]++
		if best == 0 || count[length] > count[best] || (count[length] == count[best] && length > best) {
			best = length
		}
	}
	if best == 0 {
		return 9 * 60
	}
	return best
}

// clockMinutes reads "08:30" (or "24:00") as minutes after midnight; the hours were validated when saved.
func clockMinutes(clock string) int {
	var hours, minutes int
	if _, err := fmt.Sscanf(clock, "%d:%d", &hours, &minutes); err != nil {
		return 0
	}
	return hours*60 + minutes
}

// presetForMedian is the preset a median first reply fits: up to an hour, up to 8 hours, or longer.
func presetForMedian(minutes int) string {
	switch {
	case minutes <= 60:
		return "within_the_hour"
	case minutes <= 480:
		return "same_day"
	}
	return "next_business_day"
}

// setupAIAndTargets reads the AI drafts and reply targets steps, the hours a preset applies with and the reply
// targets suggestion. Drafts are on once reply drafts write one for each new customer message (on, at Suggest);
// off, they have nothing to read without a finished import or a published article, and cannot run without
// built-in AI (serverDrafts) or a provider of the workspace's own. The suggestion's median is read only when
// someone can use it: targets off, no import running, and a caller who may change the targets; else it is nil.
func setupAIAndTargets(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time, options SetupOptions, importState string) (
	aiDrafts, replyTargets string, hours ReplyTargetHours, suggestion *ReplyTargetSuggestion, err error) {
	var draftsOn, ownProvider, articles bool
	err = tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM ai_slots WHERE workspace_id=$1 AND slot='draft' AND enabled AND default_level='suggest'),
			EXISTS (SELECT 1 FROM ai_providers WHERE workspace_id=$1),
			EXISTS (SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND status='published' AND deleted_at IS NULL)`,
		workspaceID).Scan(&draftsOn, &ownProvider, &articles)
	if err != nil {
		return "", "", hours, nil, err
	}
	targets, err := readResponseTargets(ctx, tx, workspaceID)
	if err != nil {
		return "", "", hours, nil, err
	}
	switch {
	case draftsOn:
		aiDrafts = "on"
	case !options.ServerDrafts && !ownProvider:
		aiDrafts = "unavailable"
	case importState != "done" && !articles:
		aiDrafts = "nothing_to_read"
	default:
		aiDrafts = "none"
	}
	replyTargets = "none"
	if targets.Enabled {
		replyTargets = "on"
	}
	hours = replyTargetHours(targets.Hours)
	if targets.Enabled || !options.CanSetTargets {
		return aiDrafts, replyTargets, hours, nil, nil
	}
	suggestion = &ReplyTargetSuggestion{Preset: "same_day", Basis: "no_history"}
	if importState == "running" {
		suggestion.Basis = "import_running"
		return aiDrafts, replyTargets, hours, suggestion, nil
	}
	median, err := medianFirstReply(ctx, tx, workspaceID, now)
	if err != nil || median == nil {
		return aiDrafts, replyTargets, hours, suggestion, err
	}
	minutes := int(*median + 0.5)
	suggestion = &ReplyTargetSuggestion{Preset: presetForMedian(minutes), Basis: "history", MedianFirstReplyMinutes: &minutes}
	return aiDrafts, replyTargets, hours, suggestion, nil
}

// medianFirstReply is the median wall-clock minutes from an imported conversation's start to its first reply,
// over those opened in the last 30 days; a notice the channel sent by itself is no reply. Nil with none answered.
func medianFirstReply(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time) (*float64, error) {
	var median *float64
	err := tx.QueryRow(ctx, `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM r.first_reply - c.created_at) / 60)
		FROM conversations c
		CROSS JOIN LATERAL (SELECT min(m.created_at) AS first_reply FROM messages m
			WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='outgoing' AND m.notice_kind IS NULL) r
		WHERE c.workspace_id=$1 AND c.origin IS NOT NULL AND c.created_at>=$2 AND r.first_reply IS NOT NULL`,
		workspaceID, now.Add(-30*24*time.Hour)).Scan(&median)
	if err != nil {
		return nil, fmt.Errorf("read the median first reply: %w", err)
	}
	return median, nil
}
