package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// Features a usage row is counted under.
const (
	FeatureCheck  = "check"
	FeatureTriage = "triage"
	FeatureDraft  = "draft"
)

// Who pays for a call.
const (
	BilledToServer       = "server"
	BilledToWorkspaceKey = "workspace_key"
)

// finishTimeout bounds the write that closes a usage row. It runs on a context that the caller
// cannot cancel: a person who closes the tab during a call must not leave the row started.
const finishTimeout = 10 * time.Second

// Call says what a provider call is for and who pays; it becomes the usage row.
type Call struct {
	WorkspaceID string
	Feature     string
	BilledTo    string
	// ProviderID is the workspace provider's id; empty for the server's provider.
	ProviderID string
	Kind       aiprovider.Kind
	Label      string
	Actor      Actor
	// TaskID names the task the call works for; Automatic marks a call no person asked for.
	TaskID    string
	Automatic bool
}

// ErrAllowanceReached means the server's provider spent this month's allowance of the workspace:
// nothing was sent. A workspace key is never limited this way.
var ErrAllowanceReached = errors.New("this month's included AI is used up")

// ErrAllowanceHeld means the call would fit once the calls still running finish and give back what
// they hold: the task waits a moment instead of failing.
var ErrAllowanceHeld = errors.New("the included AI left is held by calls still running")

// monthStart is the first instant of the UTC calendar month of at: allowances reset then.
func monthStart(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// allowanceLeft is the month's allowance minus what the server's provider spent for the workspace and
// what the calls still running hold (their most possible cost), in micro-dollars, and the allowance;
// ok is false when the server sets none.
func (s *Service) allowanceLeft(ctx context.Context, tx pgx.Tx, workspaceID string) (left, limit int64, ok bool, err error) {
	server := s.config.Server
	if server == nil || server.Allowance == nil {
		return 0, 0, false, nil
	}
	var access domain.Access
	if s.config.PlanLimits {
		if access, err = store.WorkspaceAccess(ctx, tx, workspaceID, s.now()); err != nil {
			return 0, 0, false, err
		}
	}
	var agents int
	var used int64
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM agents WHERE workspace_id=$1 AND deactivated_at IS NULL),
			(SELECT COALESCE(sum(CASE WHEN status='started' THEN COALESCE(reserved_micro_usd,0) ELSE COALESCE(cost_micro_usd,0) END),0)
				FROM ai_usage WHERE workspace_id=$1 AND billed_to='server' AND started_at>=$2)`,
		workspaceID, monthStart(s.now())).Scan(&agents, &used); err != nil {
		return 0, 0, false, err
	}
	limit = server.Allowance.For(access, agents)
	return limit - used, limit, true, nil
}

type usageHop struct {
	Model      string `json:"model"`
	Input      int64  `json:"input"`
	Output     int64  `json:"output"`
	CacheRead  int64  `json:"cache_read"`
	CacheWrite int64  `json:"cache_write"`
}

// Metered sends one request to a provider and records it: a started row committed before the call,
// finished after it with the provider's usage, or the error code. It is the only way a provider is
// called (TestProvidersAreCalledOnlyThroughMetered), so every attempt leaves exactly one row.
func (s *Service) Metered(ctx context.Context, call Call, provider aiprovider.Provider, request aiprovider.Request) (aiprovider.Response, error) {
	return s.metered(ctx, call, request.Model, func() [2]int64 { i, o := estimateTokens(request); return [2]int64{i, o} }(), func(ctx context.Context) (aiprovider.Response, error) {
		return provider.Complete(ctx, request)
	})
}

// estimateTokens is the most a request can use: its text at one token per 3 bytes (about one per CJK
// character, more than English uses), and all of its output.
func estimateTokens(request aiprovider.Request) (input, output int64) {
	size := len(request.System)
	for _, message := range request.Messages {
		size += len(message.Text)
	}
	if request.Schema != nil {
		size += 500
	}
	return int64(size/3 + 1), int64(request.MaxOutput)
}

// meteredCheck is Metered for a provider check.
func (s *Service) meteredCheck(ctx context.Context, call Call, provider aiprovider.Provider, model string) (aiprovider.Response, error) {
	call.Feature = FeatureCheck
	// A check is one small request: a line in, at most 256 tokens out.
	return s.metered(ctx, call, model, [2]int64{200, 256}, func(ctx context.Context) (aiprovider.Response, error) {
		return provider.Check(ctx, model)
	})
}

func (s *Service) metered(ctx context.Context, call Call, model string, tokens [2]int64, send func(context.Context) (aiprovider.Response, error)) (aiprovider.Response, error) {
	started := s.now().UTC()
	var usageID string
	err := s.db.InWorkspace(ctx, call.WorkspaceID, func(tx pgx.Tx) error {
		var reserved *int64
		if call.BilledTo == BilledToServer && s.config.Server != nil {
			// Checked and held in one step per workspace: calls that start together each see what the
			// others hold, so together they never pass the allowance by more than their estimates miss.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-allowance:"+call.WorkspaceID); err != nil {
				return err
			}
			hold := estimateMicroUSD(s.config.Server.Prices, model, tokens[0], tokens[1])
			left, _, limited, err := s.allowanceLeft(ctx, tx, call.WorkspaceID)
			if err != nil {
				return err
			}
			if limited && (left <= 0 || hold > left) {
				var held int64
				if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(reserved_micro_usd),0) FROM ai_usage WHERE workspace_id=$1 AND billed_to='server' AND status='started'`,
					call.WorkspaceID).Scan(&held); err != nil {
					return err
				}
				if held > 0 && hold <= left+held {
					return ErrAllowanceHeld
				}
				return ErrAllowanceReached
			}
			reserved = &hold
		}
		return tx.QueryRow(ctx, `INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_id,provider_kind,provider_label,model,status,actor_agent_id,actor_token_id,
				task_id,automatic,started_at,reserved_micro_usd)
			VALUES($1,$2,$3,$4,$5,$6,$7,'started',$8,$9,$10,$11,$12,$13) RETURNING id::text`,
			call.WorkspaceID, call.Feature, call.BilledTo, nullable(call.ProviderID), string(call.Kind), call.Label, model,
			nullable(call.Actor.AgentID), nullable(call.Actor.TokenID), nullable(call.TaskID), call.Automatic, started, reserved).Scan(&usageID)
	})
	if errors.Is(err, ErrAllowanceReached) || errors.Is(err, ErrAllowanceHeld) {
		return aiprovider.Response{}, err
	}
	if err != nil {
		return aiprovider.Response{}, fmt.Errorf("record the AI call before sending it: %w", err)
	}

	response, callErr := send(ctx)

	finished := s.now().UTC()
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	latency := finished.Sub(started).Milliseconds()
	err = s.db.InWorkspace(finishCtx, call.WorkspaceID, func(tx pgx.Tx) error {
		if callErr != nil {
			code := aiprovider.CodeOf(callErr)
			var providerErr *aiprovider.Error
			requestID := ""
			if errors.As(callErr, &providerErr) {
				requestID = providerErr.RequestID
			}
			_, err := tx.Exec(finishCtx, `UPDATE ai_usage SET status='error',error_code=$3,provider_request_id=$4,finished_at=$5,latency_ms=$6
				WHERE workspace_id=$1 AND id=$2`, call.WorkspaceID, usageID, string(code), nullable(requestID), finished, latency)
			return err
		}
		var input, output, cacheRead, cacheWrite *int64
		if response.Usage.Reported {
			input, output, cacheRead, cacheWrite = &response.Usage.InputTokens, &response.Usage.OutputTokens, &response.Usage.CacheReadTokens, &response.Usage.CacheWriteTokens
		}
		var hops []byte
		var cost *int64
		if response.Usage.Reported && call.BilledTo == BilledToServer && s.config.Server != nil {
			value, known := costMicroUSD(s.config.Server.Prices, response.Hops)
			if !known {
				slog.Warn("an AI model without a price in AI_SERVER_PRICES answered; charged at the highest declared price", "model", response.Model)
			}
			cost = &value
		}
		if response.Usage.Reported {
			list := make([]usageHop, 0, len(response.Hops))
			for _, hop := range response.Hops {
				list = append(list, usageHop{Model: hop.Model, Input: hop.Usage.InputTokens, Output: hop.Usage.OutputTokens, CacheRead: hop.Usage.CacheReadTokens, CacheWrite: hop.Usage.CacheWriteTokens})
			}
			hops, _ = json.Marshal(list)
		}
		_, err := tx.Exec(finishCtx, `UPDATE ai_usage SET status='ok',stop_reason=$3,served_model=$4,input_tokens=$5,output_tokens=$6,cache_read_tokens=$7,
				cache_write_tokens=$8,hops=$9,provider_request_id=$10,finished_at=$11,latency_ms=$12,cost_micro_usd=$13
			WHERE workspace_id=$1 AND id=$2`, call.WorkspaceID, usageID, string(response.Stop), nullable(response.Model),
			input, output, cacheRead, cacheWrite, hops, nullable(response.RequestID), finished, latency, cost)
		return err
	})
	if err != nil && callErr == nil {
		return response, fmt.Errorf("record the AI call after it: %w", err)
	}
	return response, callErr
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// A typical reply draft: the conversation, a few articles and excerpts in, a short reply out.
const typicalDraftInput, typicalDraftOutput = 6000, 500

