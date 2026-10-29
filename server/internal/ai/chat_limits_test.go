package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

func chatServer() *ai.ServerProvider {
	return &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-key",
		Models: map[string][]string{ai.SlotDraft: {"claude-haiku-5-5"}, ai.SlotChat: {"claude-haiku-5-5"}},
		Prices: map[string]ai.Price{"claude-haiku-5-5": {Input: 0.1, Output: 0.5}}, Allowance: &ai.Allowance{PerWorkspace: 0.01}}
}

// The chat has its own money: with the team's included AI used up it still answers, and what it
// spends never moves the team's allowance.
func TestTheChatNeverSpendsTheTeamsAllowance(t *testing.T) {
	e := newEnv(t, func(config *ai.Config) { config.Server = chatServer() })
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_kind,provider_label,model,status,started_at,finished_at,cost_micro_usd)
		VALUES($1,'draft','server','anthropic','Colliedesk AI','claude-haiku-5-5','ok',now(),now(),20000)`, e.a.id); err != nil {
		t.Fatal(err)
	}
	before, err := e.service.Settings(ctx, e.a.id)
	if err != nil || before.Allowance == nil || before.Allowance.UsedMicroUSD != 20000 {
		t.Fatalf("allowance = %+v %v", before.Allowance, err)
	}
	call := ai.Call{WorkspaceID: e.a.id, Feature: ai.FeatureChat, BilledTo: ai.BilledToServer, Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI"}
	usage := aiprovider.Usage{Reported: true, InputTokens: 30000, OutputTokens: 200}
	e.fake.Script(aiprovider.FakeReply{Deltas: []string{"Yes."}, Response: aiprovider.Response{Stop: aiprovider.StopDone, Model: "claude-haiku-5-5", Usage: usage,
		Hops: []aiprovider.Hop{{Model: "claude-haiku-5-5", Usage: usage}}}})
	if _, err := e.service.MeteredStream(ctx, call, e.fake, streamRequest(), func(string) {}); err != nil {
		t.Fatalf("a chat with the included AI used up: %v", err)
	}
	after, err := e.service.Settings(ctx, e.a.id)
	if err != nil || after.Allowance.UsedMicroUSD != before.Allowance.UsedMicroUSD {
		t.Fatalf("the chat moved the allowance: %d → %d", before.Allowance.UsedMicroUSD, after.Allowance.UsedMicroUSD)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND feature='chat' AND cost_micro_usd>0`, e.a.id); n != 1 {
		t.Fatalf("priced chat rows = %d", n)
	}
}

// On a workspace's own key the chat's monthly limit counts what it spends at the server's prices: past
// it, or past a tenth of it in a day, nothing is sent.
func TestTheChatStopsAtItsOwnMonthlyLimit(t *testing.T) {
	e := newEnv(t, func(config *ai.Config) { config.Server = chatServer() })
	ctx := context.Background()
	provider, err := e.service.AddProvider(ctx, e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: "Team key", APIKey: testKey, Models: []string{"claude-haiku-5-5"}})
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(100000)
	if _, err := e.service.SetSlot(ctx, e.a.id, e.actor(e.a), ai.SlotChat, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID, Enabled: true,
		MonthlyCap: &limit, MonthlyCapSet: true}); err != nil {
		t.Fatal(err)
	}
	requests := len(e.fake.Requests())
	call := ai.Call{WorkspaceID: e.a.id, Feature: ai.FeatureChat, BilledTo: ai.BilledToWorkspaceKey, ProviderID: provider.ID, Kind: aiprovider.KindAnthropic, Label: "Team key"}
	// A tenth of the month in a day: 10,000 micro-dollars. One small question fits.
	if _, err := e.service.MeteredStream(ctx, call, e.fake, streamRequest(), func(string) {}); err != nil {
		t.Fatalf("first question: %v", err)
	}
	// Close to the day's limit, a question whose most possible cost would pass it is still answered:
	// the day counts what was spent.
	if _, err := e.pool.Exec(ctx, `INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_kind,provider_label,model,status,started_at,finished_at,cost_micro_usd)
		VALUES($1,'chat','workspace_key','anthropic','Team key','claude-haiku-5-5','ok',now(),now(),9900)`, e.a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.MeteredStream(ctx, call, e.fake, streamRequest(), func(string) {}); err != nil {
		t.Fatalf("a question near the day's limit: %v", err)
	}
	requests++
	if _, err := e.pool.Exec(ctx, `INSERT INTO ai_usage(workspace_id,feature,billed_to,provider_kind,provider_label,model,status,started_at,finished_at,cost_micro_usd)
		VALUES($1,'chat','workspace_key','anthropic','Team key','claude-haiku-5-5','ok',now(),now(),100)`, e.a.id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.MeteredStream(ctx, call, e.fake, streamRequest(), func(string) {}); !errors.Is(err, ai.ErrChatLimit) {
		t.Fatalf("over the day's limit: %v", err)
	}
	if got := len(e.fake.Requests()); got != requests+1 {
		t.Fatalf("the provider got %d requests, want %d", got, requests+1)
	}
}
