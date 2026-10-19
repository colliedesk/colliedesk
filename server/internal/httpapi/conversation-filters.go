package httpapi

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// conversationFilterKeys are the query parameters of GET /conversations that narrow or order the list;
// a saved view holds exactly these (ConversationFilters in the contract).
var conversationFilterKeys = []string{"status", "priority", "assignee", "tag", "tag_match", "contact", "organization", "awaiting",
	"has_pending_draft", "delivery_failed", "source", "channel", "created_after", "created_before", "updated_after", "updated_before", "sort", "search"}

var (
	conversationStatuses   = []string{"open", "pending", "resolved", "closed"}
	conversationPriorities = []string{"low", "normal", "high", "urgent"}
	conversationChannels   = []string{"email", "web_form", "imported", "agent", "problem_report"}
	conversationSorts      = []string{store.ConversationSortUpdatedDesc, store.ConversationSortUpdatedAsc, store.ConversationSortCreatedDesc,
		store.ConversationSortCreatedAsc, store.ConversationSortPriority, store.ConversationSortWaitingLongest}
)

const (
	maxFilterIDs       = 50
	maxFilterTextRunes = 200
)

// errConversationFilter carries the sentence a 400 answers with.
type errConversationFilter struct{ message string }

func (e errConversationFilter) Error() string { return e.message }

func filterError(message string) error { return errConversationFilter{message} }

// conversationFilterFromQuery reads the filters of GET /conversations. A value the server does not
// know is an error, so a typo never looks like an empty inbox.
func conversationFilterFromQuery(query url.Values) (store.ConversationFilter, error) {
	var filter store.ConversationFilter
	var err error
	list := func(key string, allowed []string) []string {
		// An empty value is no filter, as it always was (?status= lists every status).
		if err != nil || query.Get(key) == "" {
			return nil
		}
		var values []string
		values, err = commaList(key, query.Get(key), func(value string) bool { return slices.Contains(allowed, value) })
		return values
	}
	filter.Statuses = list("status", conversationStatuses)
	filter.Priorities = list("priority", conversationPriorities)
	filter.Channels = list("channel", conversationChannels)
	if err == nil && query.Get("assignee") != "" {
		filter.Assignees, err = commaList("assignee", strings.ToLower(query.Get("assignee")), func(value string) bool {
			return value == "me" || value == "unassigned" || value == "anyone" || contactIDPattern.MatchString(value)
		})
	}
	if err == nil && query.Get("tag") != "" {
		filter.TagIDs, err = commaList("tag", strings.ToLower(query.Get("tag")), contactIDPattern.MatchString)
	}
	if err != nil {
		return filter, err
	}
	one := func(key string, allowed ...string) (string, error) {
		value := query.Get(key)
		if value == "" || slices.Contains(allowed, value) {
			return value, nil
		}
		return "", filterError(key + " must be one of " + strings.Join(allowed, ", ") + ".")
	}
	var match string
	if match, err = one("tag_match", "any", "all"); err != nil {
		return filter, err
	}
	filter.TagMatchAll = match == "all"
	if filter.Awaiting, err = one("awaiting", "team", "customer"); err != nil {
		return filter, err
	}
	if filter.Source, err = one("source", "imported", "native"); err != nil {
		return filter, err
	}
	if filter.Sort, err = one("sort", conversationSorts...); err != nil {
		return filter, err
	}
	if contact := strings.ToLower(query.Get("contact")); contact != "" {
		if !contactIDPattern.MatchString(contact) {
			return filter, filterError("contact must be a contact id.")
		}
		filter.ContactID = contact
	}
	for key, target := range map[string]*string{"organization": &filter.Organization, "search": &filter.Search} {
		value := query.Get(key)
		if utf8.RuneCountInString(value) > maxFilterTextRunes {
			return filter, filterError(key + " must be at most 200 characters.")
		}
		*target = value
	}
	for key, target := range map[string]**bool{"has_pending_draft": &filter.HasPendingDraft, "delivery_failed": &filter.DeliveryFailed} {
		switch query.Get(key) {
		case "":
		case "true", "false":
			value := query.Get(key) == "true"
			*target = &value
		default:
			return filter, filterError(key + " must be true or false.")
		}
	}
	for key, target := range map[string]**time.Time{"created_after": &filter.CreatedAfter, "created_before": &filter.CreatedBefore,
		"updated_after": &filter.UpdatedAfter, "updated_before": &filter.UpdatedBefore} {
		value := query.Get(key)
		if value == "" {
			continue
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil {
			return filter, filterError(key + " must be an RFC 3339 time with an offset, for example 2026-10-01T00:00:00Z.")
		}
		*target = &parsed
	}
	return filter, nil
}

// commaList splits a comma-separated value, refusing empty items, unknown values and more than 50.
func commaList(key, raw string, valid func(string) bool) ([]string, error) {
	values := []string{}
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" || !valid(value) {
			return nil, filterError(key + " has a value the server does not know: " + strconv.Quote(value) + ".")
		}
		if !slices.Contains(values, value) {
			values = append(values, value)
		}
	}
	if len(values) > maxFilterIDs {
		return nil, filterError(key + " takes at most 50 values.")
	}
	return values, nil
}

func isFilterError(err error) (string, bool) {
	var target errConversationFilter
	if errors.As(err, &target) {
		return target.message, true
	}
	return "", false
}
