package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// TestMCPTaskEval gives Claude the 20 tasks of testdata/mcp-task-eval.json with the real tools/list and
// server instructions, runs its tool calls against a seeded workspace and scores the workspace after.
// It calls the Claude API and costs money, so it runs only by hand: make eval-mcp with
// ANTHROPIC_API_KEY set. Results go to docs/internals/mcp-task-eval.md.
//
//	MCP_TASK_EVAL_MODEL  claude-sonnet-5-5 (default) or claude-haiku-5-5
//	MCP_TASK_EVAL_TOOLS  deferred (default: tool search loads tools) or loaded (every tool up front)
//	MCP_TASK_EVAL_RUNS   how many times each task runs (default 1)
//	MCP_TASK_EVAL_ONLY   comma-separated task ids to run (default all)
//	MCP_TASK_EVAL_OUT    file for one JSON line per run (default in the temp directory)
func TestMCPTaskEval(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if os.Getenv("MCP_TASK_EVAL") != "1" || key == "" {
		t.Skip("set MCP_TASK_EVAL=1 and ANTHROPIC_API_KEY to run the MCP task eval (make eval-mcp)")
	}
	model := envOr("MCP_TASK_EVAL_MODEL", "claude-sonnet-5-5")
	mode := envOr("MCP_TASK_EVAL_TOOLS", "deferred")
	runs, err := strconv.Atoi(envOr("MCP_TASK_EVAL_RUNS", "1"))
	if err != nil || runs < 1 || (mode != "deferred" && mode != "loaded") {
		t.Fatalf("MCP_TASK_EVAL_RUNS %q, MCP_TASK_EVAL_TOOLS %q", os.Getenv("MCP_TASK_EVAL_RUNS"), mode)
	}
	only := strings.FieldsFunc(os.Getenv("MCP_TASK_EVAL_ONLY"), func(r rune) bool { return r == ',' })
	out := envOr("MCP_TASK_EVAL_OUT", filepath.Join(os.TempDir(), "mcp-task-eval-"+time.Now().UTC().Format("20060102-150405")+".jsonl"))
	file, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	client := anthropic.NewClient(option.WithAPIKey(key), option.WithBaseURL("https://api.anthropic.com"))

	var all []evalRun
	for round := 1; round <= runs; round++ {
		for _, task := range loadEvalTasks(t) {
			if len(only) > 0 && !slices.Contains(only, task.ID) {
				continue
			}
			e := newEnvWith(t, withSMTP)
			f := seedEval(t, e, task.Seed)
			session := e.connect(evalToken(f, task.Token), "")
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			list, err := session.ListTools(ctx, nil)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			tools := claudeTools(t, list.Tools, mode == "deferred")
			run, calls := runModel(client, model, session, session.InitializeResult().Instructions, tools, task)
			run.Tools = mode
			state := readEvalState(t, e, f)
			state.Answer = run.Answer
			result := scoreRun(task, state, calls)
			run.Pass, run.Unsafe, run.Reasons, run.Calls = result.Pass && run.Error == "", result.Unsafe, result.Reasons, calls
			all = append(all, run)
			line, _ := json.Marshal(run)
			fmt.Fprintln(file, string(line))
			t.Logf("%-24s pass %-5v unsafe %-5v calls %2d searches %d $%.4f %v %s", task.ID, run.Pass, run.Unsafe, len(calls), run.Searches, run.cost(), run.Reasons, run.Error)
		}
	}
	t.Log(evalSummary(all, model, mode, out))
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// evalSummary is the line docs/internals/mcp-task-eval.md records for a run of the eval.
func evalSummary(runs []evalRun, model, mode, out string) string {
	var passed, unsafe, calls, errors, searches int
	var cost float64
	for _, r := range runs {
		cost += r.cost()
		searches += r.Searches
		if r.Unsafe {
			unsafe++
		}
		for _, c := range r.Calls {
			if c.IsError {
				errors++
			}
		}
		if r.Pass {
			passed++
			calls += len(r.Calls)
		}
	}
	perPass := 0.0
	if passed > 0 {
		perPass = float64(calls) / float64(passed)
	}
	return fmt.Sprintf("%s %s %s: %d/%d passed, %.1f calls per pass, %d error calls, %d unsafe, %d searches, $%.2f; runs in %s",
		time.Now().UTC().Format("2006-01-02"), model, mode, passed, len(runs), perPass, errors, unsafe, searches, cost, out)
}
