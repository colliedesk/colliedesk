package results

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The oldest conversation a window reads: the 30-day resolution window starts 37 days before its end.
const lookback = 30*Day + Settle

// customerStarted selects, for conversations c already filtered, the first public message (it must be
// incoming: the conversation was started by a customer) and the first reply at or after it, up to $M.
// Indexed by messages(workspace_id, conversation_id, created_at).
const customerStarted = `
	JOIN LATERAL (SELECT m.kind,m.created_at FROM messages m
		WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind<>'note' AND m.created_at<=$2
		ORDER BY m.created_at,m.id LIMIT 1) f ON f.kind='incoming'
	LEFT JOIN LATERAL (SELECT m.created_at FROM messages m
		WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.kind='outgoing' AND m.notice_kind IS NULL AND m.created_at>=f.created_at AND m.created_at<=$2
		ORDER BY m.created_at LIMIT 1) r ON true`

// LoadLive reads the Colliedesk side measured at m: customer-started conversations not created by an
// import or a problem report, their status history, and the AI drafts decided in the last 30 days.
func LoadLive(ctx context.Context, tx pgx.Tx, workspaceID string, m time.Time) (Input, error) {
	in := Input{Side: Live, Measured: m, End: m}
	if err := tx.QueryRow(ctx, `SELECT status_history_since FROM workspaces WHERE id=$1`, workspaceID).Scan(&in.HistorySince); err != nil {
		return in, fmt.Errorf("read status history start: %w", err)
	}
	// Volume counts weeks from the first conversation in Colliedesk when it came after a window began
	// (index conversations_live_created).
	if err := tx.QueryRow(ctx, `SELECT min(created_at) FROM conversations
		WHERE workspace_id=$1 AND channel NOT IN ('imported','problem_report')`, workspaceID).Scan(&in.First); err != nil {
		return in, fmt.Errorf("read the first conversation: %w", err)
	}
	in.HistorySince, in.First = in.HistorySince.UTC(), utc(in.First)
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.created_at,f.created_at,r.created_at FROM conversations c `+customerStarted+`
		WHERE c.workspace_id=$1 AND c.channel NOT IN ('imported','problem_report') AND c.created_at>$3 AND c.created_at<=$2`,
		workspaceID, m, m.Add(-lookback))
	if err != nil {
		return in, fmt.Errorf("read conversations: %w", err)
	}
	conversations, err := scanConversations(rows, nil)
	if err != nil {
		return in, err
	}
	index := make(map[string]int, len(conversations))
	ids := make([]string, len(conversations))
	for i, c := range conversations {
		index[c.ID], ids[i] = i, c.ID
	}
	changes, err := tx.Query(ctx, `SELECT conversation_id::text,from_status,to_status,changed_at FROM conversation_status_changes
		WHERE workspace_id=$1 AND conversation_id=ANY($2::uuid[]) AND source='colliedesk' AND changed_at<=$3
		ORDER BY conversation_id,changed_at,id`, workspaceID, ids, m)
	if err != nil {
		return in, fmt.Errorf("read status history: %w", err)
	}
	for changes.Next() {
		var id string
		var change StatusChange
		if err := changes.Scan(&id, &change.From, &change.To, &change.At); err != nil {
			changes.Close()
			return in, fmt.Errorf("read status history: %w", err)
		}
		change.At = change.At.UTC()
		conversations[index[id]].Changes = append(conversations[index[id]].Changes, change)
	}
	changes.Close()
	if err := changes.Err(); err != nil {
		return in, fmt.Errorf("read status history: %w", err)
	}
	in.Conversations = conversations
	drafts, err := tx.Query(ctx, `SELECT conversation_id::text,outcome,decided_at FROM ai_drafts
		WHERE workspace_id=$1 AND outcome IN ('sent_unchanged','sent_edited','discarded') AND decided_at>$2 AND decided_at<=$3`,
		workspaceID, m.Add(-30*Day), m)
	if err != nil {
		return in, fmt.Errorf("read AI drafts: %w", err)
	}
	defer drafts.Close()
	for drafts.Next() {
		var d Draft
		if err := drafts.Scan(&d.ConversationID, &d.Outcome, &d.DecidedAt); err != nil {
			return in, fmt.Errorf("read AI drafts: %w", err)
		}
		d.DecidedAt = d.DecidedAt.UTC()
		in.Drafts = append(in.Drafts, d)
	}
	return in, drafts.Err()
}

// LoadBaseline reads the imported side of source measured at the anchor: its customer-started
// conversations (through the import ledger, so only that account), their imported stats and time
// entries. The windows end at the newest customer-started ticket created by the anchor.
func LoadBaseline(ctx context.Context, tx pgx.Tx, workspaceID, source string, anchor time.Time) (Input, error) {
	in := Input{Side: Baseline, Measured: anchor}
	const imported = `FROM import_items i JOIN conversations c ON c.workspace_id=i.workspace_id AND c.id=i.target_id `
	var newest *time.Time
	// The span of the customer-started history: a ticket the team started does not move where the
	// windows end. One message lookup per imported ticket (100,000 take a few hundred milliseconds).
	if err := tx.QueryRow(ctx, `SELECT min(c.created_at),max(c.created_at) `+imported+customerStarted+`
		WHERE i.workspace_id=$1 AND i.source=$3 AND i.entity='ticket' AND c.created_at<=$2`,
		workspaceID, anchor, source).Scan(&in.First, &newest); err != nil {
		return in, fmt.Errorf("read the imported history: %w", err)
	}
	in.First = utc(in.First)
	in.End = anchor
	if newest != nil && newest.Before(anchor) {
		in.End = newest.UTC()
	}
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.created_at,f.created_at,r.created_at,c.origin->'stats',
		(SELECT COALESCE(sum(t.minutes),0) FROM time_entries t WHERE t.workspace_id=c.workspace_id AND t.conversation_id=c.id) `+imported+customerStarted+`
		WHERE i.workspace_id=$1 AND i.source=$3 AND i.entity='ticket' AND c.created_at>$4 AND c.created_at<=$5`,
		workspaceID, anchor, source, in.End.Add(-lookback), in.End)
	if err != nil {
		return in, fmt.Errorf("read imported conversations: %w", err)
	}
	in.Conversations, err = scanConversations(rows, func(c *Conversation, stats []byte, minutes int) error {
		c.HandleMinutes = minutes
		if len(stats) == 0 {
			return nil
		}
		var read struct {
			Status     string     `json:"status"`
			ResolvedAt *time.Time `json:"resolved_at"`
		}
		if err := json.Unmarshal(stats, &read); err != nil {
			return fmt.Errorf("read ticket stats of %s: %w", c.ID, err)
		}
		c.Stats = &Stats{Status: read.Status, ResolvedAt: utc(read.ResolvedAt)}
		return nil
	})
	return in, err
}

func scanConversations(rows pgx.Rows, baseline func(*Conversation, []byte, int) error) ([]Conversation, error) {
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		var stats []byte
		var minutes int
		targets := []any{&c.ID, &c.CreatedAt, &c.FirstIncoming, &c.FirstReply}
		if baseline != nil {
			targets = append(targets, &stats, &minutes)
		}
		if err := rows.Scan(targets...); err != nil {
			return nil, fmt.Errorf("read conversations: %w", err)
		}
		c.CreatedAt, c.FirstIncoming, c.FirstReply = c.CreatedAt.UTC(), c.FirstIncoming.UTC(), utc(c.FirstReply)
		if baseline != nil {
			if err := baseline(&c, stats, minutes); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read conversations: %w", err)
	}
	return out, nil
}

// utc keeps every time the API returns in UTC (the database session may run in any zone).
func utc(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	value := at.UTC()
	return &value
}
