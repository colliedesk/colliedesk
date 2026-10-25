package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// conversationFilters is the ConversationFilters object of the contract: the query of GET
// /conversations with lists for the comma-separated values. JSON types are strict, so "status":
// "open" (not a list) is refused like an unknown value.
type conversationFilters struct {
	Status          []string `json:"status,omitempty"`
	Priority        []string `json:"priority,omitempty"`
	Assignee        []string `json:"assignee,omitempty"`
	Tag             []string `json:"tag,omitempty"`
	TagMatch        string   `json:"tag_match,omitempty"`
	Contact         string   `json:"contact,omitempty"`
	Organization    string   `json:"organization,omitempty"`
	Awaiting        string   `json:"awaiting,omitempty"`
	HasPendingDraft *bool    `json:"has_pending_draft,omitempty"`
	DeliveryFailed  *bool    `json:"delivery_failed,omitempty"`
	Source          string   `json:"source,omitempty"`
	Channel         []string `json:"channel,omitempty"`
	ChannelID       []string `json:"channel_id,omitempty"`
	CreatedAfter    string   `json:"created_after,omitempty"`
	CreatedBefore   string   `json:"created_before,omitempty"`
	UpdatedAfter    string   `json:"updated_after,omitempty"`
	UpdatedBefore   string   `json:"updated_before,omitempty"`
	Sort            string   `json:"sort,omitempty"`
	Search          string   `json:"search,omitempty"`
}

// query is the same filters as GET /conversations takes them.
func (f conversationFilters) query() url.Values {
	query := url.Values{}
	lists := map[string][]string{"status": f.Status, "priority": f.Priority, "assignee": f.Assignee, "tag": f.Tag, "channel": f.Channel, "channel_id": f.ChannelID}
	for key, values := range lists {
		if len(values) > 0 {
			query.Set(key, strings.Join(values, ","))
		}
	}
	texts := map[string]string{"tag_match": f.TagMatch, "contact": f.Contact, "organization": f.Organization, "awaiting": f.Awaiting,
		"source": f.Source, "created_after": f.CreatedAfter, "created_before": f.CreatedBefore, "updated_after": f.UpdatedAfter,
		"updated_before": f.UpdatedBefore, "sort": f.Sort, "search": f.Search}
	for key, value := range texts {
		if value != "" {
			query.Set(key, value)
		}
	}
	for key, value := range map[string]*bool{"has_pending_draft": f.HasPendingDraft, "delivery_failed": f.DeliveryFailed} {
		if value != nil {
			query.Set(key, strconv.FormatBool(*value))
		}
	}
	return query
}

// checkedFilters decodes the filters of a saved view and holds them to what GET /conversations
// accepts, so a saved view always opens. It returns them re-encoded without empty fields.
func checkedFilters(raw json.RawMessage, channels []string) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, filterError("A saved view needs filters: an object, {} for every conversation.")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var filters conversationFilters
	if err := decoder.Decode(&filters); err != nil {
		return nil, filterError("The filters of a saved view are the query of GET /api/v1/conversations as an object, with lists for several values.")
	}
	if _, err := conversationFilterFromQuery(filters.query(), channels); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(filters)
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func checkedViewName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > domain.MaxSavedViewName {
		return "", filterError("A saved view needs a name of 1 to 80 characters.")
	}
	return name, nil
}

func checkedViewScope(scope string) error {
	if scope != domain.SavedViewPersonal && scope != domain.SavedViewShared {
		return filterError("scope must be personal or shared.")
	}
	return nil
}

