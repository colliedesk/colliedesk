package ai_test

import (
	"context"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

func streamRequest() aiprovider.Request {
	return aiprovider.Request{Model: "claude-haiku-5-5", Effort: aiprovider.EffortLow, MaxOutput: 800,
		SystemParts: []aiprovider.SystemPart{{Text: "rules"}, {Text: "articles", Cache: true}},
		Messages:    []aiprovider.Message{{Role: aiprovider.RoleUser, Text: "how do I ship?"}}}
}

// A streamed call leaves one usage row like any other: finished with the provider's usage and its
// cost when it ends, and still with the usage reported so far when it fails or is cut short, so a
// chat answer that stopped halfway is not free.
func TestAStreamedCallIsMeteredEvenWhenCutShort(t *testing.T) {
	e := newEnv(t, func(config *ai.Config) { config.Server = testServer })
	call := ai.Call{WorkspaceID: e.a.id, Feature: ai.FeatureDraft, BilledTo: ai.BilledToServer, Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI"}
	usage := aiprovider.Usage{Reported: true, InputTokens: 50, OutputTokens: 20, CacheReadTokens: 40000}
	hops := []aiprovider.Hop{{Model: "claude-haiku-5-5", Usage: usage}}
	hold := make(chan struct{})
	e.fake.Script(
		aiprovider.FakeReply{Deltas: []string{"Ship ", "it."}, Response: aiprovider.Response{Stop: aiprovider.StopDone, Model: "claude-haiku-5-5", Usage: usage, Hops: hops}},
		aiprovider.FakeReply{Deltas: []string{"Half"}, Response: aiprovider.Response{Model: "claude-haiku-5-5", Usage: usage, Hops: hops},
			Err: &aiprovider.Error{Code: aiprovider.ErrorOverloaded}},
		aiprovider.FakeReply{Deltas: []string{"One ", "two"}, Response: aiprovider.Response{Model: "claude-haiku-5-5", Usage: usage, Hops: hops}, Hold: hold})

	var text strings.Builder
	response, err := e.service.MeteredStream(context.Background(), call, e.fake, streamRequest(), func(piece string) { text.WriteString(piece) })
	if err != nil || text.String() != "Ship it." || response.Text != "Ship it." {
		t.Fatalf("stream = %q %+v %v", text.String(), response, err)
	}
	if _, err := e.service.MeteredStream(context.Background(), call, e.fake, streamRequest(), func(string) {}); aiprovider.CodeOf(err) != aiprovider.ErrorOverloaded {
		t.Fatalf("failed stream err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := e.service.MeteredStream(ctx, call, e.fake, streamRequest(), func(string) { cancel() }); err == nil {
		t.Fatal("a canceled stream succeeded")
	}

	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND status='started'`, e.a.id); n != 0 {
		t.Fatalf("%d rows left started", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND status='ok' AND input_tokens=50 AND cache_read_tokens=40000 AND cost_micro_usd>0`, e.a.id); n != 1 {
		t.Fatalf("finished rows with usage = %d, want 1", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND status='error' AND input_tokens=50 AND cost_micro_usd>0 AND finished_at IS NOT NULL`, e.a.id); n != 2 {
		t.Fatalf("failed rows that kept their usage = %d, want 2", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1 AND error_code='overloaded'`, e.a.id); n != 1 {
		t.Fatalf("overloaded rows = %d", n)
	}
}
