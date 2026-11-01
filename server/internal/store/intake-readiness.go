package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Readiness of a scope's intake (docs/user/ai.md "When AI acts on its own"): AI acts on its own only for
// an action people's explicit labels show it gets right, counted from people following or correcting its
// suggestions and from offline evaluations of the same model and prompt.
const (
	// IntakeLabelsNeeded is how many labels an action needs before it may open.
	IntakeLabelsNeeded = 30
	// IntakeLabelWindow is how far back people's labels and severe errors are counted.
	IntakeLabelWindow = 30 * 24 * time.Hour
	// intakeJunkPerWeek is how many conversations AI opened for chatter a week create allows.
	intakeJunkPerWeek = 1
)

// Gates of an action.
const (
	GateOpen       = "open"
	GateClosed     = "closed"
	GateOverridden = "overridden"
)

// IntakeActions are the actions AI may take on its own.
var IntakeActions = []string{"create", "attach", "ignore"}

// intakeAccuracyNeeded is the share of labels an action must get right.
var intakeAccuracyNeeded = map[string]float64{"create": 0.90, "attach": 0.95, "ignore": 0.95}

// IntakeCounts are the labelled decisions of an action: how many, right, and the severe and light errors.
type IntakeCounts struct {
	Labels  int `json:"labels"`
	Correct int `json:"correct"`
	Severe  int `json:"severe"`
	Light   int `json:"light"`
}

// IntakeEvaluation is one recorded offline evaluation of a scope.
type IntakeEvaluation struct {
	ID            string                  `json:"id"`
	ChannelID     string                  `json:"channel_id"`
	Scope         string                  `json:"scope"`
	Model         string                  `json:"model"`
	PromptVersion string                  `json:"prompt_version"`
	DatasetSHA256 string                  `json:"dataset_sha256"`
	Counts        map[string]IntakeCounts `json:"counts"`
	RunByAgentID  *string                 `json:"run_by_agent_id"`
	ViaToken      bool                    `json:"via_token"`
	CreatedAt     time.Time               `json:"created_at"`
}

// IntakeEvaluationInput is what an operator records of a run.
type IntakeEvaluationInput struct {
	Scope, Model, PromptVersion, DatasetSHA256 string
	Counts                                     map[string]IntakeCounts
}

// ErrInvalidEvaluation is counts that cannot be: an unknown action, or more right than labelled.
var ErrInvalidEvaluation = errors.New("an evaluation counts labels of create, attach and ignore, never more right or wrong than labelled")

func checkCounts(counts map[string]IntakeCounts) error {
	for action, c := range counts {
		if !slices.Contains(IntakeActions, action) || c.Labels < 0 || c.Correct < 0 || c.Severe < 0 || c.Light < 0 ||
			c.Correct+c.Severe+c.Light > c.Labels {
			return ErrInvalidEvaluation
		}
	}
	return nil
}

const evaluationColumns = `id::text,channel_id::text,scope,model,prompt_version,dataset_sha256,counts,run_by_agent_id::text,run_by_token_id IS NOT NULL,created_at`

func scanEvaluation(row pgx.Row) (IntakeEvaluation, error) {
	var e IntakeEvaluation
	var counts []byte
	if err := row.Scan(&e.ID, &e.ChannelID, &e.Scope, &e.Model, &e.PromptVersion, &e.DatasetSHA256, &counts, &e.RunByAgentID, &e.ViaToken, &e.CreatedAt); err != nil {
		return e, err
	}
	e.CreatedAt = domain.UTC(e.CreatedAt)
	return e, json.Unmarshal(counts, &e.Counts)
}

