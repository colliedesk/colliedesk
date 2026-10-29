package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// The routes of every channel type (contracts: /api/v1/channels). They know no type: what is particular
// to one comes from its adapter through the store.

const (
	channelEventsPageSize = 50
	maxHeldEventIDs       = 500
)

type channelStateBody struct {
	State      string  `json:"state"`
	ReasonCode *string `json:"reason_code"`
	Message    *string `json:"message"`
}

type channelBody struct {
	ID            string           `json:"id"`
	Type          channels.Type    `json:"type"`
	Name          string           `json:"name"`
	Enabled       bool             `json:"enabled"`
	Status        channelStateBody `json:"status"`
	Summary       string           `json:"summary"`
	Capabilities  []string         `json:"capabilities"`
	HeldCount     int              `json:"held_count"`
	UnsentCount   int              `json:"unsent_count"`
	LastInboundAt *string          `json:"last_inbound_at"`
	IsDefault     bool             `json:"is_default"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
	// DeletedAt is set on a removed channel, which only the list with include_removed shows.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type channelDetailBody struct {
	channelBody
	Config any `json:"config"`
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (a *API) channelJSON(public store.ChannelPublic) channelBody {
	channel := public.Channel
	capabilities := []string{}
	if adapter, ok := a.store.Channels().Adapter(channel.Type); ok {
		capabilities = append(capabilities, adapter.Definition().Capabilities...)
	}
	return channelBody{ID: channel.ID, Type: channel.Type, Name: channel.Name, Enabled: channel.Enabled,
		Status:  channelStateBody{State: public.Status.State, ReasonCode: optionalText(public.Status.ReasonCode), Message: optionalText(public.Status.Message)},
		Summary: public.Summary, Capabilities: capabilities, HeldCount: public.Held, UnsentCount: public.Unsent, LastInboundAt: public.LastSeen,
		IsDefault: channel.IsDefault, CreatedAt: channel.CreatedAt, UpdatedAt: channel.UpdatedAt, DeletedAt: channel.DeletedAt}
}

func (a *API) writeChannelDetail(response http.ResponseWriter, status int, public store.ChannelPublic) {
	writeJSON(response, status, channelDetailBody{channelBody: a.channelJSON(public), Config: public.Config})
}

// writeChannelConflict answers a value another channel of the workspace has: 409 with the field and
// the channel, which is always of the same workspace, so naming it tells nothing of another. It reports
// whether err was such a conflict.
func writeChannelConflict(response http.ResponseWriter, err error) bool {
	var nameErr *channels.NameTakenError
	var addressErr *store.AddressTakenError
	conflict := func(code, field, message, channelID, channelName string) {
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": code, "message": message,
			"fields": map[string]string{field: message}, "conflict": map[string]string{"channel_id": channelID, "channel_name": channelName}})})
	}
	switch {
	case errors.As(err, &nameErr):
		conflict("channel_name_taken", "name", "Another channel of this type is called "+nameErr.ChannelName+".", nameErr.ChannelID, nameErr.ChannelName)
	case errors.As(err, &addressErr):
		conflict("support_address_taken", "support_address", "The email channel "+addressErr.ChannelName+" already receives mail for "+addressErr.SupportAddress+
			" (an address with a +tag reaches the same mailbox).", addressErr.ChannelID, addressErr.ChannelName)
	case errors.Is(err, channels.ErrConflict):
		writeError(response, http.StatusConflict, "channel_conflict", "Another change of these channels was made at the same moment. Nothing was changed; try again.")
	default:
		return false
	}
	return true
}

// writeChannelError answers the frame's errors the same for every type.
func writeChannelError(response http.ResponseWriter, err error) {
	var fieldErr *channels.FieldError
	var heldErr *channels.HeldError
	if writeChannelConflict(response, err) {
		return
	}
	switch {
	case errors.As(err, &fieldErr):
		code := fieldErr.Code
		status := http.StatusBadRequest
		if code == "" {
			code = "invalid_request"
		} else {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(response, status, map[string]any{"error": errorBody(response, map[string]any{"code": code, "message": fieldErr.Message, "fields": fieldErr.Fields})})
	case errors.As(err, &heldErr):
		writeHeld(response, heldErr)
	case errors.Is(err, channels.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		writeError(response, http.StatusNotFound, "channel_not_found", "No channel of this workspace has that id.")
	case errors.Is(err, channels.ErrExternalAccount):
		writeError(response, http.StatusConflict, "channel_account_taken", "That account is already connected to a channel.")
	case errors.Is(err, channels.ErrUnknownType):
		writeError(response, http.StatusBadRequest, "invalid_request", "This server runs no channel of that type.")
	case errors.Is(err, store.ErrSealKeysMissing):
		logMissingSettings("channel_secrets", "MAILBOX_TOKEN_KEYS")
		writeError(response, http.StatusConflict, "secret_keys_missing", "This server cannot keep a channel secret yet: its key that encrypts them is not set up, so ask whoever runs it.")
	case errors.Is(err, store.ErrRateLimited):
		response.Header().Set("Retry-After", strconv.Itoa(int(store.InboundRateWindow.Seconds())))
		writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many at once. Try again later.")
	case errors.Is(err, store.ErrBadCursor):
		writeError(response, http.StatusBadRequest, "invalid_request", "The cursor is not one this server handed out.")
	default:
		writeStoreError(response, err)
	}
}

// writeHeld refuses to remove a channel that still holds messages, with how many.
func writeHeld(response http.ResponseWriter, held *channels.HeldError) {
	noun := "messages"
	if held.Count == 1 {
		noun = "message"
	}
	writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": "channel_has_held", "held_count": held.Count,
		"message": fmt.Sprintf("This channel holds %d %s that arrived while it was off. Open or discard them first.", held.Count, noun)})})
}

// channelID reads the {id} of a channel route; anything but a UUID is no channel.
func channelID(response http.ResponseWriter, request *http.Request) (string, bool) {
	id := request.PathValue("id")
	if !uuidShape.MatchString(id) {
		writeError(response, http.StatusNotFound, "channel_not_found", "No channel of this workspace has that id.")
		return "", false
	}
	return id, true
}

func channelActor(request *http.Request) store.ChannelActor {
	who := principal(request)
	actor := store.ChannelActor{AgentID: who.Agent.ID}
	if who.IsToken() {
		actor.TokenID = who.SessionID
	}
	return actor
}

func (a *API) listChannels(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	includeRemoved := request.URL.Query().Get("include_removed") == "true"
	list, err := a.store.ListChannels(request.Context(), workspaceID, includeRemoved)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	counts, err := a.store.CountChannelsOfType(request.Context(), workspaceID)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	items := make([]channelBody, 0, len(list))
	for _, public := range list {
		items = append(items, a.channelJSON(public))
	}
	type typeBody struct {
		Type             channels.Type `json:"type"`
		Available        bool          `json:"available"`
		Count            int           `json:"count"`
		DefaultChannelID *string       `json:"default_channel_id"`
	}
	defaults := map[channels.Type]*string{}
	for _, public := range list {
		if public.Channel.IsDefault {
			id := public.Channel.ID
			defaults[public.Channel.Type] = &id
		}
	}
	types := []typeBody{}
	for _, definition := range a.store.Channels().Definitions() {
		types = append(types, typeBody{Type: definition.Type, Available: true, Count: counts[definition.Type], DefaultChannelID: defaults[definition.Type]})
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "types": types})
}

func (a *API) getChannel(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	public, err := a.store.GetChannelPublic(request.Context(), principal(request).Workspace.ID, id)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	a.writeChannelDetail(response, http.StatusOK, public)
}

// recordActivation records the type's product event when a channel is connected or moved to another
// account, as connecting email always has (email_channel_saved), whichever route did it.
func (a *API) recordActivation(workspaceID string, before *store.ChannelPublic, after store.ChannelPublic) {
	adapter, ok := a.store.Channels().Adapter(after.Channel.Type)
	if !ok || adapter.Definition().ActivationEvent == "" {
		return
	}
	if before == nil || before.Summary != after.Summary {
		a.recordFunnel(adapter.Definition().ActivationEvent, workspaceID, nil)
	}
}

func (a *API) createChannel(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Type      channels.Type   `json:"type"`
		Name      *string         `json:"name"`
		Enabled   *bool           `json:"enabled"`
		IsDefault *bool           `json:"is_default"`
		Config    json.RawMessage `json:"config"`
		Settings  json.RawMessage `json:"settings"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Type == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be a channel with its type.")
		return
	}
	workspaceID := principal(request).Workspace.ID
	name, enabled, isDefault := "", "", ""
	if input.Name != nil {
		name = "name:" + *input.Name
	}
	if input.Enabled != nil {
		enabled = strconv.FormatBool(*input.Enabled)
	}
	if input.IsDefault != nil {
		isDefault = strconv.FormatBool(*input.IsDefault)
	}
	fingerprint := []string{string(input.Type), name, enabled, isDefault, string(input.Config), string(input.Settings)}
	a.createOnce(response, request, createChannelOperation, fingerprint, func(ctx context.Context) (any, error) {
		public, err := a.store.CreateChannelPublic(ctx, workspaceID, input.Type,
			store.ChannelInput{Name: input.Name, Enabled: input.Enabled, IsDefault: input.IsDefault, Config: input.Config, Settings: input.Settings}, channelActor(request))
		if err != nil {
			writeChannelError(response, err)
			return nil, errAnswered
		}
		a.recordActivation(workspaceID, nil, public)
		return channelDetailBody{channelBody: a.channelJSON(public), Config: public.Config}, nil
	})
}

