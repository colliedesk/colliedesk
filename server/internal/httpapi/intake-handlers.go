package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/intake"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// The intake queue of chat channels (docs/user/channels.md "Intake queue"): what customers said in one
// go, decided by the channel's rules, AI or a person. Every route needs a session or a token of the
// workspace; reading what customers wrote needs messages:read.

const intakeDecideRoute = "/api/v1/intake/gatherings/{id}/decide"

func (a *API) intakeService() *intake.Service {
	return intake.New(a.store, a.pipeline, a.now)
}

func writeIntakeError(response http.ResponseWriter, err error) {
	var conflict *intake.ConflictError
	switch {
	case errors.As(err, &conflict):
		body := map[string]any{"code": "gathering_decided", "message": "This card was decided otherwise meanwhile; nothing changed.",
			"state": conflict.Outcome.State, "decided_by": nil, "decided_by_agent_id": nil, "conversation_id": nil}
		if conflict.Outcome.DecidedBy != "" {
			body["decided_by"] = conflict.Outcome.DecidedBy
		}
		if conflict.Outcome.DecidedByAgentID != "" {
			body["decided_by_agent_id"] = conflict.Outcome.DecidedByAgentID
		}
		if conflict.Outcome.ConversationID != "" {
			body["conversation_id"] = conflict.Outcome.ConversationID
		}
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, body)})
	case errors.Is(err, store.ErrGatheringNotFound):
		writeError(response, http.StatusNotFound, "not_found", "No intake card with that id in this workspace.")
	case errors.Is(err, store.ErrNotACandidate):
		writeError(response, http.StatusBadRequest, "invalid_request", "conversation_id is not one this card may join: it is of another place, customer or channel, or merged away. Nothing was decided.")
	case errors.Is(err, intake.ErrInvalid):
		writeError(response, http.StatusBadRequest, "invalid_request", "Send action create, attach with conversation_id, or ignore.")
	case errors.Is(err, pipeline.ErrNotAnOpenCommand):
		writeError(response, http.StatusBadRequest, "invalid_request", "command is one of the channel's commands that opens a request.")
	case errors.Is(err, store.ErrBadCursor):
		writeError(response, http.StatusBadRequest, "invalid_request", "The cursor is not one this server handed out.")
	default:
		writeStoreError(response, err)
	}
}

