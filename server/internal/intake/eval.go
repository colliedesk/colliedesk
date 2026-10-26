package intake

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Offline evaluation (`collie intake-eval`, docs/self-host/intake-eval.md): the AI decider's own prompt
// and answer reader run over a labelled dataset, one card a line, and the counts per action are what a
// scope's readiness reads once recorded.

// EvalCase is one labelled card of a dataset (JSONL, one per line, already anonymised).
type EvalCase struct {
	Place      string          `json:"place"`
	Items      []EvalItem      `json:"items"`
	Before     []EvalItem      `json:"before"`
	Candidates []EvalCandidate `json:"candidates"`
	Commands   []string        `json:"commands"`
	Label      EvalLabel       `json:"label"`
}

// EvalItem is a message of a case: a customer's or the team's.
type EvalItem struct {
	Role   string    `json:"role"`
	Sender string    `json:"sender"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	Quote  *struct {
		Sender string    `json:"sender"`
		Role   string    `json:"role"`
		Text   string    `json:"text"`
		At     time.Time `json:"at"`
		Ref    string    `json:"ref"`
	} `json:"quote"`
	Ref string `json:"ref"`
}

// EvalCandidate is a conversation the case may join, by a reference of the dataset.
type EvalCandidate struct {
	Ref       string    `json:"ref"`
	Reporter  string    `json:"reporter"`
	Subject   string    `json:"subject"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	Last      string    `json:"last_message"`
}

// EvalLabel is what a person decided: create, attach (to target_ref) or ignore.
type EvalLabel struct {
	Action    string `json:"action"`
	TargetRef string `json:"target_ref"`
}

// EvalOptions are what the run decides with: the channel's options and where a decision applies on its own.
type EvalOptions struct {
	Options    prompt.Snapshot
	Thresholds map[string]float64
}

// EvalResult is what a run counts: per action the labelled cases and how they were decided, the
// confusion of labels with answers, how many it would have applied at the thresholds and how many of
// those were wrong, and the calls' latency.
type EvalResult struct {
	DatasetSHA256 string                        `json:"dataset_sha256"`
	PromptVersion string                        `json:"prompt_version"`
	Model         string                        `json:"model"`
	Cases         int                           `json:"cases"`
	Unreachable   int                           `json:"unreachable"`
	Failed        int                           `json:"failed"`
	Counts        map[string]store.IntakeCounts `json:"counts"`
	Confusion     map[string]map[string]int     `json:"confusion"`
	Applied       map[string]int                `json:"applied"`
	AppliedWrong  map[string]int                `json:"applied_wrong"`
	LatencyP50MS  int                           `json:"latency_p50_ms"`
	LatencyP95MS  int                           `json:"latency_p95_ms"`
}

// Asker sends one request to the model and returns its answer.
type Asker func(ctx context.Context, request aiprovider.Request) (aiprovider.Response, error)

// ReadEvalDataset reads a JSONL dataset and its SHA-256.
func ReadEvalDataset(r io.Reader) ([]EvalCase, string, error) {
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(r, hash))
	scanner.Buffer(make([]byte, 0, 1<<20), 16<<20)
	var cases []EvalCase
	for line := 1; scanner.Scan(); line++ {
		if len(scanner.Bytes()) == 0 {
			continue
		}
		var c EvalCase
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			return nil, "", fmt.Errorf("line %d: %w", line, err)
		}
		if !slices.Contains(store.IntakeActions, c.Label.Action) || len(c.Items) == 0 {
			return nil, "", fmt.Errorf("line %d: a case has items and a label with action create, attach or ignore", line)
		}
		cases = append(cases, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	return cases, hex.EncodeToString(hash.Sum(nil)), nil
}

