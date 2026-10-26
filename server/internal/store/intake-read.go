package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// IntakeGathering is a card of the intake queue as the API shows it.
type IntakeGathering struct {
	ID        string  `json:"id"`
	ChannelID string  `json:"channel_id"`
	Scope     string  `json:"scope"`
	ThreadID  *string `json:"thread_id"`
	// LaneKey names the lane (place and sender) whose gatherings are decided one after the other.
	LaneKey           string       `json:"lane_key"`
	ContactID         *string      `json:"contact_id"`
	State             string       `json:"state"`
	ReviewReason      *string      `json:"review_reason"`
	Rules             IntakeRules  `json:"rules"`
	ConversationID    *string      `json:"conversation_id"`
	DecidedBy         *string      `json:"decided_by"`
	DecidedAt         *time.Time   `json:"decided_at"`
	DecidedByAgentID  *string      `json:"decided_by_agent_id"`
	IgnoreReason      *string      `json:"ignore_reason"`
	Fallback          bool         `json:"fallback"`
	OpenedAt          time.Time    `json:"opened_at"`
	ClosedAt          time.Time    `json:"closed_at"`
	DecideBy          *time.Time   `json:"decide_by"`
	RequeuedAt        *time.Time   `json:"requeued_at"`
	RequeuedByAgentID *string      `json:"requeued_by_agent_id"`
	ItemCount         int          `json:"item_count"`
	Sender            ChatSender   `json:"sender"`
	Place             *IntakePlace `json:"place"`
	// Preview is the beginning of what the customer wrote first.
	Preview string `json:"preview"`
	// Suggestion is what AI suggested and did not do, the newest, while the card waits or the rules
	// decided it, unless a person decided since.
	Suggestion *IntakeSuggestion `json:"suggestion"`
}

// IntakeSuggestion is what AI suggested of a card and did not do, with the conversations it names.
type IntakeSuggestion struct {
	DecisionID             string    `json:"decision_id"`
	Action                 string    `json:"action"`
	TargetConversationID   *string   `json:"target_conversation_id"`
	TargetSubject          *string   `json:"target_subject"`
	TargetStatus           *string   `json:"target_status"`
	RunnerUpConversationID *string   `json:"runner_up_conversation_id"`
	RunnerUpSubject        *string   `json:"runner_up_subject"`
	RunnerUpConfidence     *float64  `json:"runner_up_confidence"`
	Confidence             *float64  `json:"confidence"`
	Command                *string   `json:"command"`
	Subject                *string   `json:"subject"`
	ReplyKind              *string   `json:"reply_kind"`
	IgnoreReason           *string   `json:"ignore_reason"`
	Reason                 *string   `json:"reason"`
	NotAppliedReason       string    `json:"not_applied_reason"`
	CreatedAt              time.Time `json:"created_at"`
}

// IntakeRules is what the channel's rules decided when the gathering closed.
type IntakeRules struct {
	Action string `json:"action"`
	Rule   string `json:"rule"`
	// Basis is match when the batch carried evidence, default when the scope decided without.
	Basis                string  `json:"basis"`
	TargetConversationID *string `json:"target_conversation_id"`
}

// IntakePlace is the chat place a gathering was said in.
type IntakePlace struct {
	Kind string  `json:"kind"`
	Name *string `json:"name"`
}

// IntakeCandidate is a conversation a card may join without its reply going to someone else.
type IntakeCandidate struct {
	ConversationID string    `json:"conversation_id"`
	Subject        string    `json:"subject"`
	Status         string    `json:"status"`
	ContactID      string    `json:"contact_id"`
	ContactName    string    `json:"contact_name"`
	ChannelID      *string   `json:"channel_id"`
	UpdatedAt      time.Time `json:"updated_at"`
	// Resolved says joining it reopens it.
	Resolved bool `json:"resolved"`
}

