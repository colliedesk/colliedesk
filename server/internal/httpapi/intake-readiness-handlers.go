package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/intake/prompt"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Readiness of a chat channel's intake (docs/user/ai.md "When AI acts on its own"): per action, whether
// people's explicit labels and offline evaluations of the running model and prompt let AI act on its
// own, and what one more month at auto would cost. Nothing here calls a model: offline evaluations run
// with `collie intake-eval` and only their counts come here.

var datasetDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// intakeScope reads the scope a request names and checks the channel's type collects it for intake.
func (a *API) intakeScope(response http.ResponseWriter, request *http.Request, workspaceID, channelID, scope string) (ai.ChannelRef, bool) {
	ref, _, err := a.channelRef(request.Context(), workspaceID, channelID)
	if err != nil {
		writeChannelError(response, err)
		return ref, false
	}
	if !slices.Contains(ref.IntakeScopes, scope) {
		writeError(response, http.StatusBadRequest, "invalid_request", "scope is one of the scopes the channel's type collects for intake.")
		return ref, false
	}
	return ref, true
}

func (a *API) getIntakeReadiness(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	workspaceID, scope := principal(request).Workspace.ID, request.URL.Query().Get("scope")
	ref, ok := a.intakeScope(response, request, workspaceID, id, scope)
	if !ok {
		return
	}
	model, err := a.ai.SlotModel(request.Context(), workspaceID, ai.SlotIntake)
	if err != nil {
		writeAIError(response, err)
		return
	}
	levels, err := a.ai.ChannelAI(request.Context(), workspaceID, ref)
	if err != nil {
		writeAIError(response, err)
		return
	}
	readiness, err := a.store.IntakeReadinessOf(request.Context(), workspaceID, id, scope, model, prompt.Version, gateOverrides(levels, scope), a.now())
	if err != nil {
		writeChannelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, readiness)
}

// gateOverrides are the actions an admin opened by hand on a scope's intake.
func gateOverrides(levels ai.ChannelAI, scope string) []string {
	for _, s := range levels.Scopes {
		if s.Scope != scope {
			continue
		}
		for _, slot := range s.Slots {
			if slot.Slot == ai.SlotIntake && slot.Options != nil {
				return slot.Options.GateOverride
			}
		}
	}
	return nil
}

func (a *API) recordIntakeEvaluation(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	var input struct {
		Scope         string                        `json:"scope"`
		Model         string                        `json:"model"`
		PromptVersion string                        `json:"prompt_version"`
		DatasetSHA256 string                        `json:"dataset_sha256"`
		Counts        map[string]store.IntakeCounts `json:"counts"`
	}
	if decodeJSON(request, &input) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send scope, model, prompt_version, dataset_sha256 and counts.")
		return
	}
	if input.Model == "" || len(input.Model) > 200 || input.PromptVersion == "" || len(input.PromptVersion) > 80 || !datasetDigest.MatchString(input.DatasetSHA256) || len(input.Counts) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send scope, model, prompt_version, dataset_sha256 (64 lowercase hex) and counts per action.")
		return
	}
	who := principal(request)
	if _, ok := a.intakeScope(response, request, who.Workspace.ID, id, input.Scope); !ok {
		return
	}
	actor := store.ChannelActor{AgentID: who.Agent.ID}
	if who.IsToken() {
		actor.TokenID = who.SessionID
	}
	counts, _ := json.Marshal(input.Counts)
	a.createOnce(response, request, intakeEvaluationOperation, []string{id, input.Scope, input.Model, input.PromptVersion, input.DatasetSHA256, string(counts)}, func(ctx context.Context) (any, error) {
		evaluation, err := a.store.RecordIntakeEvaluation(ctx, who.Workspace.ID, id, store.IntakeEvaluationInput{Scope: input.Scope, Model: input.Model,
			PromptVersion: input.PromptVersion, DatasetSHA256: input.DatasetSHA256, Counts: input.Counts}, actor, a.now())
		if errors.Is(err, store.ErrInvalidEvaluation) {
			writeError(response, http.StatusBadRequest, "invalid_request", "counts name create, attach and ignore, each with labels, correct, severe and light, never more right or wrong than labelled.")
			return nil, errAnswered
		}
		if err != nil {
			writeChannelError(response, err)
			return nil, errAnswered
		}
		return evaluation, nil
	})
}

func (a *API) listIntakeEvaluations(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	evaluations, err := a.store.IntakeEvaluations(request.Context(), principal(request).Workspace.ID, id, request.URL.Query().Get("scope"))
	if err != nil {
		writeChannelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": evaluations})
}