// RecordIntakeEvaluation keeps the counts of an offline evaluation of a channel's scope.
func (s *Store) RecordIntakeEvaluation(ctx context.Context, workspaceID, channelID string, in IntakeEvaluationInput, actor ChannelActor, now time.Time) (IntakeEvaluation, error) {
	if err := checkCounts(in.Counts); err != nil {
		return IntakeEvaluation{}, err
	}
	counts, err := json.Marshal(in.Counts)
	if err != nil {
		return IntakeEvaluation{}, err
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeEvaluation, error) {
		if _, err := s.channelByID(ctx, tx, workspaceID, channelID); err != nil {
			return IntakeEvaluation{}, err
		}
		return scanEvaluation(tx.QueryRow(ctx, `INSERT INTO intake_eval_runs(workspace_id,channel_id,scope,model,prompt_version,dataset_sha256,counts,
				run_by_agent_id,run_by_token_id,created_at)
			VALUES($1,$2::uuid,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10) RETURNING `+evaluationColumns,
			workspaceID, channelID, in.Scope, in.Model, in.PromptVersion, in.DatasetSHA256, counts, actor.AgentID, actor.TokenID, now))
	})
}

// MaxIntakeEvaluations is the most runs a list holds, newest first.
const MaxIntakeEvaluations = 100

// IntakeEvaluations lists a channel's recorded evaluations, newest first; scope narrows them.
func (s *Store) IntakeEvaluations(ctx context.Context, workspaceID, channelID, scope string) ([]IntakeEvaluation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]IntakeEvaluation, error) {
		if _, err := s.channelByID(ctx, tx, workspaceID, channelID); err != nil {
			return nil, err
		}
		rows, err := tx.Query(ctx, `SELECT `+evaluationColumns+` FROM intake_eval_runs WHERE workspace_id=$1 AND channel_id=$2::uuid AND ($3='' OR scope=$3)
			ORDER BY created_at DESC,id DESC LIMIT $4`, workspaceID, channelID, scope, MaxIntakeEvaluations)
		if err != nil {
			return nil, fmt.Errorf("read intake evaluations: %w", err)
		}
		list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (IntakeEvaluation, error) { return scanEvaluation(row) })
		if list == nil {
			list = []IntakeEvaluation{}
		}
		return list, err
	})
}

// IntakeLabelCounts are labels of an action from one source.
type IntakeLabelCounts struct {
	Labels  int `json:"labels"`
	Correct int `json:"correct"`
	Wrong   int `json:"wrong"`
	Severe  int `json:"severe"`
}

// IntakeEvaluationCounts are the labels of the newest run of each dataset of the model and prompt.
type IntakeEvaluationCounts struct {
	IntakeLabelCounts
	RunAt time.Time `json:"run_at"`
}

// IntakeReadinessAction is whether AI may take one action on its own, and what it misses.
type IntakeReadinessAction struct {
	Action         string                  `json:"action"`
	Gate           string                  `json:"gate"`
	Labels         int                     `json:"labels"`
	Correct        int                     `json:"correct"`
	Wrong          int                     `json:"wrong"`
	Severe         int                     `json:"severe"`
	Needed         int                     `json:"needed"`
	AccuracyNeeded float64                 `json:"accuracy_needed"`
	Missing        []string                `json:"missing"`
	Live           IntakeLabelCounts       `json:"live"`
	Evaluation     *IntakeEvaluationCounts `json:"evaluation"`
}

// IntakeApplied counts what AI did on its own in the window and how much of it people corrected.
type IntakeApplied struct {
	Decisions      int      `json:"decisions"`
	Corrected      int      `json:"corrected"`
	CorrectionRate *float64 `json:"correction_rate"`
}

// IntakeEstimate is what the last days say a month of the scope at auto costs.
type IntakeEstimate struct {
	Days        int `json:"days"`
	Cards       int `json:"cards"`
	AICalls     int `json:"ai_calls"`
	CostPerCall struct {
		MicroUSD     *int64   `json:"micro_usd"`
		ProviderCost *float64 `json:"provider_cost"`
	} `json:"cost_per_call"`
}

// IntakeReadiness is a scope's readiness for AI to act on its own, for the model and prompt it runs.
type IntakeReadiness struct {
	ChannelID     string                  `json:"channel_id"`
	Scope         string                  `json:"scope"`
	Model         string                  `json:"model"`
	PromptVersion string                  `json:"prompt_version"`
	Actions       []IntakeReadinessAction `json:"actions"`
	Applied       IntakeApplied           `json:"applied"`
	Estimate      IntakeEstimate          `json:"estimate"`
}

// Open says whether the action's gate lets AI act on its own.
func (r IntakeReadiness) Open(action string) bool {
	for _, a := range r.Actions {
		if a.Action == action {
			return a.Gate == GateOpen || a.Gate == GateOverridden
		}
	}
	return false
}

