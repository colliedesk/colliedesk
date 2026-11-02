package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/ai"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// AI levels per channel (docs/user/ai.md "Levels per channel"): what each AI feature does by itself on
// a channel and its scopes. The levels live in the AI settings, not in the channel's settings, because
// changing a channel is a lower tier than letting AI do more by itself.

const channelAIRoute = "/api/v1/channels/{id}/ai/{slot}"

// channelRef reads what the AI settings need of a channel: its type's scopes.
func (a *API) channelRef(ctx context.Context, workspaceID, id string) (ai.ChannelRef, channels.Channel, error) {
	channel, err := a.store.GetChannel(ctx, workspaceID, id)
	if err != nil {
		return ai.ChannelRef{}, channel, err
	}
	ref := ai.ChannelRef{ID: channel.ID, Name: channel.Name, Type: string(channel.Type)}
	if adapter, ok := a.store.Channels().Adapter(channel.Type); ok {
		definition := adapter.Definition()
		ref.Scopes, ref.IntakeScopes = definition.Scopes, definition.IntakeScopes
	}
	return ref, channel, nil
}

func (a *API) getChannelAI(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	workspaceID := principal(request).Workspace.ID
	ref, _, err := a.channelRef(request.Context(), workspaceID, id)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	levels, err := a.ai.ChannelAI(request.Context(), workspaceID, ref)
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, levels)
}

// setChannelAI sets one feature's level on a scope of a channel. Lowering needs ai_settings:update;
// a change that lets AI do more by itself (a higher level, looser intake options) waits for a person
// whatever the token holds, as turning a feature on does.
func (a *API) setChannelAI(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, domain.MaxAgentRequestBody+1))
	if err != nil || len(body) > domain.MaxAgentRequestBody {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send scope, level (off, suggest, auto, or null to follow) and, for intake, options; at most 64 KiB.")
		return
	}
	var input struct {
		Scope   *string         `json:"scope"`
		Level   json.RawMessage `json:"level"`
		Options json.RawMessage `json:"options"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var level *string
	if decoder.Decode(&input) != nil || input.Scope == nil || len(input.Level) == 0 || json.Unmarshal(input.Level, &level) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send scope ('' for the whole channel), level (off, suggest, auto, or null to follow the channel and the workspace) and, for intake, options.")
		return
	}
	who := principal(request)
	ref, channel, err := a.channelRef(request.Context(), who.Workspace.ID, id)
	if err == nil && !channel.Live() {
		err = channels.ErrNotFound
	}
	if err != nil {
		writeChannelError(response, err)
		return
	}
	// A call that could never run is refused now, not filed for a person to approve in vain.
	change := ai.ChannelLevelInput{Scope: *input.Scope, Level: level, Options: input.Options}
	if _, err := ai.CheckChannelLevel(ref, request.PathValue("slot"), change); err != nil {
		writeChannelAIError(response, err)
		return
	}
	if !who.Can(auth.PermAISettingsUpdate) {
		a.writePermissionMissing(response, request, who, auth.PermAISettingsUpdate)
		return
	}
	// Raising what AI does by itself takes ai_settings:update, which this caller holds.
	levels, err := a.ai.SetChannelLevel(request.Context(), who.Workspace.ID, aiActor(request), ref, request.PathValue("slot"), change, true)
	if err != nil {
		writeChannelAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, levels)
}

func writeChannelAIError(response http.ResponseWriter, err error) {
	var unsupported *ai.IntakeNotSupportedError
	switch {
	case errors.As(err, &unsupported):
		writeJSON(response, http.StatusUnprocessableEntity, map[string]any{"error": errorBody(response, map[string]any{
			"code": "intake_not_supported", "available_slots": unsupported.Available,
			"message": "Intake runs only on chat channels, on the scopes their type collects messages in. Email and web forms keep their rules; triage and drafts work there.",
		})})
	case errors.Is(err, ai.ErrLevelNotAllowed):
		writeError(response, http.StatusUnprocessableEntity, "ai_level_not_allowed",
			"Drafts are never sent by AI, so they go up to suggest; intake runs by itself only on a channel's scope, never as the workspace's default.")
	case errors.Is(err, ai.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No AI feature with that name: triage, draft or intake.")
	default:
		writeAIError(response, err)
	}
}

func (a *API) setAISourceConsent(response http.ResponseWriter, request *http.Request) {
	consent, err := a.ai.SetSourceConsent(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("class"))
	if errors.Is(err, ai.ErrSourceClass) {
		writeError(response, http.StatusNotFound, "not_found", "A source class is images, contact_profile or external:<name>.")
		return
	}
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, consent)
}

func (a *API) removeAISourceConsent(response http.ResponseWriter, request *http.Request) {
	removed, err := a.ai.RemoveSourceConsent(request.Context(), principal(request).Workspace.ID, aiActor(request), request.PathValue("class"))
	if errors.Is(err, ai.ErrSourceClass) {
		writeError(response, http.StatusNotFound, "not_found", "A source class is images, contact_profile or external:<name>.")
		return
	}
	if err != nil {
		writeAIError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"removed": removed})
}
