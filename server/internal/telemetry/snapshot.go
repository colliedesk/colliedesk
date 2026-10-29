package telemetry

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

const day = 24 * time.Hour

// snapshot is the health of one Colliedesk Cloud workspace at one moment: counts and enums plus the
// workspace's name and slug, so the team knows which customer it is. Never an address or anything
// a person wrote in a conversation. docs/user/telemetry.md lists every field.
type snapshot struct {
	workspaceID string
	properties  map[string]any
}

// readSnapshots counts every workspace's activity inside the privileged telemetry transaction.
// Each metric family is one grouped query over all workspaces; only the counts leave it.
func readSnapshots(ctx context.Context, tx pgx.Tx, now time.Time) ([]snapshot, error) {
	type workspace struct {
		id, name, slug   string
		created, trialTo time.Time
	}
	var workspaces []workspace
	rows, err := tx.Query(ctx, `SELECT id::text,name,slug,created_at,trial_ends_at FROM workspaces`)
	if err != nil {
		return nil, fmt.Errorf("read workspaces: %w", err)
	}
	for rows.Next() {
		var w workspace
		if err := rows.Scan(&w.id, &w.name, &w.slug, &w.created, &w.trialTo); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read workspaces: %w", err)
		}
		workspaces = append(workspaces, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read workspaces: %w", err)
	}

	agents, err := countsByWorkspace(ctx, tx, 1, `SELECT workspace_id::text,count(*) FROM agents
		WHERE deactivated_at IS NULL GROUP BY workspace_id`)
	if err != nil {
		return nil, fmt.Errorf("count agents: %w", err)
	}
	// A token's last_seen_at is an AI agent working, not a person signing in, so only sessions
	// without a scope count as active agents.
	signedIn, err := countsByWorkspace(ctx, tx, 2, `SELECT workspace_id::text,
		count(DISTINCT agent_id) FILTER (WHERE last_seen_at >= $1::timestamptz - interval '7 days'),
		count(DISTINCT agent_id) FILTER (WHERE last_seen_at >= $1::timestamptz - interval '30 days')
		FROM sessions WHERE scope IS NULL GROUP BY workspace_id`, now)
	if err != nil {
		return nil, fmt.Errorf("count signed-in agents: %w", err)
	}
	tokens, err := countsByWorkspace(ctx, tx, 1, `SELECT workspace_id::text,count(*) FROM sessions
		WHERE scope IS NOT NULL AND revoked_at IS NULL AND expires_at > $1::timestamptz GROUP BY workspace_id`, now)
	if err != nil {
		return nil, fmt.Errorf("count tokens: %w", err)
	}
	// Replies imported from another helpdesk keep their original time; only replies sent here count.
	replies, err := countsByWorkspace(ctx, tx, 3, `SELECT workspace_id::text,
		count(*) FILTER (WHERE created_at >= $1::timestamptz - interval '7 days'),
		count(*),
		count(*) FILTER (WHERE created_at >= $1::timestamptz - interval '7 days' AND agent_token_id IS NOT NULL)
		FROM messages WHERE kind = 'outgoing' AND origin IS NULL AND created_at >= $1::timestamptz - interval '30 days'
		GROUP BY workspace_id`, now)
	if err != nil {
		return nil, fmt.Errorf("count replies: %w", err)
	}
	conversations, err := countsByWorkspace(ctx, tx, 2, `SELECT workspace_id::text,
		count(*) FILTER (WHERE status = 'open'),
		count(*) FILTER (WHERE created_at >= $1::timestamptz - interval '7 days')
		FROM conversations GROUP BY workspace_id`, now)
	if err != nil {
		return nil, fmt.Errorf("count conversations: %w", err)
	}
	emailChannels, err := workspaceSet(ctx, tx, `SELECT DISTINCT workspace_id::text FROM email_channels`)
	if err != nil {
		return nil, fmt.Errorf("read email channels: %w", err)
	}
	webForms, err := workspaceSet(ctx, tx, `SELECT DISTINCT f.workspace_id::text FROM web_form_channels f JOIN channels c ON c.workspace_id=f.workspace_id AND c.id=f.channel_id WHERE c.enabled`)
	if err != nil {
		return nil, fmt.Errorf("read web forms: %w", err)
	}
	imports, err := workspaceSet(ctx, tx, `SELECT DISTINCT workspace_id::text FROM import_items WHERE source LIKE 'freshdesk:%'`)
	if err != nil {
		return nil, fmt.Errorf("read imports: %w", err)
	}
	// Time to first result: from sign-up to the first in-app import whose reconcile report is ready
	// (OK, FAILED or UNVERIFIED). A report still being retried is not final; an undo keeps the time.
	firstResults, err := countsByWorkspace(ctx, tx, 1, `SELECT j.workspace_id::text,
		floor(extract(epoch FROM min(j.finished_at) - w.created_at))::bigint
		FROM import_jobs j JOIN workspaces w ON w.id = j.workspace_id
		WHERE j.kind = 'import' AND j.state = 'finished' AND j.status IS NOT NULL
		GROUP BY j.workspace_id, w.created_at`)
	if err != nil {
		return nil, fmt.Errorf("read first results: %w", err)
	}
	// The help chat: AI answers this month (UTC, the month its limits count) and chats handed to the team.
	chats, err := countsByWorkspace(ctx, tx, 2, `SELECT workspace_id::text,
		count(*) FILTER (WHERE kind='answer'), count(*) FILTER (WHERE kind='handoff') FROM (
			SELECT workspace_id,'answer' AS kind FROM ai_chat_turns WHERE status='answered' AND created_at >= date_trunc('month',$1::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
			UNION ALL SELECT workspace_id,'handoff' FROM ai_chat_sessions WHERE conversation_id IS NOT NULL AND created_at >= date_trunc('month',$1::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') c
		GROUP BY workspace_id`, now)
	if err != nil {
		return nil, fmt.Errorf("count help chats: %w", err)
	}
	helpChats, err := workspaceSet(ctx, tx, `SELECT DISTINCT workspace_id::text FROM channels WHERE type='help_chat' AND enabled`)
	if err != nil {
		return nil, fmt.Errorf("read help chats: %w", err)
	}
	subscriptions, err := store.LoadAllSubscriptions(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("read subscriptions: %w", err)
	}

	snapshots := make([]snapshot, 0, len(workspaces))
	for _, w := range workspaces {
		access := domain.NewAccess(subscriptions[w.id], w.trialTo, now)
		var trialDaysLeft any // null unless the workspace is on its trial
		if access.Plan == domain.PlanTrial {
			trialDaysLeft = int64(math.Ceil(float64(w.trialTo.Sub(now)) / float64(day)))
		}
		var firstResult any // null until the first import report is ready
		if row, ok := firstResults[w.id]; ok {
			firstResult = max(row[0], 0)
		}
		replyCounts, signIns, convCounts, chatCounts := replies.at(w.id, 3), signedIn.at(w.id, 2), conversations.at(w.id, 2), chats.at(w.id, 2)
		snapshots = append(snapshots, snapshot{workspaceID: w.id, properties: map[string]any{
			"plan":                         access.Plan,
			"trial_days_left":              trialDaysLeft,
			"days_since_created":           max(int64(now.Sub(w.created)/day), 0),
			"agents_total":                 agents.at(w.id, 1)[0],
			"agents_active_7d":             signIns[0],
			"agents_active_30d":            signIns[1],
			"replies_7d":                   replyCounts[0],
			"replies_30d":                  replyCounts[1],
			"ai_agent_replies_7d":          replyCounts[2],
			"conversations_open":           convCounts[0],
			"conversations_created_7d":     convCounts[1],
			"email_channel":                emailChannels[w.id],
			"web_form":                     webForms[w.id],
			"tokens_active":                tokens.at(w.id, 1)[0],
			"help_chat":                    helpChats[w.id],
			"chat_answers_month":           chatCounts[0],
			"chat_handoffs_month":          chatCounts[1],
			"freshdesk_import":             imports[w.id],
			"time_to_first_result_seconds": firstResult,
			"workspace_name":               w.name,
			"workspace_slug":               w.slug,
		}})
	}
	slices.SortFunc(snapshots, func(a, b snapshot) int { return strings.Compare(a.workspaceID, b.workspaceID) })
	return snapshots, nil
}

// counts holds the numbers of one grouped query per workspace id.
type counts map[string][]int64

// at returns the workspace's row, or zeros when it has none (nothing to count there).
func (c counts) at(workspaceID string, width int) []int64 {
	if row, ok := c[workspaceID]; ok {
		return row
	}
	return make([]int64, width)
}

// countsByWorkspace runs a query that returns a workspace id followed by width counts.
func countsByWorkspace(ctx context.Context, tx pgx.Tx, width int, sql string, args ...any) (counts, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := counts{}
	for rows.Next() {
		var id string
		row := make([]int64, width)
		dest := []any{&id}
		for i := range row {
			dest = append(dest, &row[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		result[id] = row
	}
	return result, rows.Err()
}

// workspaceSet runs a query that returns workspace ids.
func workspaceSet(ctx context.Context, tx pgx.Tx, sql string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}
