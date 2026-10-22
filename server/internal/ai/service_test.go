package ai_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

func TestAddProviderSavesOnlyAfterACheckAndSealsTheKey(t *testing.T) {
	e := newEnv(t, nil)
	e.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}})
	_, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: "Team key", APIKey: testKey, Models: []string{"claude-sonnet-5-5"}})
	var failed *ai.CheckError
	if !errors.As(err, &failed) || failed.Code != aiprovider.ErrorAuth {
		t.Fatalf("refused key: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_providers`); n != 0 {
		t.Fatalf("a provider whose check failed was saved (%d rows)", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE feature='check' AND status='error' AND error_code='auth' AND billed_to='workspace_key'`); n != 1 {
		t.Fatalf("failed check usage rows = %d, want 1", n)
	}

	provider := e.add(t, e.a, "Team key")
	if provider.KeyHint == nil || *provider.KeyHint != "9f3a" || provider.CheckStatus != "ok" || provider.AuthKind != "api_key" {
		t.Fatalf("provider = %+v", provider)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_providers WHERE position($1::bytea in sealed_key) > 0`, []byte(testKey)); n != 0 {
		t.Fatal("the key is stored in clear")
	}
	if n := e.count(t, `SELECT count(*) FROM ai_settings_events WHERE kind='provider_added' AND detail::text LIKE '%9f3a%' AND detail::text NOT LIKE '%'||$1||'%'`, testKey); n != 1 {
		t.Fatalf("provider_added events = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE feature='check' AND status='ok' AND provider_id=$1 AND input_tokens=10 AND hops IS NOT NULL`, provider.ID); n != 1 {
		t.Fatalf("check usage rows for the saved provider = %d, want 1", n)
	}
	if spec := e.specs[len(e.specs)-1]; spec.APIKey != testKey || spec.Server || spec.Kind != aiprovider.KindAnthropic {
		t.Fatalf("adapter built from %+v", spec)
	}
	if got := e.fake.Requests(); got[len(got)-1].Model != "claude-sonnet-5-5" {
		t.Fatalf("check used model %q, want the first", got[len(got)-1].Model)
	}
}

func TestACallCancelledMidwayStillFinishesItsUsageRow(t *testing.T) {
	e := newEnv(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	e.fake.Answer = func(aiprovider.Request) (aiprovider.Response, error) {
		cancel()
		<-ctx.Done()
		return aiprovider.Response{}, &aiprovider.Error{Code: aiprovider.ErrorTimeout}
	}
	_, err := e.service.AddProvider(ctx, e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: "Team key", APIKey: testKey, Models: []string{"m"}})
	if err == nil {
		t.Fatal("cancelled check passed")
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE status='started'`); n != 0 {
		t.Fatalf("%d usage rows left started after a cancelled call", n)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE status='error' AND error_code='timeout' AND finished_at IS NOT NULL`); n != 1 {
		t.Fatalf("finished error rows = %d, want 1", n)
	}
}

func TestProviderLimitHoldsUnderConcurrentAdds(t *testing.T) {
	e := newEnv(t, nil)
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic",
				Label: "Key " + string(rune('A'+i)), APIKey: testKey, Models: []string{"m"}})
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	refused := 0
	for err := range errs {
		switch {
		case errors.Is(err, ai.ErrLimit):
			refused++
		case err != nil:
			t.Fatal(err)
		}
	}
	if n := e.count(t, `SELECT count(*) FROM ai_providers WHERE workspace_id=$1`, e.a.id); n != ai.MaxProviders || refused != 8-ai.MaxProviders {
		t.Fatalf("providers = %d, refused = %d", n, refused)
	}
}

func TestLabelsAreUniquePerWorkspace(t *testing.T) {
	e := newEnv(t, nil)
	e.add(t, e.a, "Team key")
	if _, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: " Team key ", APIKey: testKey, Models: []string{"m"}}); !errors.Is(err, ai.ErrLabelTaken) {
		t.Fatalf("duplicate label: %v", err)
	}
	e.add(t, e.b, "Team key")
}

func TestNoKeyRingRefusesBeforeSpendingACheck(t *testing.T) {
	e := newEnv(t, func(config *ai.Config) { config.Keys = tokenseal.Keys{} })
	_, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: "Team key", APIKey: testKey, Models: []string{"m"}})
	if !errors.Is(err, ai.ErrKeysMissing) || len(e.fake.Requests()) != 0 {
		t.Fatalf("no key ring: %v, %d calls", err, len(e.fake.Requests()))
	}
}

