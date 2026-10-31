package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (a *API) createConversation(response http.ResponseWriter, request *http.Request) {
	if mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type")); err == nil && mediaType == "multipart/form-data" {
		a.startConversationWithFiles(response, request)
		return
	}
	var input struct {
		ContactID string `json:"contact_id"`
		// Contact is a customer added with the conversation, instead of ContactID.
		Contact *store.NewContact `json:"contact"`
		Subject string            `json:"subject"`
		// Body, when sent, is the first reply: the conversation starts by emailing the contact.
		Body *string  `json:"body"`
		CC   []string `json:"cc"`
		// ChannelID is the channel the conversation starts from; empty for the default one.
		ChannelID string `json:"channel_id"`
	}
	if err := decodeJSON(request, &input); err != nil || (input.ContactID == "") == (input.Contact == nil) || strings.TrimSpace(input.Subject) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A subject and either contact_id or a new contact are required.")
		return
	}
	if input.Contact != nil {
		if problem := newContactProblem(input.Contact, input.Body != nil); problem != "" {
			writeError(response, http.StatusBadRequest, "invalid_request", problem)
			return
		}
	}
	identity := principal(request)
	if input.Body == nil && len(input.CC) > 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Only a conversation that starts with a reply (body) can copy other addresses.")
		return
	}
	if input.Body != nil && strings.TrimSpace(*input.Body) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "The first reply needs a body.")
		return
	}
	fingerprint := append([]string{input.ContactID, input.Subject, input.ChannelID}, input.CC...)
	if input.Contact != nil {
		fingerprint = append(fingerprint, "contact", input.Contact.Name, input.Contact.Email)
	}
	if input.Body != nil {
		fingerprint = append(fingerprint, "body", *input.Body)
	}
	// A repeat of one press (the same Idempotency-Key) answers the conversation the first one started and
	// emails nothing again.
	a.createOnce(response, request, createConversationOperation, fingerprint, func(ctx context.Context) (any, error) {
		if input.Body == nil {
			item, err := a.store.CreateConversation(ctx, identity.Workspace.ID, input.ContactID, input.Subject, input.ChannelID)
			if err != nil {
				writeStoreError(response, err)
				return nil, errAnswered
			}
			return item, nil
		}
		actor, _ := messageActor(ctx, identity, "outgoing")
		cc, refusal := a.cleanCC(ctx, identity.Workspace.ID, "outgoing", input.CC)
		if refusal != nil {
			a.writeUploadError(response, refusal)
			return nil, errAnswered
		}
		item, err := a.store.StartConversation(ctx, identity.Workspace.ID,
			store.NewConversation{ContactID: input.ContactID, NewContact: input.Contact, Subject: input.Subject, ChannelID: input.ChannelID}, actor, *input.Body, cc)
		if err != nil {
			writeStoreError(response, err)
			return nil, errAnswered
		}
		a.recordReply(identity, "outgoing")
		return item, nil
	})
}

// newContactProblem checks a customer added with a conversation, trimming it in place, and says what is
// wrong. A customer is added only with a first message: an empty conversation needs a contact already.
func newContactProblem(contact *store.NewContact, withBody bool) string {
	contact.Name, contact.Email = strings.TrimSpace(contact.Name), strings.TrimSpace(contact.Email)
	switch {
	case !withBody:
		return "A new customer is added with the first message (body); add the contact first to open an empty conversation."
	case contact.Name == "" || utf8.RuneCountInString(contact.Name) > maxContactName:
		return fmt.Sprintf("The new customer needs a name of 1 to %d characters.", maxContactName)
	case !plainAddress(contact.Email):
		return "The new customer needs a plain email address such as name@example.com."
	}
	return ""
}