func writeSavedViewError(response http.ResponseWriter, err error) {
	if message, invalid := isFilterError(err); invalid {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return
	}
	switch {
	case errors.Is(err, domain.ErrSavedViewNameTaken):
		writeError(response, http.StatusConflict, "duplicate_name", "A view with this name already exists. Names are compared ignoring case, among your personal views and among the shared ones; pick another name.")
	case errors.Is(err, domain.ErrSavedViewLimit):
		writeError(response, http.StatusConflict, "saved_view_limit", "There are 50 views here already: each person keeps at most 50 personal views and a workspace 50 shared ones. Delete one first.")
	case errors.Is(err, domain.ErrSavedViewChanged):
		writeError(response, http.StatusConflict, "changed_elsewhere", "Someone changed this view after you read it. Nothing was changed; read it again, then make your change.")
	case errors.Is(err, domain.ErrSavedViewNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The saved view was not found.")
	default:
		writeStoreError(response, err)
	}
}

// savedViewID reads the id of the path; anything that is not an id is a view that does not exist.
func savedViewID(response http.ResponseWriter, request *http.Request) (string, bool) {
	id := strings.ToLower(request.PathValue("id"))
	if !contactIDPattern.MatchString(id) {
		writeSavedViewError(response, domain.ErrSavedViewNotFound)
		return "", false
	}
	return id, true
}

func (a *API) listSavedViews(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	items, err := a.store.ListSavedViews(request.Context(), who.Workspace.ID, who.Agent.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listDeletedSavedViews(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	items, err := a.store.ListDeletedSavedViews(request.Context(), who.Workspace.ID, who.Agent.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createSavedView(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name    string          `json:"name"`
		Scope   string          `json:"scope"`
		Filters json.RawMessage `json:"filters"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A saved view needs a name, a scope (personal or shared) and filters.")
		return
	}
	name, err := checkedViewName(input.Name)
	if err == nil {
		err = checkedViewScope(input.Scope)
	}
	var filters json.RawMessage
	if err == nil {
		filters, err = checkedFilters(input.Filters, a.conversationChannels())
	}
	if err != nil {
		writeSavedViewError(response, err)
		return
	}
	who := principal(request)
	view, err := a.store.CreateSavedView(request.Context(), who.Workspace.ID, who.Agent.ID, name, input.Scope, filters)
	if err != nil {
		writeSavedViewError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, view)
}

func (a *API) updateSavedView(response http.ResponseWriter, request *http.Request) {
	id, ok := savedViewID(response, request)
	if !ok {
		return
	}
	var input struct {
		UpdatedAt *time.Time      `json:"updated_at"`
		Name      *string         `json:"name"`
		Scope     *string         `json:"scope"`
		Filters   json.RawMessage `json:"filters"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil || (input.Name == nil && input.Scope == nil && input.Filters == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new name, scope or filters.")
		return
	}
	change := store.SavedViewChange{Seen: *input.UpdatedAt}
	var err error
	if input.Name != nil {
		var name string
		if name, err = checkedViewName(*input.Name); err == nil {
			change.Name = &name
		}
	}
	if err == nil && input.Scope != nil {
		if err = checkedViewScope(*input.Scope); err == nil {
			change.Scope = input.Scope
		}
	}
	if err == nil && input.Filters != nil {
		change.Filters, err = checkedFilters(input.Filters, a.conversationChannels())
	}
	if err != nil {
		writeSavedViewError(response, err)
		return
	}
	who := principal(request)
	view, err := a.store.UpdateSavedView(request.Context(), who.Workspace.ID, who.Agent.ID, id, change)
	if err != nil {
		writeSavedViewError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (a *API) deleteSavedView(response http.ResponseWriter, request *http.Request) {
	id, ok := savedViewID(response, request)
	if !ok {
		return
	}
	who := principal(request)
	view, err := a.store.DeleteSavedView(request.Context(), who.Workspace.ID, who.Agent.ID, id)
	if err != nil {
		writeSavedViewError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func (a *API) restoreSavedView(response http.ResponseWriter, request *http.Request) {
	id, ok := savedViewID(response, request)
	if !ok {
		return
	}
	who := principal(request)
	view, err := a.store.RestoreSavedView(request.Context(), who.Workspace.ID, who.Agent.ID, id)
	if err != nil {
		writeSavedViewError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, view)
}
