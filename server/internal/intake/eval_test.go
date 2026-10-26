package intake

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// scriptedAnswers answers each request, in order, with the next decision; "fail" fails the call.
func scriptedAnswers(t *testing.T, answers ...string) (Asker, *[]aiprovider.Request) {
	t.Helper()
	var asked []aiprovider.Request
	return func(_ context.Context, request aiprovider.Request) (aiprovider.Response, error) {
		asked = append(asked, request)
		if len(answers) == 0 {
			t.Fatal("more requests than answers")
		}
		next := answers[0]
		answers = answers[1:]
		if next == "fail" {
			return aiprovider.Response{}, &aiprovider.Error{Code: aiprovider.ErrorOverloaded}
		}
		return aiprovider.Response{Text: next, JSON: next, Stop: aiprovider.StopDone}, nil
	}, &asked
}

func answer(action, target, subject string, confidence float64) string {
	raw, _ := json.Marshal(map[string]any{"action": action, "target": target, "runner_up": "", "runner_up_confidence": 0, "command": "", "subject": subject,
		"reply_kind": "substantive", "ignore_reason": map[bool]string{true: "chat", false: ""}[action == "ignore"], "confidence": confidence, "reason": "x"})
	return string(raw)
}

// The run reads the dataset with the decider's own prompt and reader, counts per action what it got
// right, its severe and light errors, leaves out a case whose label it could never reach, and says how
// many it would have applied at the thresholds and how many of those were wrong.
func TestEvaluateCountsWhatTheDeciderGetsRight(t *testing.T) {
	file, err := os.Open("testdata/eval-sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cases, sha, err := ReadEvalDataset(file)
	if err != nil || len(cases) != 6 || len(sha) != 64 {
		t.Fatalf("dataset = %d cases, %q, %v", len(cases), sha, err)
	}
	ask, asked := scriptedAnswers(t,
		answer("create", "", "Stock export fails", 0.95), // right
		answer("attach", "C1", "", 0.97),                 // right
		answer("attach", "C1", "", 0.92),                 // a new request merged into the resolved one: severe
		answer("ignore", "", "", 0.99),                   // right
		answer("ignore", "", "", 0.60),                   // a request set aside: severe, below the threshold
	)
	result, err := Evaluate(context.Background(), cases, sha, "claude-haiku-5-5", EvalOptions{Thresholds: map[string]float64{"create": 0.85, "attach": 0.90, "ignore": 0.95}}, ask)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cases != 5 || result.Unreachable != 1 || result.Failed != 0 || result.PromptVersion != prompt.Version {
		t.Fatalf("result = %+v", result)
	}
	want := map[string]store.IntakeCounts{"create": {Labels: 1, Correct: 1}, "attach": {Labels: 2, Correct: 1, Severe: 1}, "ignore": {Labels: 2, Correct: 1, Severe: 1}}
	for action, counts := range want {
		if result.Counts[action] != counts {
			t.Errorf("%s = %+v, want %+v", action, result.Counts[action], counts)
		}
	}
	if result.Applied["attach"] != 2 || result.AppliedWrong["attach"] != 1 || result.Applied["ignore"] != 1 || result.AppliedWrong["ignore"] != 0 {
		t.Errorf("applied = %v, wrong %v", result.Applied, result.AppliedWrong)
	}
	if result.Confusion["create"]["attach"] != 1 || result.Confusion["create"]["ignore"] != 1 {
		t.Errorf("confusion = %v", result.Confusion)
	}
	// The model reads people as labels and conversations as C1, never the dataset's references.
	if len(*asked) != 5 || strings.Contains((*asked)[1].Messages[0].Text, "Lan") || strings.Contains((*asked)[1].Messages[0].Text, "170") {
		t.Fatalf("the prompt names people or references:\n%s", (*asked)[1].Messages[0].Text)
	}
}

// A failed call is a wrong answer, never dropped; a stopped run stops.
func TestEvaluateCountsAFailedCallAndStopsWhenAsked(t *testing.T) {
	cases := []EvalCase{{Items: []EvalItem{{Role: "customer", Sender: "a", Text: "x"}}, Label: EvalLabel{Action: "create"}}}
	ask, _ := scriptedAnswers(t, "fail")
	result, err := Evaluate(context.Background(), cases, "", "m", EvalOptions{}, ask)
	if err != nil || result.Failed != 1 || result.Cases != 1 || result.Confusion["create"]["error"] != 1 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := func(ctx context.Context, _ aiprovider.Request) (aiprovider.Response, error) {
		return aiprovider.Response{}, ctx.Err()
	}
	if _, err := Evaluate(ctx, cases, "", "m", EvalOptions{}, stopped); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped run = %v", err)
	}
	if _, _, err := ReadEvalDataset(strings.NewReader(`{"items":[],"label":{"action":"create"}}`)); err == nil {
		t.Fatal("a case without items was read")
	}
}
