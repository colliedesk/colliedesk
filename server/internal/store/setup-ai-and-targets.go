package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// The setup's AI drafts and reply targets steps (docs/user/setup-guide.md).

// ReplyTargetPreset is a set of reply targets the setup offers, in business minutes. A day is the open hours
// of one business day: 9 hours with 09:00 to 18:00.
type ReplyTargetPreset struct {
	Key     string                   `json:"key"`
	Name    string                   `json:"name"`
	Targets map[string]TargetMinutes `json:"targets"`
}

// ReplyTargetSuggestion is the preset that fits the team, and what it is based on: history (the median first
// reply of the imported conversations opened in the last 30 days), no_history or import_running.
type ReplyTargetSuggestion struct {
	Preset                  string `json:"preset"`
	Basis                   string `json:"basis"`
	MedianFirstReplyMinutes *int   `json:"median_first_reply_minutes"`
}

const businessDayMinutes = 9 * 60

func presetTargets(minutes ...int) map[string]TargetMinutes {
	targets := map[string]TargetMinutes{}
	for i, priority := range targetPriorities {
		first, resolve := minutes[2*i], minutes[2*i+1]
		targets[priority] = TargetMinutes{FirstResponse: &first, Resolution: &resolve}
	}
	return targets
}

// ReplyTargetPresets are the setup's presets, fastest first; Same day is what Settings suggests too.
func ReplyTargetPresets() []ReplyTargetPreset {
	day := businessDayMinutes
	return []ReplyTargetPreset{
		{Key: "within_the_hour", Name: "Within the hour", Targets: presetTargets(15, 120, 30, 240, 60, day, 240, 2*day)},
		{Key: "same_day", Name: "Same day", Targets: presetTargets(60, 240, 120, day, 480, 3*day, day, 5*day)},
		{Key: "next_business_day", Name: "Next business day", Targets: presetTargets(240, day, day, 2*day, day, 3*day, 2*day, 5*day)},
	}
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

// setupAIAndTargets reads the AI drafts and reply targets steps and the reply targets suggestion. Drafts are on
// once reply drafts write one for each new customer message (on, at Suggest); off, they have nothing to read
// without a finished import or a published article, and cannot run without built-in AI (serverDrafts) or a
// provider of the workspace's own.
func setupAIAndTargets(ctx context.Context, tx pgx.Tx, workspaceID string, now time.Time, serverDrafts bool, importState string) (aiDrafts, replyTargets string, suggestion ReplyTargetSuggestion, err error) {
	var draftsOn, ownProvider, articles, targetsOn bool
	var median *float64
	err = tx.QueryRow(ctx, `SELECT
			EXISTS (SELECT 1 FROM ai_slots WHERE workspace_id=$1 AND slot='draft' AND enabled AND default_level='suggest'),
			EXISTS (SELECT 1 FROM ai_providers WHERE workspace_id=$1),
			EXISTS (SELECT 1 FROM kb_articles WHERE workspace_id=$1 AND status='published' AND deleted_at IS NULL),
			COALESCE((SELECT enabled FROM response_targets WHERE workspace_id=$1), false),
			(SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM r.first_reply - c.created_at) / 60)
				FROM conversations c
				CROSS JOIN LATERAL (SELECT min(m.created_at) AS first_reply FROM messages m
					WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='outgoing') r
				WHERE c.workspace_id=$1 AND c.origin IS NOT NULL AND c.created_at>=$2 AND r.first_reply IS NOT NULL)`,
		workspaceID, now.Add(-30*24*time.Hour)).Scan(&draftsOn, &ownProvider, &articles, &targetsOn, &median)
	if err != nil {
		return "", "", suggestion, err
	}
	switch {
	case draftsOn:
		aiDrafts = "on"
	case !serverDrafts && !ownProvider:
		aiDrafts = "unavailable"
	case importState != "done" && !articles:
		aiDrafts = "nothing_to_read"
	default:
		aiDrafts = "none"
	}
	replyTargets = "none"
	if targetsOn {
		replyTargets = "on"
	}
	suggestion = ReplyTargetSuggestion{Preset: "same_day", Basis: "no_history"}
	switch {
	case importState == "running":
		suggestion.Basis = "import_running"
	case median != nil:
		minutes := int(*median + 0.5)
		suggestion = ReplyTargetSuggestion{Preset: presetForMedian(minutes), Basis: "history", MedianFirstReplyMinutes: &minutes}
	}
	return aiDrafts, replyTargets, suggestion, nil
}