func TestBaseURLRules(t *testing.T) {
	e := newEnv(t, nil)
	add := func(url, key string) error {
		_, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "openai_compatible", Label: "P " + url, BaseURL: url, APIKey: key, Models: []string{"m"}})
		return err
	}
	var refused *ai.InvalidError
	if err := add("http://api.example.com/v1", testKey); !errors.As(err, &refused) || !strings.Contains(err.Error(), "https") {
		t.Fatalf("public http: %v", err)
	}
	for _, blocked := range []string{"http://10.0.0.12:11434/v1", "https://10.0.0.12:11434/v1", "https://localhost:11434/v1", "https://[::1]/v1", "https://169.254.169.254/v1"} {
		var failed *ai.CheckError
		if err := add(blocked, testKey); !errors.As(err, &failed) || failed.Code != aiprovider.ErrorBlockedAddress {
			t.Fatalf("%s: %v", blocked, err)
		}
	}
	if err := add("https://openrouter.ai/api/v1", ""); !errors.As(err, &refused) {
		t.Fatalf("public URL without a key: %v", err)
	}
	if _, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "anthropic", Label: "A", BaseURL: "https://proxy.example/v1", APIKey: testKey, Models: []string{"m"}}); !errors.As(err, &refused) {
		t.Fatalf("Anthropic with a base URL: %v", err)
	}
	if len(e.fake.Requests()) != 0 {
		t.Fatalf("%d checks were spent on refused URLs", len(e.fake.Requests()))
	}
	if err := add("https://openrouter.ai/api/v1/", testKey); err != nil {
		t.Fatal(err)
	}
	if spec := e.specs[len(e.specs)-1]; spec.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("base URL = %q", spec.BaseURL)
	}

	private := newEnv(t, func(config *ai.Config) { config.AllowPrivateBaseURLs = true })
	provider, err := private.service.AddProvider(context.Background(), private.a.id, private.actor(private.a), ai.ProviderInput{Kind: "openai_compatible", Label: "Ollama", BaseURL: "http://localhost:11434/v1", Models: []string{"llama"}})
	if err != nil || provider.AuthKind != "none" || provider.KeyHint != nil {
		t.Fatalf("operator-allowed private URL without key: %+v %v", provider, err)
	}
}

func TestSetKeyKeepsTheOldKeyWhenTheNewOneFails(t *testing.T) {
	e := newEnv(t, nil)
	provider := e.add(t, e.a, "Team key")
	e.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorAuth, Status: 401}})
	if _, err := e.service.SetKey(context.Background(), e.a.id, e.actor(e.a), provider.ID, "sk-new-bad-0000"); err == nil {
		t.Fatal("bad key accepted")
	}
	if n := e.count(t, `SELECT count(*) FROM ai_providers WHERE key_hint='9f3a'`); n != 1 {
		t.Fatal("the old key was replaced by one that failed its check")
	}
	updated, err := e.service.SetKey(context.Background(), e.a.id, e.actor(e.a), provider.ID, "sk-new-good-7Qx2")
	if err != nil || *updated.KeyHint != "7Qx2" {
		t.Fatalf("good key: %+v %v", updated, err)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_settings_events WHERE kind='provider_key_changed' AND detail->>'from_hint'='9f3a' AND detail->>'to_hint'='7Qx2'`); n != 1 {
		t.Fatal("key change not in the history")
	}
	// The new key opens: a check sends it.
	if _, err := e.service.CheckProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID); err != nil {
		t.Fatal(err)
	}
	if spec := e.specs[len(e.specs)-1]; spec.APIKey != "sk-new-good-7Qx2" {
		t.Fatalf("check sent key %q", spec.APIKey)
	}
}

func TestCheckRecordsAFailingProvider(t *testing.T) {
	e := newEnv(t, nil)
	provider := e.add(t, e.a, "Team key")
	e.fake.Script(aiprovider.FakeReply{Err: &aiprovider.Error{Code: aiprovider.ErrorQuotaExhausted, Status: 402}})
	checked, err := e.service.CheckProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID)
	if err != nil || checked.CheckStatus != "failing" || checked.CheckErrorCode == nil || *checked.CheckErrorCode != "quota_exhausted" {
		t.Fatalf("failing check: %+v %v", checked, err)
	}
}

func TestChecksAreRateLimitedPerWorkspace(t *testing.T) {
	e := newEnv(t, nil)
	provider := e.add(t, e.a, "Team key")
	var err error
	for range 12 {
		if _, err = e.service.CheckProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID); err != nil {
			break
		}
	}
	if !errors.Is(err, ai.ErrCheckRateLimited) {
		t.Fatalf("12 checks a minute: %v", err)
	}
	other := e.add(t, e.b, "Other key")
	if _, err := e.service.CheckProvider(context.Background(), e.b.id, e.actor(e.b), other.ID); err != nil {
		t.Fatalf("another workspace is limited too: %v", err)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE workspace_id=$1`, e.a.id); n != 10 {
		t.Fatalf("usage rows = %d, want 10 (refused checks never reach the provider)", n)
	}
}

