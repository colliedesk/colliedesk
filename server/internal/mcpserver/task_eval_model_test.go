package mcpserver_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// evalTurns caps one task's conversation with the model; a model still calling tools by then fails.
const evalTurns = 15

// evalRun is what one model did on one task, written to disk as one JSON line so a failure can be
// read call by call.
type evalRun struct {
	Task       string     `json:"task"`
	Model      string     `json:"model"`
	Tools      string     `json:"tools"`
	Pass       bool       `json:"pass"`
	Unsafe     bool       `json:"unsafe"`
	Reasons    []string   `json:"reasons"`
	Calls      []evalCall `json:"calls"`
	Said       []string   `json:"said"`
	Searches   int        `json:"searches"`
	Turns      int        `json:"turns"`
	Answer     string     `json:"answer"`
	Input      int64      `json:"input_tokens"`
	CacheRead  int64      `json:"cache_read_tokens"`
	CacheWrite int64      `json:"cache_write_tokens"`
	Output     int64      `json:"output_tokens"`
	Error      string     `json:"error,omitempty"`
}

// claudeTools turns the tools/list a client loads into Claude tools. With deferred, every tool is
// loaded on demand through the BM25 tool search, the way Claude Code loads a large MCP server.
func claudeTools(t *testing.T, tools []*mcp.Tool, deferred bool) []anthropic.ToolUnionParam {
	t.Helper()
	var out []anthropic.ToolUnionParam
	for _, tool := range tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		input := anthropic.ToolInputSchemaParam{Properties: schema["properties"], ExtraFields: map[string]any{}}
		input.Required = anyStrings(t, schema["required"])
		for key, value := range schema {
			if key != "properties" && key != "required" && key != "type" {
				input.ExtraFields[key] = value
			}
		}
		param := &anthropic.ToolParam{Name: tool.Name, Description: anthropic.String(tool.Description), InputSchema: input}
		if deferred {
			param.DeferLoading = anthropic.Bool(true)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: param})
	}
	if deferred {
		out = append(out, anthropic.ToolUnionParam{OfToolSearchToolBm25_20251119: &anthropic.ToolSearchToolBm25_20251119Param{
			Type: anthropic.ToolSearchToolBm25_20251119TypeToolSearchToolBm25_20251119}})
	}
	return out
}

// runModel gives the model the task's prompt, the server's instructions as the system prompt and its
// tools, and runs every tool call it makes through MCP until it answers or runs out of turns.
func runModel(client anthropic.Client, model string, session *mcp.ClientSession, system string, tools []anthropic.ToolUnionParam, task evalTask) (evalRun, []evalCall) {
	run := evalRun{Task: task.ID, Model: model}
	var calls []evalCall
	messages := []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(task.Prompt))}
	for run.Turns < evalTurns {
		run.Turns++
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: 8000,
			System:    []anthropic.TextBlockParam{{Text: system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
			Tools:     tools,
			Messages:  messages,
		})
		cancel()
		if err != nil {
			run.Error = err.Error()
			return run, calls
		}
		run.Input += resp.Usage.InputTokens
		run.CacheRead += resp.Usage.CacheReadInputTokens
		run.CacheWrite += resp.Usage.CacheCreationInputTokens
		run.Output += resp.Usage.OutputTokens
		messages = append(messages, resp.ToParam())
		var results []anthropic.ContentBlockParamUnion
		var answer []string
		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				answer = append(answer, v.Text)
			case anthropic.ServerToolUseBlock:
				run.Searches++
			case anthropic.ToolUseBlock:
				var args map[string]any
				if err := json.Unmarshal([]byte(v.JSON.Input.Raw()), &args); err != nil {
					args = map[string]any{}
				}
				call, said := callEvalTool(session, v.Name, args)
				calls = append(calls, call)
				run.Said = append(run.Said, fmt.Sprintf("%s -> %.600s", v.Name, said))
				results = append(results, anthropic.NewToolResultBlock(v.ID, said, call.IsError))
			}
		}
		switch {
		case len(results) > 0:
			messages = append(messages, anthropic.NewUserMessage(results...))
		case resp.StopReason == anthropic.StopReasonPauseTurn:
		default:
			run.Answer = strings.Join(answer, "\n")
			return run, calls
		}
	}
	run.Error = fmt.Sprintf("no answer after %d turns", evalTurns)
	return run, calls
}

// evalPrice is the list price in dollars per million tokens: input, cache read, cache write, output.
var evalPrice = map[string][4]float64{
	"claude-sonnet-5-5": {2, 0.2, 2.5, 10},
	"claude-haiku-5-5":  {0.1, 0.01, 0.125, 0.5},
}

func (r evalRun) cost() float64 {
	p := evalPrice[r.Model]
	return (float64(r.Input)*p[0] + float64(r.CacheRead)*p[1] + float64(r.CacheWrite)*p[2] + float64(r.Output)*p[3]) / 1e6
}
