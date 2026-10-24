package httpapi

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

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
	if input.Body == nil {
		if len(input.CC) > 0 {
			writeError(response, http.StatusBadRequest, "invalid_request", "Only a conversation that starts with a reply (body) can copy other addresses.")
			return
		}
		item, err := a.store.CreateConversation(request.Context(), identity.Workspace.ID, input.ContactID, input.Subject)
		if err != nil {
			writeStoreError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, item)
		return
	}
	if strings.TrimSpace(*input.Body) == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "The first reply needs a body.")
		return
	}
	actor, _ := messageActor(request.Context(), identity, "outgoing")
	cc, refusal := a.cleanCC(request.Context(), identity.Workspace.ID, "outgoing", input.CC)
	if refusal != nil {
		a.writeUploadError(response, refusal)
		return
	}
	item, err := a.store.StartConversation(request.Context(), identity.Workspace.ID,
		store.NewConversation{ContactID: input.ContactID, NewContact: input.Contact, Subject: input.Subject}, actor, *input.Body, cc)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	a.recordReply(identity, "outgoing")
	writeJSON(response, http.StatusCreated, item)
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
	filter, err := conversationFilterFromQuery(query)
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
	}
	if err := decodeJSON(request, &input); err != nil {
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
	item, err := a.store.UpdateConversation(request.Context(), identity.Workspace.ID, request.PathValue("id"), store.ConversationUpdate{
		Status: input.Status, Priority: input.Priority, AssigneeID: assignee, TagIDs: input.TagIDs, ActorID: identity.Agent.ID,
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