func (a *API) updateChannel(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	var input struct {
		Name      *string         `json:"name"`
		Enabled   *bool           `json:"enabled"`
		IsDefault *bool           `json:"is_default"`
		Config    json.RawMessage `json:"config"`
		Settings  json.RawMessage `json:"settings"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.Name == nil && input.Enabled == nil && input.IsDefault == nil && input.Config == nil && input.Settings == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send at least one of name, enabled, is_default, config or settings.")
		return
	}
	workspaceID := principal(request).Workspace.ID
	before, err := a.store.GetChannelPublic(request.Context(), workspaceID, id)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	if _, err := a.store.UpdateChannel(request.Context(), workspaceID, id,
		store.ChannelInput{Name: input.Name, Enabled: input.Enabled, IsDefault: input.IsDefault, Config: input.Config, Settings: input.Settings}, channelActor(request)); err != nil {
		writeChannelError(response, err)
		return
	}
	after, err := a.store.GetChannelPublic(request.Context(), workspaceID, id)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	if input.Config != nil {
		a.recordActivation(workspaceID, &before, after)
	}
	a.writeChannelDetail(response, http.StatusOK, after)
}

func (a *API) removeChannel(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	if err := a.store.RemoveChannel(request.Context(), principal(request).Workspace.ID, id, channelActor(request)); err != nil {
		writeChannelError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) setChannelCredential(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	var input struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Name == "" || input.Value == "" || len(input.Value) > 4096 {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be {\"name\": \"<secret>\", \"value\": \"...\"}.")
		return
	}
	hint, err := a.store.SetChannelCredential(request.Context(), principal(request).Workspace.ID, id, input.Name, input.Value, channelActor(request))
	if err != nil {
		writeChannelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"name": input.Name, "hint": optionalText(hint)})
}

func (a *API) checkChannel(response http.ResponseWriter, request *http.Request) {
	a.getChannel(response, request)
}

// heldInput reads the event ids of a held-messages action; none means all.
func heldInput(response http.ResponseWriter, request *http.Request, target any, ids func() []string) bool {
	if err := decodeJSON(request, target); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The body must be a JSON object.")
		return false
	}
	list := ids()
	if len(list) > maxHeldEventIDs {
		writeError(response, http.StatusBadRequest, "invalid_request", "At most "+strconv.Itoa(maxHeldEventIDs)+" messages at a time.")
		return false
	}
	for _, id := range list {
		if !uuidShape.MatchString(id) {
			writeError(response, http.StatusBadRequest, "invalid_request", "event_ids must be ids of held messages.")
			return false
		}
	}
	return true
}

func (a *API) replayHeldChannelMessages(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	var input struct {
		EventIDs       []string `json:"event_ids"`
		ConversationID string   `json:"conversation_id"`
	}
	if !heldInput(response, request, &input, func() []string { return input.EventIDs }) {
		return
	}
	if input.ConversationID != "" && !uuidShape.MatchString(input.ConversationID) {
		writeError(response, http.StatusNotFound, "not_found", "No conversation of this workspace has that id.")
		return
	}
	results, err := a.pipeline.ReplayHeld(request.Context(), principal(request).Workspace.ID, id, input.EventIDs, input.ConversationID, channelActor(request))
	if errors.Is(err, pgx.ErrNoRows) && input.ConversationID != "" {
		writeError(response, http.StatusNotFound, "not_found", "No conversation of this workspace has that id.")
		return
	}
	if err != nil {
		writeChannelError(response, err)
		return
	}
	type resultBody struct {
		Status         string  `json:"status"`
		ConversationID *string `json:"conversation_id"`
		MessageID      *string `json:"message_id"`
	}
	items := make([]resultBody, 0, len(results))
	for _, result := range results {
		items = append(items, resultBody{Status: result.Status, ConversationID: result.ConversationID, MessageID: result.MessageID})
	}
	writeJSON(response, http.StatusOK, map[string]any{"results": items})
}

func (a *API) discardHeldChannelMessages(response http.ResponseWriter, request *http.Request) {
	id, ok := channelID(response, request)
	if !ok {
		return
	}
	var input struct {
		EventIDs []string `json:"event_ids"`
	}
	if !heldInput(response, request, &input, func() []string { return input.EventIDs }) {
		return
	}
	workspaceID := principal(request).Workspace.ID
	if _, err := a.store.GetChannelPublic(request.Context(), workspaceID, id); err != nil {
		writeChannelError(response, err)
		return
	}
	discarded, err := a.store.DiscardHeld(request.Context(), workspaceID, id, input.EventIDs, channelActor(request))
	if err != nil {
		writeChannelError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]int{"discarded": discarded})
}

func (a *API) listChannelEvents(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	channel := query.Get("channel_id")
	if channel != "" && !uuidShape.MatchString(channel) {
		writeError(response, http.StatusBadRequest, "invalid_request", "channel_id must be a channel id.")
		return
	}
	page, err := a.store.ListChannelEvents(request.Context(), principal(request).Workspace.ID, channel, query.Get("cursor"), channelEventsPageSize)
	if err != nil {
		writeChannelError(response, err)
		return
	}
	type actorBody struct {
		AgentID    *string `json:"agent_id"`
		Name       *string `json:"name"`
		TokenLabel *string `json:"token_label"`
	}
	type eventBody struct {
		ID          string    `json:"id"`
		At          time.Time `json:"at"`
		Kind        string    `json:"kind"`
		ChannelID   string    `json:"channel_id"`
		ChannelName string    `json:"channel_name"`
		ChannelType string    `json:"channel_type"`
		Actor       actorBody `json:"actor"`
		Fields      []string  `json:"fields"`
		Count       *int      `json:"count"`
	}
	items := make([]eventBody, 0, len(page.Items))
	for _, event := range page.Items {
		fields, count := eventFacts(event)
		items = append(items, eventBody{ID: event.ID, At: event.CreatedAt, Kind: event.Kind, ChannelID: event.ChannelID, ChannelName: event.ChannelName,
			ChannelType: event.ChannelType, Actor: actorBody{AgentID: event.ActorAgentID, Name: event.ActorName, TokenLabel: event.ActorTokenLabel},
			Fields: fields, Count: count})
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalText(page.Next)})
}

// eventFacts is what a history entry shows of its detail, never a value: the names of the settings that
// changed (or of the secret that was replaced), and how many held messages were opened or discarded.
func eventFacts(event store.ChannelEvent) ([]string, *int) {
	var detail map[string]any
	if len(event.Detail) == 0 || json.Unmarshal(event.Detail, &detail) != nil {
		return []string{}, nil
	}
	fields := []string{}
	var count *int
	switch event.Kind {
	case "config_changed":
		for name := range detail {
			fields = append(fields, name)
		}
		slices.Sort(fields)
	case "credential_changed":
		if name, ok := detail["name"].(string); ok {
			fields = append(fields, name)
		}
	case "held_replayed", "held_discarded":
		if n, ok := detail["count"].(float64); ok {
			value := int(n)
			count = &value
		}
	}
	return fields, count
}

// channelRouteKit is what a type's own route gets (channels.RouteKit).
type channelRouteKit struct {
	api  *API
	kind channels.Type
}

func (k channelRouteKit) Receive(ctx context.Context, channel channels.Channel, raws []channels.Raw) ([]channels.IngestResult, error) {
	if channel.Type != k.kind {
		return nil, channels.ErrNotFound
	}
	return k.api.pipeline.Receive(ctx, channel, raws)
}

func (k channelRouteKit) FindChannel(ctx context.Context, account string) (channels.Channel, bool, error) {
	return k.api.store.ChannelForExternalAccount(ctx, k.kind, account)
}

func (k channelRouteKit) WriteError(w http.ResponseWriter, status int, code, message string) {
	writeError(w, status, code, message)
}

func (k channelRouteKit) Channel(ctx context.Context, workspaceID, id string) (channels.Channel, bool, error) {
	if id == "" {
		channel, found, err := k.api.store.DefaultChannelOfType(ctx, workspaceID, k.kind)
		return channel, found && channel.Live(), err
	}
	if !uuidShape.MatchString(id) {
		return channels.Channel{}, false, nil
	}
	channel, err := k.api.store.GetChannel(ctx, workspaceID, id)
	if errors.Is(err, channels.ErrNotFound) {
		return channels.Channel{}, false, nil
	}
	if err != nil {
		return channels.Channel{}, false, err
	}
	return channel, channel.Type == k.kind && channel.Live(), nil
}

// channelHelpCenterRoutes are the help center routes the registered types declare, with their handlers;
// the help center mounts them below every address of an open help center.
func (a *API) channelHelpCenterRoutes() []helpcenter.ChannelRoute {
	var routes []helpcenter.ChannelRoute
	for _, definition := range a.store.Channels().Definitions() {
		for _, route := range definition.HelpCenterRoutes {
			routes = append(routes, helpcenter.ChannelRoute{Method: route.Method, Path: route.Path,
				Handler: route.Handler(channelRouteKit{api: a, kind: definition.Type})})
		}
	}
	return routes
}

// channelHelpCenterAdditions asks each registered type what it adds to the help center's pages now; the
// first type that adds a link or a box wins (one chat at a time).
func (a *API) channelHelpCenterAdditions() func(ctx context.Context, at channels.HelpCenter) (channels.HelpCenterAdditions, error) {
	var each []func(context.Context, channels.HelpCenter) (channels.HelpCenterAdditions, error)
	for _, definition := range a.store.Channels().Definitions() {
		if definition.HelpCenterAdditions != nil {
			each = append(each, definition.HelpCenterAdditions(channelRouteKit{api: a, kind: definition.Type}))
		}
	}
	if len(each) == 0 {
		return nil
	}
	return func(ctx context.Context, at channels.HelpCenter) (channels.HelpCenterAdditions, error) {
		for _, additions := range each {
			found, err := additions(ctx, at)
			if err != nil || found.Version != "" {
				return found, err
			}
		}
		return channels.HelpCenterAdditions{}, nil
	}
}

// channelTypeRoutes are the routes the registered types declare, mounted under /api/v1 with the
// permission each declares.
func (a *API) channelTypeRoutes() []routeSpec {
	var specs []routeSpec
	for _, definition := range a.store.Channels().Definitions() {
		for _, route := range definition.Routes {
			permission := auth.PermissionAccessWrite
			if route.Public {
				permission = auth.PermissionPublic
			}
			specs = append(specs, routeSpec{Route: Route{route.Method, "/api/v1" + route.Path, permission},
				handler: route.Handler(channelRouteKit{api: a, kind: definition.Type})})
		}
	}
	return specs
}
