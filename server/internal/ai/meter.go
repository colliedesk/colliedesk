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
	// FeatureChat is the help center's AI chat: answers, pre-warms and the admin's previews.
	FeatureChat   = "chat"
	FeatureIntake = "intake"
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

// ErrAllowanceReached means the workspace used its credits on the server's provider: nothing was sent.
// A workspace key is never limited this way.
var ErrAllowanceReached = errors.New("this month's AI credits are used up")

// AllowanceReachedError is ErrAllowanceReached with the day the credits start again, so the refusal can
// say when to come back. errors.Is(err, ErrAllowanceReached) holds for it.
type AllowanceReachedError struct{ ResetsAt time.Time }

func (e *AllowanceReachedError) Error() string        { return ErrAllowanceReached.Error() }
func (e *AllowanceReachedError) Is(target error) bool { return target == ErrAllowanceReached }

// ErrAllowanceHeld means the call would fit once the calls still running finish: a call that fails gives
// its credit back. The task waits a moment instead of failing.
var ErrAllowanceHeld = errors.New("the AI credits left are held by calls still running")

// monthStart is the first instant of the UTC calendar month of at: credits start again then.
func monthStart(at time.Time) time.Time {
	at = at.UTC()
	return time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// credits are a workspace's credits on the server's provider: how many it gets (limit), how many results
// used one (used) and how many of those are calls still running (held), counted from from until resets.
type credits struct {
	limit, used, held int64
	from, resets      time.Time
}

// creditSQL counts results: a task's calls (retries, intake's fallback model) are one result, a call
// without a task is one, and a failed call is none. The help center's chat and provider checks use no
// credit.
const creditSQL = `SELECT count(DISTINCT k), count(DISTINCT k) FILTER (WHERE status='ok') FROM (
		SELECT COALESCE(task_id::text,id::text) AS k, status FROM ai_usage
		WHERE workspace_id=$1 AND billed_to='server' AND feature NOT IN ('chat','check') AND status IN ('ok','started') AND started_at>=$2 %s) u`

// credits reads the workspace's credits now: those of the UTC month, or of the whole trial when the
// server gives a trial its own number. ok is false when the server sets none.
func (s *Service) credits(ctx context.Context, tx pgx.Tx, workspaceID string) (c credits, ok bool, err error) {
	server := s.config.Server
	if server == nil || server.Allowance == nil {
		return credits{}, false, nil
	}
	var access domain.Access
	if s.config.PlanLimits {
		if access, err = store.WorkspaceAccess(ctx, tx, workspaceID, s.now()); err != nil {
			return credits{}, false, err
		}
	}
	month := monthStart(s.now())
	c = credits{limit: server.Allowance.For(access), from: month, resets: month.AddDate(0, 1, 0)}
	if server.Allowance.Trial > 0 && access.Plan == domain.PlanTrial {
		c.from, c.resets = access.TrialEndsAt.Add(-domain.TrialLength), access.TrialEndsAt
	}
	var finished int64
	if err := tx.QueryRow(ctx, fmt.Sprintf(creditSQL, ""), workspaceID, c.from).Scan(&c.used, &finished); err != nil {
		return credits{}, false, err
	}
	c.held = c.used - finished
	return c, true, nil
}

// creditCounted says whether the task already used its credit: its next call (a retry, intake's
// fallback model) uses no other.
func (s *Service) creditCounted(ctx context.Context, tx pgx.Tx, workspaceID, taskID string, from time.Time) (bool, error) {
	if taskID == "" {
		return false, nil
	}
	var results, finished int64
	if err := tx.QueryRow(ctx, fmt.Sprintf(creditSQL, "AND task_id=$3"), workspaceID, from, taskID).Scan(&results, &finished); err != nil {
		return false, err
	}
	return results > 0, nil
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

// MeteredStream is Metered for an answer handed to onText as it is written (aiprovider.Provider.Stream).
// A call that fails or is cut short after the provider read the prompt is recorded with the usage it
// reported, so what it cost counts.
func (s *Service) MeteredStream(ctx context.Context, call Call, provider aiprovider.Provider, request aiprovider.Request, onText func(text string)) (aiprovider.Response, error) {
	input, output := estimateTokens(request)
	return s.metered(ctx, call, request.Model, [2]int64{input, output}, func(ctx context.Context) (aiprovider.Response, error) {
		return provider.Stream(ctx, request, onText)
	})
}

// estimateTokens is the most a request can use: its text at one token per 3 bytes (about one per CJK
// character, more than English uses), and all of its output.
func estimateTokens(request aiprovider.Request) (input, output int64) {
	size := len(request.SystemText())
	images := 0
	for _, message := range request.Messages {
		size += len(message.Text)
		images += len(message.Images)
	}
	if request.Schema != nil {
		size += 500
	}
	return int64(size/3+1) + int64(images)*tokensPerImage, int64(request.MaxOutput)
}

// operatorCheck is the one provider call that is not metered: the operator console's check of the server
// provider of a change not saved yet. It belongs to no workspace, so there is no usage row to write; it is
// one check request (at most 256 tokens out), run only by a person signed in to the console.
func operatorCheck(ctx context.Context, provider aiprovider.Provider, model string) error {
	_, err := provider.Check(ctx, model)
	return err
}

// tokensPerImage is the most an image costs as input: Claude counts a picture of at most 1.15
// megapixels at about 1,600 tokens, and other providers about the same or less.
const tokensPerImage = 1600

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
		if call.Feature == FeatureChat {
			// The chat has its own money: it never spends or waits on the team's allowance.
			hold, err := s.chatRoom(ctx, tx, call, model, tokens)
			if err != nil {
				return err
			}
			reserved = hold
		} else if call.BilledTo == BilledToServer && s.config.Server != nil && call.Feature != FeatureCheck {
			// Checked and held in one step per workspace: a running call holds its credit, so calls that
			// start together each see the others and together never pass the credits.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "ai-allowance:"+call.WorkspaceID); err != nil {
				return err
			}
			c, limited, err := s.credits(ctx, tx, call.WorkspaceID)
			if err != nil {
				return err
			}
			if limited && c.used >= c.limit {
				counted, err := s.creditCounted(ctx, tx, call.WorkspaceID, call.TaskID, c.from)
				if err != nil {
					return err
				}
				if !counted {
					if c.held > 0 && c.used-c.held < c.limit {
						return ErrAllowanceHeld
					}
					return &AllowanceReachedError{ResetsAt: c.resets}
				}
			}
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
		// A failed call keeps the usage the provider reported before it failed (a stream cut short).
		var input, output, cacheRead, cacheWrite *int64
		if response.Usage.Reported {
			input, output, cacheRead, cacheWrite = &response.Usage.InputTokens, &response.Usage.OutputTokens, &response.Usage.CacheReadTokens, &response.Usage.CacheWriteTokens
		}
		var hops []byte
		var cost *int64
		if response.Usage.Reported && s.priced(call, model) {
			value, known := costMicroUSD(s.config.Server.Prices, response.Hops)
			if !known {
				slog.Warn("an AI model without a price in ai.server.prices answered; charged at the highest declared price", "model", response.Model)
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
		if callErr != nil {
			code := aiprovider.CodeOf(callErr)
			var providerErr *aiprovider.Error
			requestID := ""
			if errors.As(callErr, &providerErr) {
				requestID = providerErr.RequestID
			}
			if requestID == "" {
				requestID = response.RequestID
			}
			_, err := tx.Exec(finishCtx, `UPDATE ai_usage SET status='error',error_code=$3,provider_request_id=$4,finished_at=$5,latency_ms=$6,
					served_model=$7,input_tokens=$8,output_tokens=$9,cache_read_tokens=$10,cache_write_tokens=$11,hops=$12,cost_micro_usd=$13
				WHERE workspace_id=$1 AND id=$2`, call.WorkspaceID, usageID, string(code), nullable(requestID), finished, latency,
				nullable(response.Model), input, output, cacheRead, cacheWrite, hops, cost)
			return err
		}
		_, err := tx.Exec(finishCtx, `UPDATE ai_usage SET status='ok',stop_reason=$3,served_model=$4,input_tokens=$5,output_tokens=$6,cache_read_tokens=$7,
				cache_write_tokens=$8,hops=$9,provider_request_id=$10,finished_at=$11,latency_ms=$12,cost_micro_usd=$13,provider_cost=$14
			WHERE workspace_id=$1 AND id=$2`, call.WorkspaceID, usageID, string(response.Stop), nullable(response.Model),
			input, output, cacheRead, cacheWrite, hops, nullable(response.RequestID), finished, latency, cost, response.ProviderCost)
		return err
	})
	if err != nil && callErr == nil {
		return response, fmt.Errorf("record the AI call after it: %w", err)
	}
	return response, callErr
}

// priced says whether a call's cost is computed: every call on the server's provider, and a chat call
// on a workspace's key when the server declares its model's price (its own monthly limit counts it).
func (s *Service) priced(call Call, model string) bool {
	if s.config.Server == nil {
		return false
	}
	if call.BilledTo == BilledToServer {
		return true
	}
	_, known := s.config.Server.Prices[model]
	return call.Feature == FeatureChat && known
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// A typical reply draft: the conversation, a few articles and excerpts in, a short reply out.
const typicalDraftInput, typicalDraftOutput = 6000, 500

func (c credits) view() *AllowanceView {
	return &AllowanceView{LimitCredits: c.limit, UsedCredits: c.used, ResetsAt: c.resets}
}

func (s *Service) autoDraftEstimate(ctx context.Context, tx pgx.Tx, workspaceID string) (AutoDraftEstimate, error) {
	var estimate AutoDraftEstimate
	var average *float64
	since := s.now().Add(-30 * 24 * time.Hour)
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM messages WHERE workspace_id=$1 AND kind='incoming' AND origin IS NULL AND created_at>=$2),
			(SELECT avg(cost_micro_usd)::float8 FROM ai_usage WHERE workspace_id=$1 AND feature='draft' AND billed_to='server' AND cost_micro_usd>0 AND started_at>=$2)`,
		workspaceID, since).Scan(&estimate.CustomerMessages, &average); err != nil {
		return estimate, err
	}
	if average == nil && s.config.Server != nil {
		if models := s.config.Server.ModelsFor(SlotDraft); len(models) > 0 {
			if price, ok := s.config.Server.Prices[models[0]]; ok {
				typical := typicalDraftInput*price.Input + typicalDraftOutput*price.Output
				average = &typical
			}
		}
	}
	if average != nil {
		cost := int64(*average + 0.5)
		estimate.DraftCostMicroUSD = &cost
	}
	return estimate, nil
}

// creditsLeft refuses at once what could never run: no credit left even once the calls still running
// give theirs back. What a person asks for is refused before it is queued.
func (s *Service) creditsLeft(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	c, limited, err := s.credits(ctx, tx, workspaceID)
	if err != nil || !limited {
		return err
	}
	if c.used-c.held >= c.limit {
		return &AllowanceReachedError{ResetsAt: c.resets}
	}
	return nil
}
