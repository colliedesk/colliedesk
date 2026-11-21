package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// kbViewFilters is the KBArticleFilters object of the contract: the query of GET /kb/articles with
// lists for tag (repeated in the query) and author (comma-separated). JSON types are strict, so
// "status": ["draft"] is refused like an unknown value.
type kbViewFilters struct {
	Q                     string   `json:"q,omitempty"`
	Status                string   `json:"status,omitempty"`
	CollectionID          string   `json:"collection_id,omitempty"`
	IncludeSubcollections *bool    `json:"include_subcollections,omitempty"`
	Author                []string `json:"author,omitempty"`
	Tag                   []string `json:"tag,omitempty"`
	TagMatch              string   `json:"tag_match,omitempty"`
	CreatedAfter          string   `json:"created_after,omitempty"`
	CreatedBefore         string   `json:"created_before,omitempty"`
	UpdatedAfter          string   `json:"updated_after,omitempty"`
	UpdatedBefore         string   `json:"updated_before,omitempty"`
	Translation           string   `json:"translation,omitempty"`
	Locale                string   `json:"locale,omitempty"`
	HasDraft              *bool    `json:"has_draft,omitempty"`
	Visibility            string   `json:"visibility,omitempty"`
	State                 string   `json:"state,omitempty"`
	Sort                  string   `json:"sort,omitempty"`
}

// query is the same filters as GET /kb/articles takes them.
func (f kbViewFilters) query() url.Values {
	query := url.Values{}
	texts := map[string]string{"q": f.Q, "status": f.Status, "collection_id": f.CollectionID, "tag_match": f.TagMatch,
		"created_after": f.CreatedAfter, "created_before": f.CreatedBefore, "updated_after": f.UpdatedAfter, "updated_before": f.UpdatedBefore,
		"translation": f.Translation, "locale": f.Locale, "visibility": f.Visibility, "state": f.State, "sort": f.Sort}
	for key, value := range texts {
		if value != "" {
			query.Set(key, value)
		}
	}
	for key, value := range map[string]*bool{"include_subcollections": f.IncludeSubcollections, "has_draft": f.HasDraft} {
		if value != nil {
			query.Set(key, strconv.FormatBool(*value))
		}
	}
	if len(f.Author) > 0 {
		query.Set("author", strings.Join(f.Author, ","))
	}
	for _, tag := range f.Tag {
		query.Add("tag", tag)
	}
	return query
}

// checkedKBFilters decodes the filters of a knowledge base view and holds them to what GET
// /kb/articles accepts in this workspace, a locale included, so the view always opens. It returns them
// re-encoded without empty fields and with the tags cleaned as an article's are.
func (a *API) checkedKBFilters(ctx context.Context, workspaceID string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, filterError("A saved view needs filters: an object, {} for every article.")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var filters kbViewFilters
	if err := decoder.Decode(&filters); err != nil {
		return nil, filterError("The filters of a knowledge base view are the query of GET /api/v1/kb/articles as an object, with tag and author as lists.")
	}
	if filters.CollectionID != "" && !isUUID(filters.CollectionID) {
		return nil, filterError("collection_id is a collection id; GET /api/v1/kb/collections lists them.")
	}
	// The workspace's timezone only places a bare date when the view opens; any zone checks it.
	parsed, err := kbArticleFilter(filters.query(), "UTC")
	if err != nil {
		return nil, err
	}
	if err := a.store.CheckKBFilterLocale(ctx, workspaceID, parsed.Locale); err != nil {
		return nil, err
	}
	filters.Tag = parsed.Tags
	return json.Marshal(filters)
}

// checkedViewArea reads the area of a saved view; none is the inbox.
func checkedViewArea(area string) (string, error) {
	switch area {
	case "":
		return domain.SavedViewInbox, nil
	case domain.SavedViewInbox, domain.SavedViewKB:
		return area, nil
	}
	return "", filterError("area is inbox (conversation filters) or kb (knowledge base article filters).")
}

// checkedViewFilters holds the filters of a view to what its area's list accepts in the workspace.
func (a *API) checkedViewFilters(ctx context.Context, workspaceID, area string, raw json.RawMessage) (json.RawMessage, error) {
	if area == domain.SavedViewKB {
		return a.checkedKBFilters(ctx, workspaceID, raw)
	}
	return checkedFilters(raw, a.conversationChannels())
}