func (a *API) getConversation(response http.ResponseWriter, request *http.Request) {
	item, err := a.store.GetConversation(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (a *API) listConversations(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	page := positiveInt(query.Get("page"), 1)
	pageSize := positiveInt(query.Get("page_size"), 50)
	if pageSize > 100 {
		pageSize = 100
	}
	filter, err := conversationFilterFromQuery(query, a.conversationChannels())
	if err == nil {
		err = resultFilterFromQuery(query, &filter)
	}
	if message, invalid := isFilterError(err); invalid {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return
	}
	identity := principal(request)
	filter.AgentID, filter.Page, filter.PageSize = identity.Agent.ID, page, pageSize
	items, err := a.store.ListConversations(request.Context(), identity.Workspace.ID, filter)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, items)
}

func (a *API) updateConversation(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Status     *string         `json:"status"`
		Priority   *string         `json:"priority"`
		AssigneeID json.RawMessage `json:"assignee_id"`
		TagIDs     *[]string       `json:"tag_ids"`
		Notify     *bool           `json:"notify"`
	}
	raw, err := readBody(request)
	if err != nil || decodeJSON(request, &input) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Conversation changes are invalid.")
		return
	}
	var assignee **string
	if len(input.AssigneeID) > 0 {
		var value *string
		if string(input.AssigneeID) != "null" && json.Unmarshal(input.AssigneeID, &value) != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "Assignee is invalid.")
			return
		}
		assignee = &value
	}
	identity := principal(request)
	quiet := input.Notify != nil && !*input.Notify
	if a.statusNeedsApproval(response, request, Route{Method: http.MethodPatch, Pattern: "/api/v1/conversations/{id}"}, identity, raw,
		[]string{request.PathValue("id")}, input.Status, quiet) {
		return
	}
	item, err := a.store.UpdateConversation(request.Context(), identity.Workspace.ID, request.PathValue("id"), store.ConversationUpdate{
		Status: input.Status, Priority: input.Priority, AssigneeID: assignee, TagIDs: input.TagIDs, ActorID: identity.Agent.ID, TokenID: tokenOf(request), Quiet: quiet,
	})
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

// statusNeedsApproval files the call as an agent request when it is a token's that may not write to
// customers and the status it sets would tell one (the channel's status notice): it answers 202 and says
// so. A quiet change (notify false) never asks.
func (a *API) statusNeedsApproval(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal, raw json.RawMessage,
	ids []string, status *string, quiet bool) bool {
	if status == nil || quiet || !who.IsToken() || who.Allows(auth.PermMessagesSend) {
		return false
	}
	notices, err := a.store.StatusNoticePreview(request.Context(), who.Workspace.ID, ids, *status)
	if err != nil {
		writeStoreError(response, err)
		return true
	}
	if len(notices) == 0 {
		return false
	}
	a.fileAgentRequest(response, request, route, who, domain.AgentRequestAction, raw, statusRequestSummary(ids, *status, notices))
	return true
}

// statusRequestSummary says what a status change that waits for a person does, as the approval card
// reads it: "Set REF-1 to Resolved and tell the customer on Zalo: REF-1: fixed.", or for several
// "Set 3 conversations to Resolved and tell their customers on Zalo, Web chat".
func statusRequestSummary(ids []string, status string, notices []domain.StatusNoticePreview) string {
	label := strings.ToUpper(status[:1]) + status[1:]
	if len(ids) == 1 {
		return fmt.Sprintf("Set %s to %s and tell the customer on %s: %s", notices[0].ExampleRef, label, notices[0].ChannelName, notices[0].Text)
	}
	names := make([]string, len(notices))
	for index, notice := range notices {
		names[index] = notice.ChannelName
	}
	return fmt.Sprintf("Set %d conversations to %s and tell their customers on %s", len(ids), label, strings.Join(names, ", "))
}

// tokenOf is the id of the AI agent's token making a call, or "" for a person's own: a token's request a
// person approved is still the token's, made with their approval.
func tokenOf(request *http.Request) string {
	if replay, ok := approvalOf(request.Context()); ok {
		return replay.tokenID
	}
	if who := principal(request); who.IsToken() {
		return who.SessionID
	}
	return ""
}

// readBody reads the JSON body once and puts it back for decodeJSON, so a call can be filed as it came.
func readBody(request *http.Request) (json.RawMessage, error) {
	raw, err := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, nil
}