// IntakeDecisionView is one decision of a gathering as the API shows it.
type IntakeDecisionView struct {
	ID                     string   `json:"id"`
	Decider                string   `json:"decider"`
	Basis                  string   `json:"basis"`
	Action                 string   `json:"action"`
	TargetConversationID   *string  `json:"target_conversation_id"`
	RunnerUpConversationID *string  `json:"runner_up_conversation_id"`
	RunnerUpConfidence     *float64 `json:"runner_up_confidence"`
	Command                *string  `json:"command"`
	Subject                *string  `json:"subject"`
	ReplyKind              *string  `json:"reply_kind"`
	Confidence             *float64 `json:"confidence"`
	Reason                 *string  `json:"reason"`
	Applied                bool     `json:"applied"`
	IgnoreReason           *string  `json:"ignore_reason"`
	NotAppliedReason       *string  `json:"not_applied_reason"`
	Fallback               bool     `json:"fallback"`
	CorrectsDecisionID     *string  `json:"corrects_decision_id"`
	FollowsDecisionID      *string  `json:"follows_decision_id"`
	// ErrorCode says what failed when AI's answer was not a decision (target_invalid, invalid_answer).
	ErrorCode *string   `json:"error_code"`
	Model     *string   `json:"model"`
	AgentID   *string   `json:"agent_id"`
	ViaToken  bool      `json:"via_token"`
	CreatedAt time.Time `json:"created_at"`
}

// IntakeGatheringDetail is a card with everything its customer said and every decision made on it.
type IntakeGatheringDetail struct {
	IntakeGathering
	Items      []ChatItem           `json:"items"`
	Decisions  []IntakeDecisionView `json:"decisions"`
	Candidates []IntakeCandidate    `json:"candidates"`
	// Commands are the channel's commands that open a request.
	Commands []string `json:"commands"`
}

// IntakeGatheringPage is a page of cards.
type IntakeGatheringPage struct {
	Items []IntakeGathering `json:"items"`
	Next  *string           `json:"next_cursor"`
}

// Lists of the queue, as GET /api/v1/intake/gatherings names them.
const (
	IntakeListReview   = "review"
	IntakeListDeciding = "deciding"
	IntakeListIgnored  = "ignored"
	IntakeListDecided  = "decided"
)

// MaxIntakePage is the most cards a page holds.
const MaxIntakePage = 50

const gatheringColumns = `g.id::text,g.channel_id::text,g.scope,g.thread_id::text,g.lane_key,g.contact_id::text,g.state,g.review_reason,g.rules_action,g.rules_rule,
	g.rules_basis,g.rules_target::text,g.conversation_id::text,g.decided_by,g.decided_at,g.decided_by_agent_id::text,g.ignore_reason,g.fallback,g.opened_at,g.closed_at,
	g.decide_by,g.requeued_at,g.requeued_by_agent_id::text,
	(SELECT count(*) FROM channel_thread_items i WHERE i.workspace_id=g.workspace_id AND i.gathering_id=g.id),
	f.sender_kind,f.sender_value,f.sender_name,t.kind,t.name,COALESCE(left(f.body,280),''),
	s.id::text,s.action,s.target_conversation_id::text,s.target_subject,s.target_status,s.runner_up_conversation_id::text,s.runner_up_subject,
	s.runner_up_confidence::float8,s.confidence::float8,s.command,s.subject,s.reply_kind,s.ignore_reason,s.reason,s.not_applied_reason,s.created_at`

const gatheringFrom = `FROM intake_gatherings g LEFT JOIN LATERAL (SELECT i.sender_kind,i.sender_value,i.sender_name,i.body FROM channel_thread_items i
	WHERE i.workspace_id=g.workspace_id AND i.gathering_id=g.id ORDER BY i.occurred_at,i.id LIMIT 1) f ON true
	LEFT JOIN channel_threads t ON t.workspace_id=g.workspace_id AND t.id=g.thread_id
	LEFT JOIN LATERAL (SELECT d.id,d.action,d.target_conversation_id,tc.subject AS target_subject,tc.status AS target_status,d.runner_up_conversation_id,
			rc.subject AS runner_up_subject,d.runner_up_confidence,d.confidence,d.command,d.subject,d.reply_kind,d.ignore_reason,d.reason,d.not_applied_reason,d.created_at
		FROM intake_decisions d
		LEFT JOIN conversations tc ON tc.workspace_id=d.workspace_id AND tc.id=d.target_conversation_id
		LEFT JOIN conversations rc ON rc.workspace_id=d.workspace_id AND rc.id=d.runner_up_conversation_id
		WHERE g.state IN ('deciding','review','applied') AND d.workspace_id=g.workspace_id AND d.gathering_id=g.id AND d.decider='ai' AND NOT d.applied
			AND d.action<>'review' AND NOT EXISTS(SELECT 1 FROM intake_decisions p WHERE p.workspace_id=d.workspace_id AND p.gathering_id=d.gathering_id
				AND p.decider='agent' AND p.created_at>d.created_at)
		ORDER BY d.created_at DESC,d.id DESC LIMIT 1) s ON true`

