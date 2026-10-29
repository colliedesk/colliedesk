package ai

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrChatLimit means the help center's AI chat reached a limit of its month or of its day (answers
// or spend): nothing was sent, and the end user gets related articles and Talk to a person.
var ErrChatLimit = errors.New("the AI chat reached its limit")

// chatRoom checks, under the workspace's chat lock, that one more chat call fits the chat's limits
// with the calls still running, and returns what the call holds (nil when its cost is not computed).
// Calls that start together each see what the others hold, as the team's allowance does.
func (s *Service) chatRoom(ctx context.Context, tx pgx.Tx, call Call, model string, tokens [2]int64) (*int64, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-chat:"+call.WorkspaceID); err != nil {
		return nil, err
	}
	slots, err := readSlots(ctx, tx, call.WorkspaceID, false)
	if err != nil {
		return nil, err
	}
	row, found := slots[SlotChat]
	limits, err := s.chatLimits(ctx, tx, call.WorkspaceID, row, found)
	if err != nil {
		return nil, err
	}
	var hold *int64
	if s.priced(call, model) {
		value := estimateMicroUSD(s.config.Server.Prices, model, tokens[0], tokens[1])
		hold = &value
	}
	now := s.now().UTC()
	month, day := monthStart(now), time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if limits.answers != nil {
		var answers, today int
		if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE created_at>=$3) FROM ai_chat_turns
			WHERE workspace_id=$1 AND status IN ('answered','no_source') AND created_at>=$2`, call.WorkspaceID, month, day).Scan(&answers, &today); err != nil {
			return nil, err
		}
		if answers >= *limits.answers || today >= *limits.answers/10 {
			return nil, ErrChatLimit
		}
	}
	if limits.microUSD != nil {
		var spent, spentToday int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(c),0),COALESCE(sum(c) FILTER (WHERE started_at>=$3),0) FROM (
				SELECT started_at,CASE WHEN status='started' THEN COALESCE(reserved_micro_usd,0) ELSE COALESCE(cost_micro_usd,0) END AS c
				FROM ai_usage WHERE workspace_id=$1 AND feature='chat' AND started_at>=$2) u`, call.WorkspaceID, month, day).Scan(&spent, &spentToday); err != nil {
			return nil, err
		}
		var held int64
		if hold != nil {
			held = *hold
		}
		// The month never goes past its limit: a call must fit with what it may cost. The day spreads the
		// month out: it counts what was spent, so a question is never refused for the most it might cost
		// (the whole articles at the full price, though most of them are read from the cache).
		if spent >= *limits.microUSD || spent+held > *limits.microUSD || spentToday >= *limits.microUSD/10 {
			return nil, ErrChatLimit
		}
	}
	return hold, nil
}