// IntakeReadinessOf reads a channel scope's readiness for model and promptVersion; overrides are the
// actions an admin opened by hand. channels.ErrNotFound for another workspace's channel.
func (s *Store) IntakeReadinessOf(ctx context.Context, workspaceID, channelID, scope, model, promptVersion string, overrides []string, now time.Time) (IntakeReadiness, error) {
	readiness := IntakeReadiness{ChannelID: channelID, Scope: scope, Model: model, PromptVersion: promptVersion, Actions: []IntakeReadinessAction{}}
	if !isID(channelID) {
		return readiness, channels.ErrNotFound
	}
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (IntakeReadiness, error) {
		if _, err := s.channelByID(ctx, tx, workspaceID, channelID); err != nil {
			return readiness, err
		}
		since := now.Add(-IntakeLabelWindow)
		live := map[string]IntakeLabelCounts{}
		junk := 0
		// A person's decision that followed an AI decision makes it right, one that corrected it wrong:
		// severe when it hid a request (an ignore people took back and opened or joined) or merged one
		// into an old conversation (an attach people moved to a new one).
		rows, err := tx.Query(ctx, `WITH ai AS (
				SELECT d.id,d.action,d.created_at FROM intake_decisions d JOIN intake_gatherings g ON g.workspace_id=d.workspace_id AND g.id=d.gathering_id
				WHERE d.workspace_id=$1 AND g.channel_id=$2::uuid AND g.scope=$3 AND d.decider='ai' AND d.action IN ('create','attach','ignore') AND d.created_at>=$4),
			labelled AS (
				SELECT ai.action,ai.created_at,
					EXISTS(SELECT 1 FROM intake_decisions p WHERE p.workspace_id=$1 AND p.follows_decision_id=ai.id AND p.decider='agent') AS followed,
					(SELECT p.action FROM intake_decisions p WHERE p.workspace_id=$1 AND p.corrects_decision_id=ai.id AND p.decider='agent'
						ORDER BY p.created_at,p.id LIMIT 1) AS corrected_as
				FROM ai)
			SELECT action,
				count(*) FILTER (WHERE followed OR corrected_as IS NOT NULL),
				count(*) FILTER (WHERE followed),
				count(*) FILTER (WHERE NOT followed AND (action='ignore' AND corrected_as IN ('create','attach') OR action='attach' AND corrected_as='create')),
				count(*) FILTER (WHERE NOT followed AND action='create' AND corrected_as IN ('ignore','review') AND created_at>=$5)
			FROM labelled GROUP BY action`, workspaceID, channelID, scope, since, now.Add(-7*24*time.Hour))
		if err != nil {
			return readiness, fmt.Errorf("read intake labels: %w", err)
		}
		for rows.Next() {
			var action string
			var c IntakeLabelCounts
			var junkWeek int
			if err := rows.Scan(&action, &c.Labels, &c.Correct, &c.Severe, &junkWeek); err != nil {
				rows.Close()
				return readiness, fmt.Errorf("read intake labels: %w", err)
			}
			c.Wrong = c.Labels - c.Correct
			live[action] = c
			if action == "create" {
				junk = junkWeek
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return readiness, err
		}

		evaluation := map[string]*IntakeEvaluationCounts{}
		if model != "" {
			rows, err := tx.Query(ctx, `SELECT DISTINCT ON (dataset_sha256) counts,created_at FROM intake_eval_runs
				WHERE workspace_id=$1 AND channel_id=$2::uuid AND scope=$3 AND model=$4 AND prompt_version=$5
				ORDER BY dataset_sha256,created_at DESC`, workspaceID, channelID, scope, model, promptVersion)
			if err != nil {
				return readiness, fmt.Errorf("read intake evaluations: %w", err)
			}
			for rows.Next() {
				var raw []byte
				var at time.Time
				if err := rows.Scan(&raw, &at); err != nil {
					rows.Close()
					return readiness, err
				}
				var counts map[string]IntakeCounts
				if err := json.Unmarshal(raw, &counts); err != nil {
					rows.Close()
					return readiness, err
				}
				for action, c := range counts {
					e := evaluation[action]
					if e == nil {
						e = &IntakeEvaluationCounts{}
						evaluation[action] = e
					}
					e.Labels, e.Correct, e.Severe = e.Labels+c.Labels, e.Correct+c.Correct, e.Severe+c.Severe
					e.Wrong = e.Labels - e.Correct
					if at.After(e.RunAt) {
						e.RunAt = domain.UTC(at)
					}
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return readiness, err
			}
		}

		for _, action := range IntakeActions {
			l := live[action]
			a := IntakeReadinessAction{Action: action, Needed: IntakeLabelsNeeded, AccuracyNeeded: intakeAccuracyNeeded[action], Live: l, Evaluation: evaluation[action],
				Missing: []string{}, Labels: l.Labels, Correct: l.Correct, Severe: l.Severe}
			if e := a.Evaluation; e != nil {
				a.Labels, a.Correct, a.Severe = a.Labels+e.Labels, a.Correct+e.Correct, a.Severe+e.Severe
			}
			a.Wrong = a.Labels - a.Correct
			if a.Labels < IntakeLabelsNeeded {
				a.Missing = append(a.Missing, "labels")
			}
			if a.Labels > 0 && float64(a.Correct) < a.AccuracyNeeded*float64(a.Labels) {
				a.Missing = append(a.Missing, "accuracy")
			}
			if action != "create" && a.Severe > 0 {
				a.Missing = append(a.Missing, "severe")
			}
			if action == "create" && junk > intakeJunkPerWeek {
				a.Missing = append(a.Missing, "junk")
			}
			switch {
			case slices.Contains(overrides, action):
				a.Gate = GateOverridden
			case len(a.Missing) == 0:
				a.Gate = GateOpen
			default:
				a.Gate = GateClosed
			}
			readiness.Actions = append(readiness.Actions, a)
		}

		if err := tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE EXISTS(SELECT 1 FROM intake_decisions p WHERE p.workspace_id=d.workspace_id
				AND p.corrects_decision_id=d.id AND p.decider='agent'))
			FROM intake_decisions d JOIN intake_gatherings g ON g.workspace_id=d.workspace_id AND g.id=d.gathering_id
			WHERE d.workspace_id=$1 AND g.channel_id=$2::uuid AND g.scope=$3 AND d.decider='ai' AND d.applied AND d.created_at>=$4`,
			workspaceID, channelID, scope, since).Scan(&readiness.Applied.Decisions, &readiness.Applied.Corrected); err != nil {
			return readiness, fmt.Errorf("read applied intake decisions: %w", err)
		}
		if readiness.Applied.Decisions > 0 {
			rate := float64(readiness.Applied.Corrected) / float64(readiness.Applied.Decisions)
			readiness.Applied.CorrectionRate = &rate
		}

		readiness.Estimate.Days = int(IntakeLabelWindow / (24 * time.Hour))
		var micro *float64
		if err := tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM intake_gatherings WHERE workspace_id=$1 AND channel_id=$2::uuid AND scope=$3 AND closed_at>=$4),
				(SELECT count(*) FROM intake_gatherings WHERE workspace_id=$1 AND channel_id=$2::uuid AND scope=$3 AND closed_at>=$4 AND rules_basis='default'),
				(SELECT avg(cost_micro_usd)::float8 FROM ai_usage WHERE workspace_id=$1 AND feature='intake' AND status='ok' AND started_at>=$4 AND cost_micro_usd IS NOT NULL),
				(SELECT avg(provider_cost)::float8 FROM ai_usage WHERE workspace_id=$1 AND feature='intake' AND status='ok' AND started_at>=$4 AND provider_cost IS NOT NULL)`,
			workspaceID, channelID, scope, since).Scan(&readiness.Estimate.Cards, &readiness.Estimate.AICalls, &micro, &readiness.Estimate.CostPerCall.ProviderCost); err != nil {
			return readiness, fmt.Errorf("read intake estimate: %w", err)
		}
		if micro != nil {
			rounded := int64(*micro + 0.5)
			readiness.Estimate.CostPerCall.MicroUSD = &rounded
		}
		return readiness, nil
	})
}