func scanGathering(row pgx.Row) (IntakeGathering, error) {
	var g IntakeGathering
	var placeKind, placeName, suggestionID, suggestionAction, notApplied *string
	var suggestion IntakeSuggestion
	var suggestedAt *time.Time
	err := row.Scan(&g.ID, &g.ChannelID, &g.Scope, &g.ThreadID, &g.LaneKey, &g.ContactID, &g.State, &g.ReviewReason, &g.Rules.Action, &g.Rules.Rule,
		&g.Rules.Basis, &g.Rules.TargetConversationID, &g.ConversationID, &g.DecidedBy, &g.DecidedAt, &g.DecidedByAgentID, &g.IgnoreReason, &g.Fallback,
		&g.OpenedAt, &g.ClosedAt, &g.DecideBy, &g.RequeuedAt, &g.RequeuedByAgentID, &g.ItemCount, &g.Sender.Kind, &g.Sender.Value, &g.Sender.Name,
		&placeKind, &placeName, &g.Preview,
		&suggestionID, &suggestionAction, &suggestion.TargetConversationID, &suggestion.TargetSubject, &suggestion.TargetStatus, &suggestion.RunnerUpConversationID,
		&suggestion.RunnerUpSubject, &suggestion.RunnerUpConfidence, &suggestion.Confidence, &suggestion.Command, &suggestion.Subject, &suggestion.ReplyKind,
		&suggestion.IgnoreReason, &suggestion.Reason, &notApplied, &suggestedAt)
	if placeKind != nil {
		g.Place = &IntakePlace{Kind: *placeKind, Name: placeName}
	}
	if suggestionID != nil {
		suggestion.DecisionID, suggestion.Action, suggestion.CreatedAt = *suggestionID, *suggestionAction, domain.UTC(*suggestedAt)
		if notApplied != nil {
			suggestion.NotAppliedReason = *notApplied
		}
		g.Suggestion = &suggestion
	}
	g.DecidedAt, g.DecideBy, g.RequeuedAt = domain.UTCOptional(g.DecidedAt), domain.UTCOptional(g.DecideBy), domain.UTCOptional(g.RequeuedAt)
	g.OpenedAt, g.ClosedAt = domain.UTC(g.OpenedAt), domain.UTC(g.ClosedAt)
	return g, err
}

// IntakeGatherings reads a page of a list of the queue: what waits for a person (review) or for its
// decider (deciding), oldest first; what was set aside (ignored) or decided, newest first. channelID
// and scope narrow it, and decidedBy (rules, ai, agent) to who decided.
func (s *Store) IntakeGatherings(ctx context.Context, workspaceID, list, channelID, scope, decidedBy, cursor string, limit int) (IntakeGatheringPage, error) {
	page := IntakeGatheringPage{Items: []IntakeGathering{}}
	if channelID != "" && !isID(channelID) {
		return page, nil
	}
	if limit <= 0 || limit > MaxIntakePage {
		limit = MaxIntakePage
	}
	at, atID := time.Time{}, ""
	if cursor != "" {
		var err error
		if at, atID, err = decodeItemCursor(cursor); err != nil {
			return page, err
		}
	}
	var where, order string
	switch list {
	case IntakeListReview, IntakeListDeciding:
		where = `g.state=$2 AND ($5::timestamptz IS NULL OR (g.closed_at,g.id) > ($5,NULLIF($6,'')::uuid))`
		order = `g.closed_at,g.id`
	case IntakeListIgnored, IntakeListDecided:
		states := `('created','attached','applied','ignored')`
		if list == IntakeListIgnored {
			states = `('ignored')`
		}
		where = `g.state IN ` + states + ` AND $2<>'' AND ($5::timestamptz IS NULL OR (g.decided_at,g.id) < ($5,NULLIF($6,'')::uuid))`
		order = `g.decided_at DESC,g.id DESC`
	default:
		return page, fmt.Errorf("intake list %q", list)
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeGatheringPage, error) {
		rows, err := tx.Query(ctx, `SELECT `+gatheringColumns+` `+gatheringFrom+` WHERE g.workspace_id=$1 AND `+where+`
				AND (NULLIF($3,'') IS NULL OR g.channel_id=NULLIF($3,'')::uuid) AND (NULLIF($4,'') IS NULL OR g.scope=$4)
				AND (NULLIF($8,'') IS NULL OR g.decided_by=$8)
			ORDER BY `+order+` LIMIT $7`, workspaceID, list, channelID, scope, nullTime(at), atID, limit+1, decidedBy)
		if err != nil {
			return page, fmt.Errorf("read intake queue: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGathering(rows)
			if err != nil {
				return page, fmt.Errorf("read intake queue: %w", err)
			}
			page.Items = append(page.Items, g)
		}
		if err := rows.Err(); err != nil {
			return page, err
		}
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			last := page.Items[limit-1]
			when := last.ClosedAt
			if list == IntakeListIgnored || list == IntakeListDecided {
				when = *last.DecidedAt
			}
			next := encodeItemCursor(when, last.ID)
			page.Next = &next
		}
		return page, nil
	})
}