func (s *Service) allowanceView(ctx context.Context, tx pgx.Tx, workspaceID string, left, limit int64, resets time.Time) (*AllowanceView, error) {
	view := &AllowanceView{LimitMicroUSD: limit, UsedMicroUSD: limit - left, ResetsAt: resets}
	var average *float64
	if err := tx.QueryRow(ctx, `SELECT avg(cost_micro_usd)::float8 FROM ai_usage WHERE workspace_id=$1 AND feature='draft' AND billed_to='server'
		AND cost_micro_usd>0 AND started_at>=$2`, workspaceID, s.now().Add(-30*24*time.Hour)).Scan(&average); err != nil {
		return nil, err
	}
	if average == nil && s.config.Server != nil {
		if models := s.config.Server.ModelsFor(SlotDraft); len(models) > 0 {
			if price, ok := s.config.Server.Prices[models[0]]; ok {
				typical := typicalDraftInput*price.Input + typicalDraftOutput*price.Output
				average = &typical
			}
		}
	}
	if average != nil && *average > 0 {
		drafts := int64(float64(max(left, 0)) / *average)
		view.DraftsLeft = &drafts
	}
	return view, nil
}

// typicalCallFits refuses a call of that size that could not fit what is left of the allowance even
// with nothing else running: what a person asks for is refused at once, not after it was queued.
func (s *Service) typicalCallFits(ctx context.Context, tx pgx.Tx, workspaceID, model string, input, output int64) error {
	if s.config.Server == nil {
		return nil
	}
	left, _, limited, err := s.allowanceLeft(ctx, tx, workspaceID)
	if err != nil || !limited {
		return err
	}
	var held int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(reserved_micro_usd),0) FROM ai_usage WHERE workspace_id=$1 AND billed_to='server' AND status='started'`,
		workspaceID).Scan(&held); err != nil {
		return err
	}
	if left+held <= 0 || estimateMicroUSD(s.config.Server.Prices, model, input, output) > left+held {
		return ErrAllowanceReached
	}
	return nil
}
