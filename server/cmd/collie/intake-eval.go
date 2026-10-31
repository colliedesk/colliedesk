package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// intakeAI builds the AI service an evaluation calls through, from the server's settings (the command
// loads them first); tests replace it.
var intakeAI = func(db *database.DB) (*ai.Service, error) {
	server, err := ai.ServerProviderFromEnv(settings)
	if err != nil {
		return nil, err
	}
	googleConfig, err := google.ConfigFromEnv(settings)
	if err != nil {
		return nil, err
	}
	allow := strings.TrimSpace(settings("AI_ALLOW_PRIVATE_BASE_URLS"))
	service := ai.NewService(db, ai.Config{Server: server, Keys: googleConfig.Keys, AllowPrivateBaseURLs: allow == "1" || allow == "true"})
	service.SetClock(clock)
	return service, nil
}

// intakeEval runs the AI decider of a chat channel's scope over a labelled dataset with the workspace's
// intake provider (each call metered, asked by a person), prints the counts, and with --record keeps them
// for the scope's readiness.
func intakeEval(ctx context.Context, db *database.DB, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("intake-eval", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspaceID := flags.String("workspace", "", "workspace id")
	channelID := flags.String("channel", "", "chat channel id")
	scope := flags.String("scope", "group", "the channel's intake scope")
	dataset := flags.String("dataset", "", "labelled cases, one JSON a line")
	model := flags.String("model", "", "another model of intake's provider (default: intake's model)")
	thresholds := flags.String("thresholds", "", "create,attach,ignore confidence to count as applied (default: the scope's)")
	record := flags.Bool("record", false, "keep the counts for the scope's readiness")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *workspaceID == "" || *channelID == "" || *dataset == "" {
		return fmt.Errorf("usage: collie intake-eval --workspace <id> --channel <id> [--scope group] --dataset cases.jsonl [--model m] [--thresholds 0.85,0.90,0.95] [--record]")
	}
	file, err := os.Open(*dataset)
	if err != nil {
		return fmt.Errorf("open dataset: %w", err)
	}
	cases, sha, err := intake.ReadEvalDataset(file)
	file.Close()
	if err != nil {
		return fmt.Errorf("read dataset: %w", err)
	}
	service, err := intakeAI(db)
	if err != nil {
		return err
	}
	binding, err := service.Resolve(ctx, *workspaceID, ai.SlotIntake)
	if errors.Is(err, ai.ErrFeatureOff) {
		return fmt.Errorf("intake is off for workspace %s: turn it on with a model in Settings → Built-in AI", *workspaceID)
	}
	if err != nil {
		return err
	}
	if *model != "" {
		binding.Model, binding.FallbackModel = *model, ""
	}
	options, err := service.IntakeOptionsOf(ctx, *workspaceID, *channelID, *scope)
	if err != nil {
		return err
	}
	limits := map[string]float64{"create": *options.CreateThreshold, "attach": *options.AttachThreshold, "ignore": *options.IgnoreThreshold}
	if *thresholds != "" {
		parts := strings.Split(*thresholds, ",")
		if len(parts) != 3 {
			return fmt.Errorf("--thresholds is create,attach,ignore")
		}
		for i, action := range []string{"create", "attach", "ignore"} {
			value, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
			if err != nil || value < 0 || value > 1 {
				return fmt.Errorf("--thresholds are numbers from 0 to 1")
			}
			limits[action] = value
		}
	}
	// Before any call: how many, on what. Each one is metered as intake's, asked by a person.
	fmt.Fprintf(stderr, "Evaluating %d cases on %s (up to %d calls, one more for an answer naming a conversation it was not offered), dataset %s\n",
		len(cases), binding.Model, 2*len(cases), sha[:12])
	ask := func(ctx context.Context, request aiprovider.Request) (aiprovider.Response, error) {
		return service.Ask(ctx, binding, ai.SlotIntake, request)
	}
	result, err := intake.Evaluate(ctx, cases, sha, binding.Model, intake.EvalOptions{Options: intake.OptionsSnapshot(options), Thresholds: limits}, ask)
	if err != nil {
		return err
	}
	if *record {
		if _, err := store.NewWithDB(db).RecordIntakeEvaluation(ctx, *workspaceID, *channelID, store.IntakeEvaluationInput{Scope: *scope, Model: binding.Model,
			PromptVersion: result.PromptVersion, DatasetSHA256: sha, Counts: result.Counts}, store.ChannelActor{}, clock()); err != nil {
			return fmt.Errorf("record the evaluation: %w", err)
		}
		fmt.Fprintln(stderr, "Recorded: the scope's readiness counts it for this model and prompt version.")
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
