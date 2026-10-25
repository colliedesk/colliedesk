package store

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	LaneKey           string      `json:"lane_key"`
	ContactID         *string     `json:"contact_id"`
	State             string      `json:"state"`
	ReviewReason      *string     `json:"review_reason"`
	Rules             IntakeRules `json:"rules"`
	ConversationID    *string     `json:"conversation_id"`
	DecidedBy         *string     `json:"decided_by"`
	DecidedAt         *time.Time  `json:"decided_at"`
	DecidedByAgentID  *string     `json:"decided_by_agent_id"`
	Fallback          bool        `json:"fallback"`
	OpenedAt          time.Time   `json:"opened_at"`
	ClosedAt          time.Time   `json:"closed_at"`
	DecideBy          *time.Time  `json:"decide_by"`
	RequeuedAt        *time.Time  `json:"requeued_at"`
	RequeuedByAgentID *string     `json:"requeued_by_agent_id"`
	ItemCount         int         `json:"item_count"`
	Sender            ChatSender  `json:"sender"`
	// Preview is the beginning of what the customer wrote first.
	Preview string `json:"preview"`
}

// IntakeRules is what the channel's rules decided when the gathering closed.
type IntakeRules struct {
	Action               string  `json:"action"`
	Rule                 string  `json:"rule"`
	TargetConversationID *string `json:"target_conversation_id"`
}

// IntakeDecisionView is one decision of a gathering as the API shows it.
type IntakeDecisionView struct {
	ID                     string    `json:"id"`
	Decider                string    `json:"decider"`
	Basis                  string    `json:"basis"`
	Action                 string    `json:"action"`
	TargetConversationID   *string   `json:"target_conversation_id"`
	RunnerUpConversationID *string   `json:"runner_up_conversation_id"`
	RunnerUpConfidence     *float64  `json:"runner_up_confidence"`
	Command                *string   `json:"command"`
	Subject                *string   `json:"subject"`
	ReplyKind              *string   `json:"reply_kind"`
	Confidence             *float64  `json:"confidence"`
	Reason                 *string   `json:"reason"`
	Applied                bool      `json:"applied"`
	NotAppliedReason       *string   `json:"not_applied_reason"`
	Fallback               bool      `json:"fallback"`
	CorrectsDecisionID     *string   `json:"corrects_decision_id"`
	Model                  *string   `json:"model"`
	AgentID                *string   `json:"agent_id"`
	ViaToken               bool      `json:"via_token"`
	CreatedAt              time.Time `json:"created_at"`
}

// IntakeGatheringDetail is a card with everything its customer said and every decision made on it.
type IntakeGatheringDetail struct {
	IntakeGathering
	Items     []ChatItem           `json:"items"`
	Decisions []IntakeDecisionView `json:"decisions"`
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
	g.rules_target::text,g.conversation_id::text,g.decided_by,g.decided_at,g.decided_by_agent_id::text,g.fallback,g.opened_at,g.closed_at,g.decide_by,
	g.requeued_at,g.requeued_by_agent_id::text,
	(SELECT count(*) FROM channel_thread_items i WHERE i.workspace_id=g.workspace_id AND i.gathering_id=g.id),
	f.sender_kind,f.sender_value,f.sender_name,COALESCE(left(f.body,280),'')`

const gatheringFrom = `FROM intake_gatherings g LEFT JOIN LATERAL (SELECT i.sender_kind,i.sender_value,i.sender_name,i.body FROM channel_thread_items i
	WHERE i.workspace_id=g.workspace_id AND i.gathering_id=g.id ORDER BY i.occurred_at,i.id LIMIT 1) f ON true`

func scanGathering(row pgx.Row) (IntakeGathering, error) {
	var g IntakeGathering
	err := row.Scan(&g.ID, &g.ChannelID, &g.Scope, &g.ThreadID, &g.LaneKey, &g.ContactID, &g.State, &g.ReviewReason, &g.Rules.Action, &g.Rules.Rule,
		&g.Rules.TargetConversationID, &g.ConversationID, &g.DecidedBy, &g.DecidedAt, &g.DecidedByAgentID, &g.Fallback, &g.OpenedAt, &g.ClosedAt, &g.DecideBy,
		&g.RequeuedAt, &g.RequeuedByAgentID, &g.ItemCount, &g.Sender.Kind, &g.Sender.Value, &g.Sender.Name, &g.Preview)
	g.DecidedAt, g.DecideBy, g.RequeuedAt = domain.UTCOptional(g.DecidedAt), domain.UTCOptional(g.DecideBy), domain.UTCOptional(g.RequeuedAt)
	g.OpenedAt, g.ClosedAt = domain.UTC(g.OpenedAt), domain.UTC(g.ClosedAt)
	return g, err
}

// IntakeGatherings reads a page of a list of the queue: what waits for a person (review) or for its
// decider (deciding), oldest first; what was set aside (ignored) or decided, newest first. channelID
// and scope narrow it.
func (s *Store) IntakeGatherings(ctx context.Context, workspaceID, list, channelID, scope, cursor string, limit int) (IntakeGatheringPage, error) {
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
			ORDER BY `+order+` LIMIT $7`, workspaceID, list, channelID, scope, nullTime(at), atID, limit+1)
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

// IntakeGatheringByID reads a card with its items, oldest first, and its decisions, newest first.
func (s *Store) IntakeGatheringByID(ctx context.Context, workspaceID, id string) (IntakeGatheringDetail, error) {
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
		detail.Decisions, err = gatheringDecisions(ctx, tx, workspaceID, []string{id})
		return detail, err
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
			command,subject,reply_kind,confidence::float8,reason,applied,not_applied_reason,fallback,corrects_decision_id::text,model,agent_id::text,token_id IS NOT NULL,created_at
		FROM intake_decisions WHERE workspace_id=$1 AND gathering_id=ANY($2::uuid[]) ORDER BY created_at DESC,id DESC`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read intake decisions: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (IntakeDecisionView, error) {
		var d IntakeDecisionView
		err := row.Scan(&d.ID, &d.Decider, &d.Basis, &d.Action, &d.TargetConversationID, &d.RunnerUpConversationID, &d.RunnerUpConfidence, &d.Command,
			&d.Subject, &d.ReplyKind, &d.Confidence, &d.Reason, &d.Applied, &d.NotAppliedReason, &d.Fallback, &d.CorrectsDecisionID, &d.Model, &d.AgentID,
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
		err = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE d.decider='ai'),
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
			detail := IntakeGatheringDetail{IntakeGathering: g}
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
