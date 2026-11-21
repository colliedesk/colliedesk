package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbBulkMax is the most articles one bulk action changes, by ids or by filter.
const kbBulkMax = 500

// kbBulkPermission is the permission each bulk action needs before any article is read; an article
// can need more (publish, to change what the help center shows), checked on its own.
var kbBulkPermission = map[string]string{
	"move": auth.PermKBArticlesUpdate, "visibility": auth.PermKBArticlesUpdate, "author": auth.PermKBArticlesUpdate,
	"tags_add": auth.PermKBArticlesUpdate, "tags_remove": auth.PermKBArticlesUpdate, "send_for_review": auth.PermKBArticlesUpdate,
	"approve": auth.PermKBArticlesPublish, "publish": auth.PermKBArticlesPublish, "unpublish": auth.PermKBArticlesPublish,
	"trash": auth.PermKBArticlesDelete,
}

type kbBulkInput struct {
	Action        string          `json:"action"`
	IDs           []string        `json:"ids,omitempty"`
	Filter        json.RawMessage `json:"filter,omitempty"`
	CollectionID  string          `json:"collection_id,omitempty"`
	Visibility    string          `json:"visibility,omitempty"`
	AuthorAgentID string          `json:"author_agent_id,omitempty"`
	Tags          []string        `json:"tags,omitempty"`
	ReviewerIDs   []string        `json:"reviewer_ids,omitempty"`
	Note          string          `json:"note,omitempty"`
}

// kbBulkItem is one article's line in the answer: done, unchanged (it already was so) or refused with
// the error the same call for it alone gives. An article this workspace does not have carries no title.
type kbBulkItem struct {
	ID       string             `json:"id"`
	Number   *int64             `json:"number,omitempty"`
	Title    string             `json:"title,omitempty"`
	Outcome  string             `json:"outcome"`
	Undoable bool               `json:"undoable"`
	Error    *kbBulkResultError `json:"error,omitempty"`
}

// bulkKBArticles applies one action to up to 500 articles, named by ids or by the filter of the list.
func (a *API) bulkKBArticles(response http.ResponseWriter, request *http.Request) {
	var input kbBulkInput
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send action, ids (up to 500) or filter, and what the action needs.")
		return
	}
	permission, known := kbBulkPermission[input.Action]
	if !known {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"action is move, visibility, author, tags_add, tags_remove, send_for_review, approve, publish, unpublish or trash.")
		return
	}
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, permission, nil) {
		return
	}
	if problem := input.check(); problem != "" {
		writeError(response, http.StatusBadRequest, "invalid_request", problem)
		return
	}
	ids, ok := a.kbBulkTargets(response, request, who, input)
	if !ok {
		return
	}
	params := input
	params.IDs, params.Filter = nil, nil
	key, _ := json.Marshal(params)
	a.runOnce(response, request, http.StatusOK, kbArticlesBulkOperation, append([]string{string(key)}, ids...), func(ctx context.Context) (any, error) {
		ctx = context.WithoutCancel(ctx)
		results := make([]kbBulkItem, 0, len(ids))
		entries := []store.KBBulkEntry{}
		counts := map[string]int{"done": 0, "unchanged": 0, "refused": 0}
		for _, id := range ids {
			item, entry := a.kbBulkOne(ctx, request, who, input, id)
			if entry != nil {
				entries = append(entries, *entry)
				item.Undoable = true
			}
			counts[item.Outcome]++
			results = append(results, item)
		}
		answer := map[string]any{"operation": nil, "counts": counts, "results": results}
		if len(entries) > 0 {
			operation, ran, err := a.store.RecordKBBulkOperation(ctx, who.Workspace.ID, savedReplyActor(ctx, who), input.Action, key, entries)
			if err != nil {
				return nil, err
			}
			answer["operation"] = map[string]any{"id": operation, "action": input.Action, "undo_until": ran.Add(domain.KBBulkUndoWindow).UTC().Format(time.RFC3339)}
		}
		return answer, nil
	})
}