func TestSlotsStartOffAndRecordConsent(t *testing.T) {
	server := &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Colliedesk AI", APIKey: "server-key",
		Models: map[string][]string{ai.SlotTriage: {"claude-haiku-5-5"}, ai.SlotDraft: {"claude-sonnet-5-5", "claude-opus-5-5"}}}
	e := newEnv(t, func(config *ai.Config) { config.Server = server })
	settings, err := e.service.Settings(context.Background(), e.a.id)
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range settings.Slots {
		if slot.Enabled || slot.Active || slot.Source != ai.SourceNone {
			t.Fatalf("slot %s does not start off: %+v", slot.Slot, slot)
		}
	}
	if settings.Server == nil || settings.Server.Label != "Colliedesk AI" {
		t.Fatalf("server = %+v", settings.Server)
	}

	slot, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotDraft, ai.SlotInput{Source: ai.SourceServer, Enabled: true})
	if err != nil || !slot.Active || *slot.Model != "claude-sonnet-5-5" || slot.ConsentedBy == nil || *slot.ConsentedBy != e.a.agentID {
		t.Fatalf("server draft slot: %+v %v", slot, err)
	}
	firstConsent := *slot.ConsentedAt

	// Moving the feature to the workspace's own key is a new recipient: a new consent.
	provider := e.add(t, e.a, "Team key")
	moved, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotDraft, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID, Model: "claude-haiku-5-5", Enabled: true})
	if err != nil || *moved.ProviderID != provider.ID || moved.Source != ai.SourceProvider || moved.ConsentedAt.Before(firstConsent) {
		t.Fatalf("moved slot: %+v %v", moved, err)
	}
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotDraft, ai.SlotInput{Source: ai.SourceServer, Model: "gpt-unknown", Enabled: true}); err == nil {
		t.Fatal("a model the server does not offer was accepted")
	}
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotTriage, ai.SlotInput{Source: ai.SourceNone, Enabled: true}); err == nil {
		t.Fatal("a slot without provider was turned on")
	}
	if n := e.count(t, `SELECT count(*) FROM ai_settings_events WHERE kind='slot_changed' AND slot='draft'`); n != 2 {
		t.Fatalf("slot events = %d, want 2", n)
	}
	// Saving the same setting again is not a change.
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotDraft, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID, Model: "claude-haiku-5-5", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_settings_events WHERE kind='slot_changed' AND slot='draft'`); n != 2 {
		t.Fatalf("an unchanged save added an event (%d)", n)
	}
	settings, _ = e.service.Settings(context.Background(), e.a.id)
	if len(settings.Providers) != 1 || strings.Join(settings.Providers[0].UsedBy, ",") != "draft" || len(settings.Events) != 3 {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestASlotOnTheServerProviderStopsWhenTheOperatorRemovesIt(t *testing.T) {
	server := &ai.ServerProvider{Kind: aiprovider.KindAnthropic, Label: "Server", APIKey: "k", Models: map[string][]string{ai.SlotTriage: {"claude-haiku-5-5"}}}
	e := newEnv(t, func(config *ai.Config) { config.Server = server })
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotTriage, ai.SlotInput{Source: ai.SourceServer, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotDraft, ai.SlotInput{Source: ai.SourceServer, Enabled: true}); !errors.Is(err, ai.ErrNoServerProvider) {
		t.Fatalf("a slot the server offers no model for: %v", err)
	}
	restarted := ai.NewService(database.New(e.pool), ai.Config{Keys: ring(t)})
	settings, err := restarted.Settings(context.Background(), e.a.id)
	if err != nil {
		t.Fatal(err)
	}
	triage := settings.Slots[0]
	if !triage.Enabled || triage.Active || triage.OffReason == nil || *triage.OffReason != ai.OffServerProviderMissing {
		t.Fatalf("triage after the server provider was removed: %+v", triage)
	}
}

func TestRemovingAProviderTurnsItsFeaturesOffAndKeepsHistory(t *testing.T) {
	e := newEnv(t, nil)
	provider := e.add(t, e.a, "Team key")
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotTriage, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	off, err := e.service.RemoveProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID)
	if err != nil || strings.Join(off, ",") != "triage" {
		t.Fatalf("remove: %v %v", off, err)
	}
	settings, _ := e.service.Settings(context.Background(), e.a.id)
	if settings.Slots[0].Enabled || settings.Slots[0].Source != ai.SourceNone || len(settings.Providers) != 0 {
		t.Fatalf("after remove: %+v", settings)
	}
	if settings.Events[0].Kind != "provider_removed" || *settings.Events[0].ProviderLabel != "Team key" || settings.Events[1].Kind != "slot_changed" {
		t.Fatalf("events = %+v", settings.Events)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_usage WHERE provider_id=$1`, provider.ID); n != 1 {
		t.Fatal("usage history went with the provider")
	}
}