// IntakeGatheringByID reads a card with its items, oldest first, its decisions, newest first, and,
// while it is to be decided, the conversations it may join as of now and the channel's commands.
func (s *Store) IntakeGatheringByID(ctx context.Context, workspaceID, id string, now time.Time) (IntakeGatheringDetail, error) {
	if !isID(id) {
		return IntakeGatheringDetail{}, ErrGatheringNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeGatheringDetail, error) {
		var detail IntakeGatheringDetail
		g, err := scanGathering(tx.QueryRow(ctx, `SELECT `+gatheringColumns+` `+gatheringFrom+` WHERE g.workspace_id=$1 AND g.id=$2::uuid`, workspaceID, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return detail, ErrGatheringNotFound
		}
		if err != nil {
			return detail, err
		}
		detail.IntakeGathering = g
		if detail.Items, err = gatheringItems(ctx, tx, workspaceID, id); err != nil {
			return detail, err
		}
		if detail.Decisions, err = gatheringDecisions(ctx, tx, workspaceID, []string{id}); err != nil {
			return detail, err
		}
		if detail.Candidates, err = gatheringCandidates(ctx, tx, workspaceID, g, now); err != nil {
			return detail, err
		}
		detail.Commands, err = s.openCommands(ctx, tx, workspaceID, g.ChannelID)
		return detail, err
	})
}

// candidateLimit is the most conversations a card offers to join.
const candidateLimit = 30

