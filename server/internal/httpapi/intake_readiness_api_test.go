package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
)

type readinessView struct {
	Model   string `json:"model"`
	Actions []struct {
		Action     string   `json:"action"`
		Gate       string   `json:"gate"`
		Labels     int      `json:"labels"`
		Correct    int      `json:"correct"`
		Severe     int      `json:"severe"`
		Missing    []string `json:"missing"`
		Evaluation *struct {
			Labels int `json:"labels"`
		} `json:"evaluation"`
	} `json:"actions"`
	Applied struct {
		Decisions int `json:"decisions"`
		Corrected int `json:"corrected"`
	} `json:"applied"`
	Estimate struct {
		Cards   int `json:"cards"`
		AICalls int `json:"ai_calls"`
	} `json:"estimate"`
}

func (d levelDesk) readiness(t *testing.T, token, channel string) readinessView {
	t.Helper()
	response := request(t, d.api, http.MethodGet, "/api/v1/channels/"+channel+"/ai/intake/readiness?scope=group", nil, token, nil)
	var view readinessView
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil {
		t.Fatalf("readiness: %d %s", response.Code, response.Body.String())
	}
	return view
}

func gateOf(view readinessView, action string) (string, []string, int, int) {
	for _, a := range view.Actions {
		if a.Action == action {
			return a.Gate, a.Missing, a.Labels, a.Severe
		}
	}
	return "", nil, 0, 0
}

// labelled writes n AI decisions of action on a card, each followed by a person, and wrong more each
// corrected by a person deciding as correctedAs.
func (d levelDesk) labelled(t *testing.T, card, action string, followed, wrong int, correctedAs string) {
	t.Helper()
	if _, err := d.pool.Exec(t.Context(), `WITH ai AS (
			INSERT INTO intake_decisions(workspace_id,gathering_id,decider,basis,action,applied,not_applied_reason,items_seen,model,prompt_version)
			SELECT g.workspace_id,g.id,'ai','model',$2,false,'gate_closed','{}','claude-haiku-5-5',$5 FROM intake_gatherings g, generate_series(1,$3::int+$4::int) n
			WHERE g.id=$1 RETURNING id,workspace_id,gathering_id),
		numbered AS (SELECT ai.*, row_number() OVER () AS n FROM ai)
		INSERT INTO intake_decisions(workspace_id,gathering_id,decider,basis,action,applied,items_seen,follows_decision_id,corrects_decision_id)
		SELECT workspace_id,gathering_id,'agent','person',CASE WHEN n<=$3::int THEN $2 ELSE NULLIF($6,'') END,true,'{}',
			CASE WHEN n<=$3::int THEN id END,CASE WHEN n>$3::int THEN id END FROM numbered`, card, action, followed, wrong, prompt.Version, correctedAs); err != nil {
		t.Fatal(err)
	}
}

// An action opens with 30 explicit labels at its accuracy and no severe error: 29 right attaches and one
// that merged a new request into an old conversation keep attach closed; an evaluation counts only for
// the model and prompt intake runs; an admin may open a gate by hand, with a reason, and taking an
// evaluation's word from a token waits for a person.
func TestReadinessOpensAGateOnlyOnExplicitLabels(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	if got := d.readiness(t, d.adminA, d.chatA); got.Model != "claude-haiku-5-5" || len(got.Actions) != 3 || got.Estimate.Cards != 1 {
		t.Fatalf("readiness = %+v", got)
	}
	d.labelled(t, card, "attach", 29, 1, "create")
	d.labelled(t, card, "ignore", 30, 0, "")
	view := d.readiness(t, d.adminA, d.chatA)
	if gate, missing, labels, severe := gateOf(view, "attach"); gate != "closed" || labels != 30 || severe != 1 || strings.Join(missing, ",") != "severe" {
		t.Fatalf("attach = %s %v labels %d severe %d", gate, missing, labels, severe)
	}
	if gate, _, labels, _ := gateOf(view, "ignore"); gate != "open" || labels != 30 {
		t.Fatalf("ignore = %s labels %d", gate, labels)
	}
	if gate, missing, _, _ := gateOf(view, "create"); gate != "closed" || strings.Join(missing, ",") != "labels" {
		t.Fatalf("create = %s %v", gate, missing)
	}

	record := func(token, version string) *httptestResponse {
		response := request(t, d.api, http.MethodPost, "/api/v1/channels/"+d.chatA+"/ai/intake/evaluations", map[string]any{"scope": "group", "model": "claude-haiku-5-5",
			"prompt_version": version, "dataset_sha256": strings.Repeat("b", 64),
			"counts": map[string]any{"create": map[string]any{"labels": 40, "correct": 38, "severe": 0, "light": 2}}}, token, nil)
		return &httptestResponse{Code: response.Code, Body: response.Body.String()}
	}
	if got := record(d.adminA, "intake-2026-01-01.1"); got.Code != http.StatusCreated {
		t.Fatalf("record an old prompt's run: %+v", got)
	}
	if gate, _, labels, _ := gateOf(d.readiness(t, d.adminA, d.chatA), "create"); gate != "closed" || labels != 0 {
		t.Fatalf("an old prompt's run counted: create = %s labels %d", gate, labels)
	}
	if got := record(d.adminA, prompt.Version); got.Code != http.StatusCreated {
		t.Fatalf("record: %+v", got)
	}
	if gate, _, labels, _ := gateOf(d.readiness(t, d.adminA, d.chatA), "create"); gate != "open" || labels != 40 {
		t.Fatalf("create after its evaluation = %s labels %d", gate, labels)
	}
	// The same dataset run again counts once, as its newest run.
	if got := record(d.adminA, prompt.Version); got.Code != http.StatusCreated {
		t.Fatalf("record again: %+v", got)
	}
	if _, _, labels, _ := gateOf(d.readiness(t, d.adminA, d.chatA), "create"); labels != 40 {
		t.Fatalf("a dataset run twice counts %d labels", labels)
	}
	token := issueCustomToken(t, d.api, d.adminA, "scorer", "ai_settings:update").Token
	if got := record(token, prompt.Version); got.Code != http.StatusAccepted {
		t.Fatalf("a token records an evaluation: %+v", got)
	}
	// A repeated press under one Idempotency-Key records the run once; the key with other counts is refused.
	keyed := func(model string) int {
		return postWithKey(t, d.api, d.adminA, "/api/v1/channels/"+d.chatA+"/ai/intake/evaluations", "eval-1", map[string]any{"scope": "group", "model": model,
			"prompt_version": prompt.Version, "dataset_sha256": strings.Repeat("c", 64),
			"counts": map[string]any{"create": map[string]any{"labels": 40, "correct": 38, "severe": 0, "light": 2}}}).Code
	}
	if first, again, other := keyed("claude-haiku-5-5"), keyed("claude-haiku-5-5"), keyed("claude-sonnet-5-5"); first != http.StatusCreated || again != http.StatusOK ||
		other != http.StatusUnprocessableEntity {
		t.Fatalf("keyed record = %d, again %d, other counts %d", first, again, other)
	}

	// Opening a gate by hand needs a reason, and is a raise.
	if response := d.setLevel(t, d.adminA, d.chatA, "intake", map[string]any{"scope": "group", "level": "auto", "options": map[string]any{"gate_override": []string{"attach"}}}); response.Code != http.StatusBadRequest {
		t.Fatalf("override without a reason: %+v", response)
	}
	agentToken := issueCustomToken(t, d.api, d.adminA, "settings", "ai_settings:update").Token
	override := map[string]any{"scope": "group", "level": "suggest", "options": map[string]any{"gate_override": []string{"attach"}, "gate_override_reason": "the old desk's labels"}}
	if response := d.setLevel(t, agentToken, d.chatA, "intake", override); response.Code != http.StatusAccepted {
		t.Fatalf("a token opens a gate by hand: %+v", response)
	}
	if response := d.setLevel(t, d.adminA, d.chatA, "intake", override); response.Code != http.StatusOK {
		t.Fatalf("an admin opens a gate by hand: %+v", response)
	}
	if gate, _, _, _ := gateOf(d.readiness(t, d.adminA, d.chatA), "attach"); gate != "overridden" {
		t.Fatalf("attach after its override = %s", gate)
	}

	list := request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.chatA+"/ai/intake/evaluations?scope=group", nil, d.adminA, nil)
	if list.Code != http.StatusOK || strings.Count(list.Body.String(), `"dataset_sha256"`) != 4 {
		t.Fatalf("evaluations: %d %s", list.Code, list.Body.String())
	}
}