// check reports what is wrong with the action's own fields, cleaning tags; "" when nothing is.
func (input *kbBulkInput) check() string {
	switch input.Action {
	case "move":
		if !isUUID(input.CollectionID) {
			return "A move needs collection_id: the collection to move the articles to; GET /api/v1/kb/collections lists them."
		}
	case "visibility":
		if domain.CheckKBVisibility(input.Visibility) != nil {
			return "visibility is everyone, help_chat or agents: who may read the articles."
		}
	case "author":
		if !isUUID(input.AuthorAgentID) {
			return "author_agent_id is the team member to credit the articles to; GET /api/v1/agents lists them."
		}
	case "tags_add", "tags_remove":
		tags, err := domain.CleanKBTags(input.Tags)
		if err != nil || len(tags) == 0 {
			return "tags are the tags to add or remove: at least one."
		}
		input.Tags = tags
	case "send_for_review":
		if len(input.ReviewerIDs) == 0 {
			return "send_for_review needs reviewer_ids: teammates who may publish; GET /api/v1/agents lists them."
		}
	}
	if (len(input.IDs) == 0) == (len(input.Filter) == 0) {
		return "Send either ids (1 to 500 articles) or filter (the query of GET /api/v1/kb/articles as an object, {} for every article), not both."
	}
	sorted := slices.Clone(input.IDs)
	slices.Sort(sorted)
	if len(input.IDs) > kbBulkMax || len(slices.Compact(sorted)) != len(input.IDs) {
		return "ids holds 1 to 500 different article ids."
	}
	return ""
}

// kbBulkTargets is the ids sent, or every live article the filter matches; false when it answered.
func (a *API) kbBulkTargets(response http.ResponseWriter, request *http.Request, who auth.Principal, input kbBulkInput) ([]string, bool) {
	if len(input.IDs) > 0 {
		return input.IDs, true
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Filter))
	decoder.DisallowUnknownFields()
	var filters kbViewFilters
	if err := decoder.Decode(&filters); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"filter is the query of GET /api/v1/kb/articles as an object, with tag and author as lists; a key it does not know is refused.")
		return nil, false
	}
	if filters.CollectionID != "" && !isUUID(filters.CollectionID) {
		writeError(response, http.StatusBadRequest, "invalid_request", "collection_id is a collection id; GET /api/v1/kb/collections lists them.")
		return nil, false
	}
	filter, err := kbArticleFilter(filters.query(), who.Workspace.Timezone)
	if message, bad := isFilterError(err); bad {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return nil, false
	}
	filter.Limit = kbBulkMax
	items, next, err := a.store.ListKBArticles(request.Context(), who.Workspace.ID, filter)
	if message, bad := isFilterError(err); bad {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return nil, false
	}
	if err != nil {
		writeKBError(response, err)
		return nil, false
	}
	if next != "" {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"The filter matches more than 500 articles. Narrow it (a collection, a tag, a status) and run the action on each part.")
		return nil, false
	}
	if len(items) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "No article matches the filter: nothing to change.")
		return nil, false
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids, true
}

// kbBulkPermissionError is an article that needs a permission the caller lacks, such as publish to take
// a published article off the help center.
type kbBulkPermissionError struct{ permission string }

func (e kbBulkPermissionError) Error() string { return "missing " + e.permission }

// kbBulkError is the code and message the call for this article alone would answer err with.
func (a *API) kbBulkError(request *http.Request, who auth.Principal, err error) *kbBulkResultError {
	if missing, ok := err.(kbBulkPermissionError); ok {
		writer := &itemErrorWriter{header: http.Header{}}
		a.writePermissionMissing(writer, request, who, missing.permission)
		return parsedItemError(writer)
	}
	if refusal, ok := err.(kbBulkRefusal); ok {
		return &kbBulkResultError{Code: refusal.code, Message: refusal.message}
	}
	return kbItemError(err)
}