// Evaluate runs each case through the AI decider's prompt and answer reader. A case whose label names
// a conversation it does not offer cannot be reached and is not counted; one the model failed on is
// counted wrong. Severe errors: an attach labelled create (a new request merged into an old
// conversation) and an ignore of a request.
func Evaluate(ctx context.Context, cases []EvalCase, sha string, model string, options EvalOptions, ask Asker) (EvalResult, error) {
	result := EvalResult{DatasetSHA256: sha, PromptVersion: prompt.Version, Model: model, Counts: map[string]store.IntakeCounts{},
		Confusion: map[string]map[string]int{}, Applied: map[string]int{}, AppliedWrong: map[string]int{}}
	var latencies []int
	for _, c := range cases {
		snapshot, refs := c.snapshot(options.Options)
		if c.Label.Action == ActionAttachName && !slices.Contains(refs, c.Label.TargetRef) {
			result.Unreachable++
			continue
		}
		result.Cases++
		built := prompt.Build(snapshot)
		request := aiprovider.Request{System: built.System, Messages: []aiprovider.Message{{Role: aiprovider.RoleUser, Text: built.User}},
			MaxOutput: prompt.MaxOutput, Schema: prompt.Schema}
		started := time.Now()
		answer, err := askOnce(ctx, built, request, ask)
		latencies = append(latencies, int(time.Since(started).Milliseconds()))
		if err != nil && ctx.Err() != nil {
			return result, ctx.Err()
		}
		decided := "error"
		target := ""
		if err == nil {
			decided = answer.Action
			target = refOf(answer.ConversationID)
		} else {
			result.Failed++
		}
		if result.Confusion[c.Label.Action] == nil {
			result.Confusion[c.Label.Action] = map[string]int{}
		}
		result.Confusion[c.Label.Action][decided]++
		right := decided == c.Label.Action && (decided != ActionAttachName || target == c.Label.TargetRef)
		counts := result.Counts[decided]
		if err == nil {
			counts.Labels++
			switch {
			case right:
				counts.Correct++
			case decided == ActionAttachName && c.Label.Action == "create", decided == "ignore" && c.Label.Action != "ignore":
				counts.Severe++
			default:
				counts.Light++
			}
			result.Counts[decided] = counts
			if threshold, ok := options.Thresholds[decided]; ok && answer.Confidence >= threshold && !built.Credential {
				result.Applied[decided]++
				if !right {
					result.AppliedWrong[decided]++
				}
			}
		}
	}
	sort.Ints(latencies)
	if n := len(latencies); n > 0 {
		result.LatencyP50MS, result.LatencyP95MS = latencies[n/2], latencies[min(n-1, n*95/100)]
	}
	return result, nil
}

// ActionAttachName is the answer that joins a conversation.
const ActionAttachName = prompt.ActionAttach

// askOnce asks, and asks once more for an answer naming a conversation it was not offered, as the
// decider does.
func askOnce(ctx context.Context, built prompt.Context, request aiprovider.Request, ask Asker) (prompt.Answer, error) {
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var response aiprovider.Response
		if response, err = ask(ctx, request); err != nil {
			return prompt.Answer{}, err
		}
		var answer prompt.Answer
		if answer, err = built.Parse(response.JSON); !errors.Is(err, prompt.ErrUnknownLabel) {
			return answer, err
		}
	}
	return prompt.Answer{}, err
}

// The conversations of a case are known by the dataset's references; the prompt carries them as ids.
const refPrefix = "ref:"

func refOf(conversationID string) string {
	if len(conversationID) > len(refPrefix) && conversationID[:len(refPrefix)] == refPrefix {
		return conversationID[len(refPrefix):]
	}
	return ""
}

// snapshot lays a case out as the decider reads a card, with the channel's options.
func (c EvalCase) snapshot(base prompt.Snapshot) (prompt.Snapshot, []string) {
	s := base
	s.Place = prompt.Place{OneToOne: c.Place == "direct"}
	s.Before, s.Incoming, s.Candidates, s.Commands = nil, nil, nil, nil
	item := func(i EvalItem) prompt.Item {
		out := prompt.Item{ID: i.Ref, At: i.At, From: prompt.Person{Key: i.Role + ":" + i.Sender, Name: i.Sender, Team: i.Role != "customer"}, Text: i.Text}
		if q := i.Quote; q != nil {
			out.Quote = &prompt.Quote{ID: q.Ref, At: q.At, From: prompt.Person{Key: q.Role + ":" + q.Sender, Name: q.Sender, Team: q.Role != "customer"}, Text: q.Text}
		}
		return out
	}
	for _, i := range c.Before {
		s.Before = append(s.Before, item(i))
	}
	for _, i := range c.Items {
		s.Incoming = append(s.Incoming, item(i))
	}
	var refs []string
	for _, candidate := range c.Candidates {
		refs = append(refs, candidate.Ref)
		s.Candidates = append(s.Candidates, prompt.Candidate{ConversationID: refPrefix + candidate.Ref, Status: candidate.Status, UpdatedAt: candidate.UpdatedAt,
			Subject: candidate.Subject, LastMessage: candidate.Last, Reporter: prompt.Person{Key: "customer:" + candidate.Reporter, Name: candidate.Reporter}})
	}
	for _, command := range c.Commands {
		s.Commands = append(s.Commands, prompt.Command{Key: command})
	}
	return s, refs
}