func (a *API) listIntakeGatherings(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	list := query.Get("state")
	if list == "" {
		list = store.IntakeListReview
	}
	switch list {
	case store.IntakeListReview, store.IntakeListDeciding, store.IntakeListIgnored, store.IntakeListDecided:
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "state is review, deciding, ignored or decided.")
		return
	}
	channel := query.Get("channel_id")
	if channel != "" && !uuidShape.MatchString(channel) {
		writeError(response, http.StatusBadRequest, "invalid_request", "channel_id is a channel's id.")
		return
	}
	scope := query.Get("scope")
	if scope != "" && !scopeShape.MatchString(scope) {
		writeError(response, http.StatusBadRequest, "invalid_request", "scope is a scope of the channel's type.")
		return
	}
	decidedBy := query.Get("decided_by")
	switch decidedBy {
	case "", store.DecidedByRules, store.DecidedByAI, store.DecidedByAgent:
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "decided_by is rules, ai or agent.")
		return
	}
	limit := 0
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > store.MaxIntakePage {
			writeError(response, http.StatusBadRequest, "invalid_request", "limit is a number from 1 to 50.")
			return
		}
		limit = value
	}
	page, err := a.store.IntakeGatherings(request.Context(), principal(request).Workspace.ID, list, channel, scope, decidedBy, query.Get("cursor"), limit)
	if err != nil {
		writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (a *API) getIntakeGathering(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if !uuidShape.MatchString(id) {
		writeIntakeError(response, store.ErrGatheringNotFound)
		return
	}
	detail, err := a.store.IntakeGatheringByID(request.Context(), principal(request).Workspace.ID, id, a.now())
	if err != nil {
		writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (a *API) decideIntakeGathering(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if !uuidShape.MatchString(id) {
		writeIntakeError(response, store.ErrGatheringNotFound)
		return
	}
	body, input, ok := readIntakeDecision(response, request)
	if !ok {
		return
	}
	who := principal(request)
	// Opening a conversation may tell the customer it was received: a token needs messages:send for it.
	needs := auth.PermConversationsUpdate
	if input.Action == intake.Create {
		needs = auth.PermMessagesSend
	}
	if who.IsToken() && !who.Allows(needs) {
		card, err := a.store.IntakeGatheringByID(request.Context(), who.Workspace.ID, id, a.now())
		if err != nil {
			writeIntakeError(response, err)
			return
		}
		a.fileAgentRequest(response, request, Route{Method: http.MethodPost, Pattern: intakeDecideRoute}, who, domain.AgentRequestAction, body,
			intakeSummary(card.IntakeGathering, input))
		return
	}
	actor := store.ChannelActor{AgentID: who.Agent.ID}
	if who.IsToken() {
		actor.TokenID = who.SessionID
	}
	a.decideOnce(response, request, decideIntakeOperation, []string{input.Action, input.ConversationID, input.Command, input.Subject, input.IgnoreReason}, func(ctx context.Context) error {
		_, err := a.intakeService().Decide(ctx, who.Workspace.ID, id, intake.Decision{Action: input.Action, ConversationID: input.ConversationID,
			Command: input.Command, Subject: input.Subject, IgnoreReason: input.IgnoreReason}, store.DecidedByAgent, actor)
		return err
	})
}

// decideOnce runs a decision on the card of the request's path under its Idempotency-Key and answers the
// card as it stands: a press sent again answers what the first one got and decides nothing more.
func (a *API) decideOnce(response http.ResponseWriter, request *http.Request, operation string, input []string, decide func(context.Context) error) {
	a.runOnce(response, request, http.StatusOK, operation, append([]string{request.PathValue("id")}, input...), func(ctx context.Context) (any, error) {
		if err := decide(ctx); err != nil {
			writeIntakeError(response, err)
			return nil, errAnswered
		}
		card, err := a.store.IntakeGatheringByID(ctx, principal(request).Workspace.ID, request.PathValue("id"), a.now())
		if err != nil {
			writeIntakeError(response, err)
			return nil, errAnswered
		}
		return card, nil
	})
}

type intakeDecisionInput struct {
	Action         string `json:"action"`
	ConversationID string `json:"conversation_id"`
	Command        string `json:"command"`
	Subject        string `json:"subject"`
	IgnoreReason   string `json:"ignore_reason"`
}

// ignoreReasons are why a person may say a card is set aside.
var ignoreReasons = []string{"chat", "handled_in_chat", "duplicate", "other"}

func readIntakeDecision(response http.ResponseWriter, request *http.Request) ([]byte, intakeDecisionInput, bool) {
	var input intakeDecisionInput
	body, err := readJSONBody(request, &input)
	switch {
	case err != nil:
	case input.Action != intake.Create && input.Action != intake.Attach && input.Action != intake.Ignore:
	case input.Action == intake.Attach && !uuidShape.MatchString(input.ConversationID):
	case input.Action != intake.Attach && input.ConversationID != "":
	case input.Action != intake.Create && (input.Command != "" || input.Subject != ""):
	case input.IgnoreReason != "" && (input.Action != intake.Ignore || !slices.Contains(ignoreReasons, input.IgnoreReason)):
	case len([]rune(input.Subject)) > 200 || len(input.Command) > 40:
	default:
		return body, input, true
	}
	writeError(response, http.StatusBadRequest, "invalid_request",
		"Send action: create (with an optional command and subject), attach with conversation_id, or ignore (with an optional ignore_reason: chat, handled_in_chat, duplicate or other).")
	return nil, input, false
}

func (a *API) requeueIntakeGathering(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if !uuidShape.MatchString(id) {
		writeIntakeError(response, store.ErrGatheringNotFound)
		return
	}
	who := principal(request)
	actor := store.ChannelActor{AgentID: who.Agent.ID}
	if who.IsToken() {
		actor.TokenID = who.SessionID
	}
	a.decideOnce(response, request, requeueIntakeOperation, nil, func(ctx context.Context) error {
		_, err := a.intakeService().Requeue(ctx, who.Workspace.ID, id, actor)
		return err
	})
}

func (a *API) getIntakeSummary(response http.ResponseWriter, request *http.Request) {
	summary, err := a.store.IntakeSummaryOf(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, summary)
}

func (a *API) getIntakeStats(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	channel := query.Get("channel_id")
	if channel != "" && !uuidShape.MatchString(channel) {
		writeError(response, http.StatusBadRequest, "invalid_request", "channel_id is a channel's id.")
		return
	}
	days := 7
	if raw := query.Get("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 90 {
			writeError(response, http.StatusBadRequest, "invalid_request", "days is a number from 1 to 90.")
			return
		}
		days = value
	}
	stats, err := a.store.IntakeStatsOf(request.Context(), principal(request).Workspace.ID, channel, days, a.now())
	if err != nil {
		writeIntakeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, stats)
}

func (a *API) getConversationIntake(response http.ResponseWriter, request *http.Request) {
	id := request.PathValue("id")
	if !uuidShape.MatchString(id) {
		writeError(response, http.StatusNotFound, "not_found", "Conversation not found.")
		return
	}
	found, err := a.store.ConversationIntakeOf(request.Context(), principal(request).Workspace.ID, id)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, found)
}

// scopeShape is a scope's name as types declare them.
var scopeShape = regexp.MustCompile(`^[a-z_]{1,40}$`)

// intakeSummary is what a person reads before approving a decision of a card. It names the card, not
// what the customer wrote: the agent that filed it reads the summary back, and a customer's words reach
// an agent only as untrusted content.
func intakeSummary(card store.IntakeGathering, input intakeDecisionInput) string {
	messages := fmt.Sprintf("%d messages", card.ItemCount)
	if card.ItemCount == 1 {
		messages = "1 message"
	}
	switch input.Action {
	case intake.Create:
		return "Open a conversation from intake card " + card.ID + " (" + messages + " from a customer)"
	case intake.Attach:
		return "Add intake card " + card.ID + " (" + messages + " from a customer) to conversation " + input.ConversationID
	}
	return "Set aside intake card " + card.ID + " (" + messages + " from a customer)"
}