// gatheringCandidates lists the conversations a card still to be decided may join without its reply
// going to someone else (docs/user/channels.md "Intake queue"): in a group, the open or pending
// conversations of the same place active in the last 72 hours; in a one-to-one chat, the same
// customer's on the same channel; then the ones resolved within the scope's reopen window. A closed
// conversation is never one. The rules' target comes first, then the newest.
func gatheringCandidates(ctx context.Context, tx pgx.Tx, workspaceID string, g IntakeGathering, now time.Time) ([]IntakeCandidate, error) {
	candidates := []IntakeCandidate{}
	if g.State != GatheringDeciding && g.State != GatheringReview {
		return candidates, nil
	}
	rows, err := tx.Query(ctx, `WITH card AS (
			SELECT g.workspace_id,g.channel_id,g.thread_id,g.rules_target,t.kind,COALESCE(g.contact_id,t.contact_id) AS contact_id,
				COALESCE((SELECT (l.options->>'reopen_window_days')::int FROM channel_ai_levels l WHERE l.workspace_id=g.workspace_id AND l.channel_id=g.channel_id
					AND l.scope=g.scope AND l.slot='intake'),7) AS reopen_days
			FROM intake_gatherings g LEFT JOIN channel_threads t ON t.workspace_id=g.workspace_id AND t.id=g.thread_id
			WHERE g.workspace_id=$1 AND g.id=$2::uuid),
		found AS (
			SELECT c.id FROM card JOIN channel_thread_items i ON i.workspace_id=card.workspace_id AND i.thread_id=card.thread_id
				JOIN conversation_thread_items l ON l.workspace_id=i.workspace_id AND l.item_id=i.id AND l.unlinked_at IS NULL
				JOIN conversations c ON c.workspace_id=l.workspace_id AND c.id=l.conversation_id
				WHERE card.kind='group' AND ((c.status IN ('open','pending') AND GREATEST(c.updated_at,c.last_public_at)>=$3::timestamptz-interval '72 hours')
					OR (c.status='resolved' AND c.updated_at>=$3::timestamptz-make_interval(days=>card.reopen_days)))
			UNION
			SELECT c.id FROM conversations c, card WHERE c.workspace_id=card.workspace_id AND card.kind IS DISTINCT FROM 'group'
				AND c.contact_id=card.contact_id AND c.channel_id=card.channel_id
				AND (c.status IN ('open','pending') OR (c.status='resolved' AND c.updated_at>=$3::timestamptz-make_interval(days=>card.reopen_days)))
			UNION
			SELECT c.id FROM conversations c, card WHERE c.workspace_id=card.workspace_id AND c.id=card.rules_target AND c.status<>'closed')
		SELECT c.id::text,c.subject,c.status,c.contact_id::text,COALESCE(NULLIF(p.name,''),p.email,''),c.channel_id::text,c.updated_at,c.status='resolved'
		FROM found JOIN conversations c ON c.workspace_id=$1 AND c.id=found.id JOIN contacts p ON p.workspace_id=c.workspace_id AND p.id=c.contact_id, card
		ORDER BY c.id IS DISTINCT FROM card.rules_target, c.status='resolved', GREATEST(c.updated_at,c.last_public_at) DESC, c.id DESC
		LIMIT $4`, workspaceID, g.ID, now, candidateLimit)
	if err != nil {
		return nil, fmt.Errorf("read intake candidates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c IntakeCandidate
		if err := rows.Scan(&c.ConversationID, &c.Subject, &c.Status, &c.ContactID, &c.ContactName, &c.ChannelID, &c.UpdatedAt, &c.Resolved); err != nil {
			return nil, fmt.Errorf("read intake candidates: %w", err)
		}
		c.UpdatedAt = domain.UTC(c.UpdatedAt)
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// openCommands lists, by name, the commands of a channel that open a request; none for a channel
// removed since.
func (s *Store) openCommands(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) ([]string, error) {
	commands := []string{}
	channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
	if errors.Is(err, channels.ErrNotFound) {
		return commands, nil
	}
	if err != nil {
		return nil, err
	}
	for name, command := range channel.Settings.Commands {
		if command.Action == "" || command.Action == channels.ActionOpen {
			commands = append(commands, name)
		}
	}
	slices.Sort(commands)
	return commands, nil
}

// IntakeSummary is what the queue holds at a glance: whether the workspace has intake, how many cards
// wait for a person and for AI, and the channels and scopes cards come from.
type IntakeSummary struct {
	Available bool                 `json:"available"`
	Waiting   int                  `json:"waiting"`
	Deciding  int                  `json:"deciding"`
	Scopes    []IntakeSummaryScope `json:"scopes"`
}

// IntakeSummaryScope is a channel's scope that collects messages for intake.
type IntakeSummaryScope struct {
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	Scope       string `json:"scope"`
}

// IntakeSummaryOf reads the summary of a workspace's queue.
func (s *Store) IntakeSummaryOf(ctx context.Context, workspaceID string) (IntakeSummary, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeSummary, error) {
		summary := IntakeSummary{Scopes: []IntakeSummaryScope{}}
		rows, err := tx.Query(ctx, `SELECT id::text,type,name FROM channels WHERE workspace_id=$1 AND deleted_at IS NULL ORDER BY name,id`, workspaceID)
		if err != nil {
			return summary, fmt.Errorf("read intake channels: %w", err)
		}
		type row struct{ id, kind, name string }
		found, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
			var c row
			return c, r.Scan(&c.id, &c.kind, &c.name)
		})
		if err != nil {
			return summary, fmt.Errorf("read intake channels: %w", err)
		}
		for _, c := range found {
			adapter, err := s.adapterOf(channels.Type(c.kind))
			if err != nil {
				continue // a type this server does not run has no intake here
			}
			for _, scope := range adapter.Definition().IntakeScopes {
				summary.Scopes = append(summary.Scopes, IntakeSummaryScope{ChannelID: c.id, ChannelName: c.name, Scope: scope})
			}
		}
		summary.Available = len(summary.Scopes) > 0
		err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state='review'),count(*) FILTER (WHERE state='deciding') FROM intake_gatherings
			WHERE workspace_id=$1 AND state IN ('review','deciding')`, workspaceID).Scan(&summary.Waiting, &summary.Deciding)
		return summary, err
	})
}

func gatheringItems(ctx context.Context, tx pgx.Tx, workspaceID, id string) ([]ChatItem, error) {
	rows, err := tx.Query(ctx, `SELECT `+chatItemColumns+` FROM channel_thread_items i WHERE i.workspace_id=$1 AND i.gathering_id=$2::uuid
		ORDER BY i.occurred_at,i.id`, workspaceID, id)
	if err != nil {
		return nil, fmt.Errorf("read gathering items: %w", err)
	}
	defer rows.Close()
	items := []ChatItem{}
	for rows.Next() {
		item, err := scanChatItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func gatheringDecisions(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) ([]IntakeDecisionView, error) {
	rows, err := tx.Query(ctx, `SELECT id::text,decider,basis,action,target_conversation_id::text,runner_up_conversation_id::text,runner_up_confidence::float8,
			command,subject,reply_kind,confidence::float8,reason,applied,ignore_reason,not_applied_reason,fallback,corrects_decision_id::text,follows_decision_id::text,error_code,model,agent_id::text,
			token_id IS NOT NULL,created_at
		FROM intake_decisions WHERE workspace_id=$1 AND gathering_id=ANY($2::uuid[]) ORDER BY created_at DESC,id DESC`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read intake decisions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (IntakeDecisionView, error) {
		var d IntakeDecisionView
		err := row.Scan(&d.ID, &d.Decider, &d.Basis, &d.Action, &d.TargetConversationID, &d.RunnerUpConversationID, &d.RunnerUpConfidence, &d.Command,
			&d.Subject, &d.ReplyKind, &d.Confidence, &d.Reason, &d.Applied, &d.IgnoreReason, &d.NotAppliedReason, &d.Fallback, &d.CorrectsDecisionID, &d.FollowsDecisionID, &d.ErrorCode, &d.Model, &d.AgentID,
			&d.ViaToken, &d.CreatedAt)
		d.CreatedAt = domain.UTC(d.CreatedAt)
		return d, err
	})
}

// IntakeStats counts a period of a workspace's intake (one channel, or all).
type IntakeStats struct {
	Days       int            `json:"days"`
	Gatherings int            `json:"gatherings"`
	ByState    map[string]int `json:"by_state"`
	DecidedBy  map[string]int `json:"decided_by"`
	Fallback   int            `json:"fallback"`
	// AICorrected counts AI's decisions a person changed afterwards.
	AIDecisions int `json:"ai_decisions"`
	AICorrected int `json:"ai_corrected"`
}

// IntakeStatsOf counts the gatherings that closed in the last days, and AI's decisions in them.
func (s *Store) IntakeStatsOf(ctx context.Context, workspaceID, channelID string, days int, now time.Time) (IntakeStats, error) {
	stats := IntakeStats{Days: days, ByState: map[string]int{}, DecidedBy: map[string]int{}}
	if channelID != "" && !isID(channelID) {
		return stats, nil
	}
	since := now.Add(-time.Duration(days) * 24 * time.Hour)
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeStats, error) {
		rows, err := tx.Query(ctx, `SELECT state,COALESCE(decided_by,''),fallback,count(*) FROM intake_gatherings
			WHERE workspace_id=$1 AND closed_at>=$2 AND (NULLIF($3,'') IS NULL OR channel_id=NULLIF($3,'')::uuid) GROUP BY 1,2,3`, workspaceID, since, channelID)
		if err != nil {
			return stats, fmt.Errorf("count intake: %w", err)
		}
		for rows.Next() {
			var state, decidedBy string
			var fallback bool
			var n int
			if err := rows.Scan(&state, &decidedBy, &fallback, &n); err != nil {
				rows.Close()
				return stats, err
			}
			stats.Gatherings += n
			stats.ByState[state] += n
			if decidedBy != "" {
				stats.DecidedBy[decidedBy] += n
			}
			if fallback {
				stats.Fallback += n
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return stats, err
		}
		err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE d.decider='ai' AND d.action<>'review'),
				count(*) FILTER (WHERE d.decider='ai' AND EXISTS(SELECT 1 FROM intake_decisions c WHERE c.workspace_id=d.workspace_id AND c.corrects_decision_id=d.id))
			FROM intake_decisions d JOIN intake_gatherings g ON g.workspace_id=d.workspace_id AND g.id=d.gathering_id
			WHERE d.workspace_id=$1 AND g.closed_at>=$2 AND (NULLIF($3,'') IS NULL OR g.channel_id=NULLIF($3,'')::uuid)`, workspaceID, since, channelID).
			Scan(&stats.AIDecisions, &stats.AICorrected)
		return stats, err
	})
}

