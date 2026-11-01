package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

// An operator scores a chat scope on a labelled dataset with the workspace's intake provider: every
// call is metered as intake's and asked by a person, the counts come out as JSON, and --record keeps them
// for the model and prompt version it ran.
func TestIntakeEvalScoresADatasetAndRecordsIt(t *testing.T) {
	databaseURL := databasetest.URL(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(ctx, "Eval Desk", "admin@eval.example.com")
	if err != nil {
		t.Fatal(err)
	}
	var channel string
	if _, err := pool.Exec(ctx, `INSERT INTO channel_types(type,connectable) VALUES('evalchat',true) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO channels(workspace_id,type,name) VALUES($1,'evalchat','Store chat') RETURNING id::text`, workspace.ID).Scan(&channel); err != nil {
		t.Fatal(err)
	}
	fake := &aiprovider.Fake{}
	keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
	service := ai.NewService(database.New(pool), ai.Config{Keys: keys, Build: func(ai.Spec) (aiprovider.Provider, error) { return fake, nil }})
	original := intakeAI
	intakeAI = func(*database.DB) (*ai.Service, error) { return service, nil }
	t.Cleanup(func() { intakeAI = original })
	var agent string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&agent); err != nil {
		t.Fatal(err)
	}
	actor := ai.Actor{AgentID: agent}
	provider, err := service.AddProvider(ctx, workspace.ID, actor, ai.ProviderInput{Kind: string(aiprovider.KindAnthropic), Label: "Team key", APIKey: "sk-eval-0001", Models: []string{"claude-haiku-5-5"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetSlot(ctx, workspace.ID, actor, ai.SlotIntake, ai.SlotInput{Source: ai.SourceProvider, ProviderID: provider.ID, Model: "claude-haiku-5-5", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// The answers come after the provider was added and checked.
	for _, raw := range []string{
		`{"action":"create","target":"","runner_up":"","runner_up_confidence":0,"command":"","subject":"Stock export fails","reply_kind":"substantive","ignore_reason":"","confidence":0.95,"reason":"new"}`,
		`{"action":"attach","target":"C1","runner_up":"","runner_up_confidence":0,"command":"","subject":"","reply_kind":"substantive","ignore_reason":"","confidence":0.97,"reason":"same"}`,
	} {
		fake.Script(aiprovider.FakeReply{Response: aiprovider.Response{Text: raw, JSON: raw, Stop: aiprovider.StopDone, Model: "claude-haiku-5-5",
			Usage: aiprovider.Usage{Reported: true, InputTokens: 900, OutputTokens: 40}}})
	}
	dataset := filepath.Join(t.TempDir(), "cases.jsonl")
	if err := os.WriteFile(dataset, []byte(`{"place":"group","items":[{"role":"customer","sender":"Lan","text":"the stock export fails"}],"label":{"action":"create"}}
{"place":"group","items":[{"role":"customer","sender":"Lan","text":"still failing"}],"candidates":[{"ref":"170","reporter":"Lan","subject":"Stock export","status":"open"}],"label":{"action":"attach","target_ref":"170"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, hint bytes.Buffer
	stderr = &hint
	t.Cleanup(func() { stderr = os.Stderr })
	err = run(ctx, []string{"intake-eval", "--workspace", workspace.ID, "--channel", channel, "--scope", "group", "--dataset", dataset, "--record"}, &out, databaseURL, "https://desk.example.com")
	if err != nil {
		t.Fatalf("intake-eval: %v (%s)", err, hint.String())
	}
	var result struct {
		Cases  int `json:"cases"`
		Counts map[string]struct {
			Labels, Correct int
		} `json:"counts"`
		PromptVersion string `json:"prompt_version"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Cases != 2 || result.Counts["create"].Correct != 1 || result.Counts["attach"].Correct != 1 ||
		result.PromptVersion != prompt.Version {
		t.Fatalf("result = %s, %v", out.String(), err)
	}
	if !strings.Contains(hint.String(), "Evaluating 2 cases on claude-haiku-5-5") {
		t.Fatalf("hint = %s", hint.String())
	}
	var usage, automatic int
	if err := pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE automatic) FROM ai_usage WHERE workspace_id=$1 AND feature='intake'`, workspace.ID).Scan(&usage, &automatic); err != nil {
		t.Fatal(err)
	}
	if usage != 2 || automatic != 0 {
		t.Fatalf("usage rows = %d, automatic %d", usage, automatic)
	}
	var recorded string
	if err := pool.QueryRow(ctx, `SELECT model||' '||prompt_version||' '||(counts->'attach'->>'correct') FROM intake_eval_runs WHERE workspace_id=$1`, workspace.ID).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != "claude-haiku-5-5 "+prompt.Version+" 1" {
		t.Fatalf("recorded = %s", recorded)
	}
}
