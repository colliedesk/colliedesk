package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func loadEvalTasks(t *testing.T) []evalTask {
	t.Helper()
	data, err := os.ReadFile("testdata/mcp-task-eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var tasks []evalTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		t.Fatalf("testdata/mcp-task-eval.json: %v", err)
	}
	return tasks
}

// evalToken is the token a task works with.
func evalToken(f evalFixture, tier string) string {
	return map[string]string{"read": f.w.read, "draft": f.w.draft, "send": f.w.send, "admin": f.w.adminTier}[tier]
}

// callEvalTool calls a tool the way a model's tool call does: a refused or invalid call is a result
// the model reads (is_error), never a test failure. It returns the text the model sees.
func callEvalTool(session *mcp.ClientSession, tool string, args map[string]any) (evalCall, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	call := evalCall{Tool: tool, Args: args}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		call.IsError = true
		return call, "error: " + err.Error()
	}
	call.IsError = result.IsError
	said := text(result)
	call.Refused = result.IsError && strings.HasPrefix(said, "Colliedesk refused this")
	return call, said
}

// The scorer reads a refusal from the text a model sees. A missing permission must read as a refusal
// and a wrong argument name must not, or a retried refusal passes (or a fixed call fails) unnoticed
// when the error text changes.
func TestEvalTellsARefusalFromAnInvalidCall(t *testing.T) {
	e := newEnvWith(t, withSMTP)
	f := seedEval(t, e, "inbox")
	session := e.connect(f.w.read, "")
	send := map[string]any{"conversation_id": f.conversations["refund"], "seen_reply_number": 0, "body": "Hi"}
	if call, said := callEvalTool(session, "colliedesk_reply_send", send); !call.IsError || !call.Refused {
		t.Fatalf("send without messages:send: error %v refused %v, want both: %s", call.IsError, call.Refused, said)
	}
	if call, said := callEvalTool(session, "colliedesk_inbox_search", map[string]any{"query": "refund"}); !call.IsError || call.Refused {
		t.Fatalf("search with an unknown argument: error %v refused %v, want an error that is not a refusal: %s", call.IsError, call.Refused, said)
	}
}

// Every task names a known token, seed and check, and its conversations exist in the seed. A run
// that does nothing fails every task, so a pass always means the model did the work.
func TestEvalTasksFailWhenNothingIsDone(t *testing.T) {
	tasks := loadEvalTasks(t)
	if len(tasks) < 20 {
		t.Fatalf("%d tasks, want at least 20", len(tasks))
	}
	inboxKeys := map[string]bool{}
	for _, c := range evalInbox {
		inboxKeys[c.key] = true
	}
	seen := map[string]bool{}
	e := newEnvWith(t, withSMTP)
	fixtures := map[string]evalFixture{"inbox": seedEval(t, e, "inbox"), "empty": seedEval(t, e, "empty")}
	for _, task := range tasks {
		if seen[task.ID] || task.ID == "" {
			t.Errorf("task id %q is empty or used twice", task.ID)
		}
		seen[task.ID] = true
		f, ok := fixtures[task.Seed]
		if !ok || evalToken(f, task.Token) == "" || task.MaxCalls <= 0 || task.Prompt == "" || len(task.Expect) == 0 {
			t.Errorf("%s: seed %q, token %q, max_calls %d, %d checks", task.ID, task.Seed, task.Token, task.MaxCalls, len(task.Expect))
			continue
		}
		for _, check := range task.Expect {
			if check.Conversation != "" && !inboxKeys[check.Conversation] {
				t.Errorf("%s: no conversation %q in the seed", task.ID, check.Conversation)
			}
		}
		result := scoreRun(task, readEvalState(t, e, f), nil)
		if strings.Contains(strings.Join(result.Reasons, ";"), "unknown check kind") {
			t.Errorf("%s: %v", task.ID, result.Reasons)
		}
		if result.Pass {
			t.Errorf("%s passes a run that did nothing", task.ID)
		}
	}
}

// Each task can be done with the tools a model gets: the calls a careful person would make through
// MCP, with the token the task names, pass it. A task no call sequence can pass would only measure noise.
func TestEvalTasksPassWithTheCallsAPersonWouldMake(t *testing.T) {
	for _, task := range loadEvalTasks(t) {
		script, ok := evalScripts[task.ID]
		if !ok {
			t.Errorf("%s has no scripted calls", task.ID)
			continue
		}
		t.Run(task.ID, func(t *testing.T) {
			e := newEnvWith(t, withSMTP)
			f := seedEval(t, e, task.Seed)
			session := e.connect(evalToken(f, task.Token), "")
			var calls []evalCall
			for _, step := range script.steps {
				args := fillEvalArgs(t, e, f, step.args)
				call, said := callEvalTool(session, step.tool, args)
				if call.IsError != task.NoRetry {
					t.Fatalf("%s: error %v, want %v for a task that expects a refusal: %s", step.tool, call.IsError, task.NoRetry, said)
				}
				calls = append(calls, call)
			}
			state := readEvalState(t, e, f)
			state.Answer = script.answer
			if result := scoreRun(task, state, calls); !result.Pass || result.Unsafe {
				t.Fatalf("scripted run: pass %v unsafe %v %v (calls %+v)", result.Pass, result.Unsafe, result.Reasons, calls)
			}
		})
	}
}

// fillEvalArgs puts the seeded ids into a scripted call: "@refund" is that conversation's id and
// "#refund" its latest_reply_number, read the way a model reads it.
func fillEvalArgs(t *testing.T, e *env, f evalFixture, args map[string]any) map[string]any {
	t.Helper()
	out := map[string]any{}
	for key, value := range args {
		if s, ok := value.(string); ok && strings.HasPrefix(s, "@") {
			value = f.conversations[s[1:]]
		} else if ok && strings.HasPrefix(s, "#") {
			_, got := e.rest(http.MethodGet, "/api/v1/conversations/"+f.conversations[s[1:]], nil, f.w.admin)
			value = got["latest_reply_number"]
		}
		out[key] = value
	}
	return out
}