func TestUpdateProviderChecksANewURLAndKeepsUsedModels(t *testing.T) {
	e := newEnv(t, nil)
	provider, err := e.service.AddProvider(context.Background(), e.a.id, e.actor(e.a), ai.ProviderInput{Kind: "openai_compatible", Label: "Router", BaseURL: "https://openrouter.ai/api/v1", APIKey: testKey, Models: []string{"a", "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.SetSlot(context.Background(), e.a.id, e.actor(e.a), ai.SlotDraft, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID, Model: "b"}); err != nil {
		t.Fatal(err)
	}
	models := []string{"a"}
	var inUse *ai.InUseError
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{Models: &models}); !errors.As(err, &inUse) {
		t.Fatalf("removing a used model: %v", err)
	}
	calls := len(e.fake.Requests())
	label := "Renamed"
	if _, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{Label: &label}); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Requests()) != calls {
		t.Fatal("a rename spent a check")
	}
	url := "https://api.openai.com/v1"
	updated, err := e.service.UpdateProvider(context.Background(), e.a.id, e.actor(e.a), provider.ID, ai.ProviderChange{BaseURL: &url})
	if err != nil || *updated.BaseURL != url || len(e.fake.Requests()) != calls+1 || strings.Join(updated.UsedBy, ",") != "draft" {
		t.Fatalf("new URL: %+v %v", updated, err)
	}
	if spec := e.specs[len(e.specs)-1]; spec.BaseURL != url || spec.APIKey != testKey {
		t.Fatalf("checked %+v", spec)
	}
}

func TestAnotherWorkspacesProviderIsNotFound(t *testing.T) {
	e := newEnv(t, nil)
	theirs := e.add(t, e.b, "Their key")
	label := "mine now"
	ctx, a, actor := context.Background(), e.a.id, e.actor(e.a)
	checks := map[string]error{}
	_, checks["update"] = e.service.UpdateProvider(ctx, a, actor, theirs.ID, ai.ProviderChange{Label: &label})
	_, checks["key"] = e.service.SetKey(ctx, a, actor, theirs.ID, "sk-x-1234")
	_, checks["check"] = e.service.CheckProvider(ctx, a, actor, theirs.ID)
	_, checks["remove"] = e.service.RemoveProvider(ctx, a, actor, theirs.ID)
	for name, err := range checks {
		if !errors.Is(err, ai.ErrNotFound) {
			t.Fatalf("%s on another workspace's provider: %v", name, err)
		}
	}
	var refused *ai.InvalidError
	if _, err := e.service.SetSlot(ctx, a, actor, ai.SlotDraft, ai.SlotInput{Source: ai.SourceProvider, ProviderID: theirs.ID, Enabled: true}); !errors.As(err, &refused) {
		t.Fatalf("slot on another workspace's provider: %v", err)
	}
	settings, _ := e.service.Settings(ctx, a)
	if len(settings.Providers) != 0 || len(settings.Events) != 0 {
		t.Fatalf("workspace A sees B's settings: %+v", settings)
	}
	if n := e.count(t, `SELECT count(*) FROM ai_providers WHERE label='Their key'`); n != 1 {
		t.Fatal("B's provider changed")
	}
}