// ConversationIntake says why each message from a chat or another channel is in a conversation: what
// the rules decided (the journal), and the gatherings it came in, with their decisions.
type ConversationIntake struct {
	Messages   []MessageIntake         `json:"messages"`
	Gatherings []IntakeGatheringDetail `json:"gatherings"`
}

// MessageIntake is what decided one message of a conversation.
type MessageIntake struct {
	MessageID     string  `json:"message_id"`
	DecisionBasis *string `json:"decision_basis"`
	DecisionRule  *string `json:"decision_rule"`
	GatheringID   *string `json:"gathering_id"`
}

// ConversationIntakeOf reads why a conversation's messages are in it; ErrNotFound for another workspace's.
func (s *Store) ConversationIntakeOf(ctx context.Context, workspaceID, conversationID string) (ConversationIntake, error) {
	if !isID(conversationID) {
		return ConversationIntake{}, pgx.ErrNoRows
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ConversationIntake, error) {
		out := ConversationIntake{Messages: []MessageIntake{}, Gatherings: []IntakeGatheringDetail{}}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE workspace_id=$1 AND id=$2::uuid)`, workspaceID, conversationID).Scan(&exists); err != nil {
			return out, err
		}
		if !exists {
			return out, pgx.ErrNoRows
		}
		rows, err := tx.Query(ctx, `SELECT m.id::text,e.decision_basis,e.decision_rule,e.gathering_id::text FROM messages m
			JOIN LATERAL (SELECT decision_basis,decision_rule,gathering_id FROM channel_inbound_events e WHERE e.workspace_id=m.workspace_id AND e.message_id=m.id
				ORDER BY e.received_at LIMIT 1) e ON true
			WHERE m.workspace_id=$1 AND m.conversation_id=$2::uuid AND m.kind='incoming' ORDER BY m.created_at,m.id`, workspaceID, conversationID)
		if err != nil {
			return out, fmt.Errorf("read conversation intake: %w", err)
		}
		out.Messages, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (MessageIntake, error) {
			var m MessageIntake
			return m, row.Scan(&m.MessageID, &m.DecisionBasis, &m.DecisionRule, &m.GatheringID)
		})
		if err != nil {
			return out, err
		}
		// The newest gatherings, at most a page: a chat that went on for months has many.
		gatherings, err := tx.Query(ctx, `SELECT `+gatheringColumns+` `+gatheringFrom+` WHERE g.workspace_id=$1 AND g.id IN (SELECT n.id FROM intake_gatherings n
				WHERE n.workspace_id=$1 AND n.conversation_id=$2::uuid ORDER BY n.closed_at DESC,n.id DESC LIMIT $3) ORDER BY g.closed_at,g.id`,
			workspaceID, conversationID, MaxIntakePage)
		if err != nil {
			return out, fmt.Errorf("read conversation gatherings: %w", err)
		}
		var found []IntakeGathering
		for gatherings.Next() {
			g, err := scanGathering(gatherings)
			if err != nil {
				gatherings.Close()
				return out, err
			}
			found = append(found, g)
		}
		gatherings.Close()
		if err := gatherings.Err(); err != nil {
			return out, err
		}
		for _, g := range found {
			detail := IntakeGatheringDetail{IntakeGathering: g, Candidates: []IntakeCandidate{}, Commands: []string{}}
			if detail.Items, err = gatheringItems(ctx, tx, workspaceID, g.ID); err != nil {
				return out, err
			}
			if detail.Decisions, err = gatheringDecisions(ctx, tx, workspaceID, []string{g.ID}); err != nil {
				return out, err
			}
			out.Gatherings = append(out.Gatherings, detail)
		}
		return out, nil
	})
}
