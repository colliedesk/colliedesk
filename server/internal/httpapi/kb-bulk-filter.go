package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbBulkFilterOnlyShapes are the filter keys that keep every article on their own: an order, or how the
// other keys combine. A bulk filter needs one more key, so a forgotten filter cannot change every article.
var kbBulkFilterOnlyShapes = map[string]bool{"sort": true, "tag_match": true, "include_subcollections": true}

// kbBulkFilterOf checks the filter of a bulk action and returns it with its query string, which names the
// request for its Idempotency-Key; false when it answered.
func kbBulkFilterOf(response http.ResponseWriter, who auth.Principal, raw json.RawMessage) (store.KBArticleFilter, string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var filters kbViewFilters
	if err := decoder.Decode(&filters); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"filter is the query of GET /api/v1/kb/articles as an object, with tag and author as lists; a key it does not know is refused.")
		return store.KBArticleFilter{}, "", false
	}
	if filters.CollectionID != "" && !isUUID(filters.CollectionID) {
		writeError(response, http.StatusBadRequest, "invalid_request", "collection_id is a collection id; GET /api/v1/kb/collections lists them.")
		return store.KBArticleFilter{}, "", false
	}
	query := filters.query()
	narrows := false
	for key := range query {
		narrows = narrows || !kbBulkFilterOnlyShapes[key]
	}
	if !narrows {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"filter needs at least one key that narrows the articles, such as collection_id, status or tag; send ids to change chosen articles.")
		return store.KBArticleFilter{}, "", false
	}
	filter, err := kbArticleFilter(query, who.Workspace.Timezone)
	if message, bad := isFilterError(err); bad {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return store.KBArticleFilter{}, "", false
	}
	filter.Limit = kbBulkMax
	return filter, query.Encode(), true
}

// kbBulkMatches is every live article the filter matches, read when the action runs, so a repeat with the
// same Idempotency-Key answers the first answer instead of the articles that match now. It writes its own
// refusals and returns errAnswered after them.
func (a *API) kbBulkMatches(ctx context.Context, response http.ResponseWriter, workspaceID string, filter store.KBArticleFilter) ([]string, error) {
	items, next, err := a.store.ListKBArticles(ctx, workspaceID, filter)
	if message, bad := isFilterError(err); bad {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return nil, errAnswered
	}
	if err != nil {
		writeKBError(response, err)
		return nil, errAnswered
	}
	if next != "" {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"The filter matches more than 500 articles. Narrow it (a collection, a tag, a status) and run the action on each part.")
		return nil, errAnswered
	}
	if len(items) == 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "No article matches the filter: nothing to change.")
		return nil, errAnswered
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids, nil
}