// Readiness and evaluations stay in their workspace, and the scope must be one the channel collects.
func TestReadinessStaysInItsWorkspace(t *testing.T) {
	d := newLevelDesk(t)
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/channels/" + d.chatA + "/ai/intake/readiness?scope=group"},
		{http.MethodGet, "/api/v1/channels/" + d.chatA + "/ai/intake/evaluations"},
	} {
		if response := request(t, d.api, call.method, call.path, nil, d.adminB, nil); response.Code != http.StatusNotFound {
			t.Errorf("B %s %s: %d", call.method, call.path, response.Code)
		}
	}
	body := map[string]any{"scope": "group", "model": "m", "prompt_version": "v", "dataset_sha256": strings.Repeat("c", 64),
		"counts": map[string]any{"ignore": map[string]any{"labels": 1, "correct": 1, "severe": 0, "light": 0}}}
	if response := request(t, d.api, http.MethodPost, "/api/v1/channels/"+d.chatA+"/ai/intake/evaluations", body, d.adminB, nil); response.Code != http.StatusNotFound {
		t.Fatalf("B records on A's channel: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, d.api, http.MethodGet, "/api/v1/channels/"+d.chatA+"/ai/intake/readiness?scope=direct", nil, d.adminA, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("a scope intake does not collect: %d", response.Code)
	}
	bad := map[string]any{"scope": "group", "model": "m", "prompt_version": "v", "dataset_sha256": strings.Repeat("c", 64),
		"counts": map[string]any{"ignore": map[string]any{"labels": 1, "correct": 2, "severe": 0, "light": 0}}}
	if response := request(t, d.api, http.MethodPost, "/api/v1/channels/"+d.chatA+"/ai/intake/evaluations", bad, d.adminA, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("more right than labelled: %d %s", response.Code, response.Body.String())
	}
}

// An AI answer that was no decision (an error, kept as action review) is not one people could change.
func TestIntakeStatsCountOnlyAIDecisions(t *testing.T) {
	d := newLevelDesk(t)
	card := d.heldCard(t, d.workspaceA, d.chatA, "u1", "the stock export fails")
	if _, err := d.pool.Exec(t.Context(), `INSERT INTO intake_decisions(workspace_id,gathering_id,decider,basis,action,applied,not_applied_reason,error_code,items_seen)
		SELECT workspace_id,id,'ai','model','review',false,'error','target_invalid','{}' FROM intake_gatherings WHERE id=$1`, card); err != nil {
		t.Fatal(err)
	}
	d.aiSuggestion(t, d.workspaceA, card, "create", "", "", "substantive", 1)
	response := request(t, d.api, http.MethodGet, "/api/v1/intake/stats?channel_id="+d.chatA, nil, d.adminA, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ai_decisions":1`) {
		t.Fatalf("stats: %d %s", response.Code, response.Body.String())
	}
}
