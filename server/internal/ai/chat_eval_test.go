package ai_test

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
)

type chatEval struct {
	Articles []struct {
		Number int    `json:"number"`
		Folder string `json:"folder"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	} `json:"articles"`
	Questions []struct {
		Lang   string  `json:"lang"`
		Q      string  `json:"q"`
		Expect []int64 `json:"expect"`
	} `json:"questions"`
}

var citation = regexp.MustCompile(`\[#(\d+)\]`)

// TestChatEval asks Claude the 20 questions of testdata/chat-eval.json with the real prompt and scores
// whether each answer cites an expected article (or none, for a question the articles do not answer).
// It calls the Claude API and costs a few cents, so it runs only by hand: make eval-chat with
// ANTHROPIC_API_KEY set. Results go to docs/internals/ai-chat.md.
func TestChatEval(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if os.Getenv("AI_CHAT_EVAL") != "1" || key == "" {
		t.Skip("set AI_CHAT_EVAL=1 and ANTHROPIC_API_KEY to run the chat eval (make eval-chat)")
	}
	data, err := os.ReadFile("testdata/chat-eval.json")
	if err != nil {
		t.Fatal(err)
	}
	var eval chatEval
	if err := json.Unmarshal(data, &eval); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, nil)
	kb := newKB(t, e, e.a)
	ctx := context.Background()
	for _, a := range eval.Articles {
		if _, err := e.pool.Exec(ctx, `UPDATE kb_collections SET name=$2 WHERE id=$1`, kb.collection, a.Folder); err != nil {
			t.Fatal(err)
		}
		kb.article(t, e.a, a.Number, a.Title, a.Body, "published", false, 1)
	}
	provider, err := aiprovider.NewAnthropic(aiprovider.AnthropicConfig{APIKey: key})
	if err != nil {
		t.Fatal(err)
	}
	model := os.Getenv("AI_CHAT_EVAL_MODEL")
	if model == "" {
		model = "claude-haiku-5-5"
	}
	binding := ai.Binding{Provider: provider, Model: model, Effort: aiprovider.EffortLow}
	call := ai.Call{WorkspaceID: e.a.id, Feature: ai.FeatureChat, BilledTo: ai.BilledToWorkspaceKey, Kind: aiprovider.KindAnthropic, Label: "eval"}
	passed := map[string][2]int{}
	var cacheRead int64
	for i, question := range eval.Questions {
		prompt, err := e.service.ChatPrompt(ctx, binding, e.a.id, ai.ChatPromptInput{WorkspaceName: "ParcelPilot", Question: question.Q})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		var first time.Duration
		response, err := e.service.MeteredStream(ctx, call, provider, prompt.Request, func(string) {
			if first == 0 {
				first = time.Since(started)
			}
		})
		if err != nil {
			t.Fatalf("question %d: %v", i+1, err)
		}
		cacheRead += response.Usage.CacheReadTokens
		var cited []int64
		for _, match := range citation.FindAllStringSubmatch(response.Text, -1) {
			n, _ := strconv.ParseInt(match[1], 10, 64)
			if _, ok := prompt.Articles[n]; ok && !slices.Contains(cited, n) {
				cited = append(cited, n)
			}
		}
		ok := len(question.Expect) == 0 && len(cited) == 0
		for _, n := range question.Expect {
			ok = ok || slices.Contains(cited, n)
		}
		score := passed[question.Lang]
		score[1]++
		if ok {
			score[0]++
		}
		passed[question.Lang] = score
		t.Logf("%2d %-10s ok=%-5v cited=%v want=%v first=%v in=%d cache_read=%d out=%d | %s", i+1, question.Lang, ok, cited, question.Expect,
			first.Round(10*time.Millisecond), response.Usage.InputTokens, response.Usage.CacheReadTokens, response.Usage.OutputTokens,
			strings.ReplaceAll(response.Text, "\n", " "))
	}
	for lang, score := range passed {
		t.Logf("%s: %d of %d", lang, score[0], score[1])
	}
	if cacheRead == 0 {
		t.Error("no question read the cached articles back")
	}
}
