package httpapi

import (
	"encoding/csv"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

const (
	// maxKBExportRows bounds one CSV file of articles; the filters narrow a larger knowledge base.
	maxKBExportRows = 50_000
	kbExportPage    = 500
)

var kbExportColumns = []string{"title", "address", "collection", "status", "author", "tags", "languages",
	"created_at", "updated_at", "helpful_votes", "not_helpful_votes"}

// exportKBArticles sends the articles listKBArticles lists with the same filters, in its order, as one
// CSV file streamed a page at a time. A refused filter is a 400 before the file starts; a failure after
// it started is logged and aborts the response, so the download fails instead of ending in a short file
// that reads as the whole list.
func (a *API) exportKBArticles(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	filter, err := kbArticleFilter(request.URL.Query(), who.Workspace.Timezone)
	if message, invalid := isFilterError(err); invalid {
		writeError(response, http.StatusBadRequest, "invalid_request", message)
		return
	}
	filter.Limit = kbExportPage
	items, next, err := a.store.ListKBArticles(request.Context(), who.Workspace.ID, filter)
	if err != nil {
		writeKBError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	collections, err := a.store.ListKBCollections(request.Context(), who.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	rows := kbExportRows{home: home, paths: kbCollectionPaths(collections), location: agentLocation(who.Workspace.Timezone, who.Agent.TimezoneOverride)}

	response.Header().Set("Content-Type", "text/csv; charset=utf-8")
	response.Header().Set("Content-Disposition", `attachment; filename="knowledge-base-articles.csv"`)
	_, _ = response.Write([]byte("\xEF\xBB\xBF"))
	writer := csv.NewWriter(response)
	_ = writer.Write(kbExportColumns)
	written := 0
	for {
		items = items[:min(len(items), maxKBExportRows-written)]
		if err := a.writeKBExportPage(response, request, writer, rows, items); err != nil {
			slog.Error("knowledge base export: write a page", "error", err, "workspace", who.Workspace.ID)
			panic(http.ErrAbortHandler)
		}
		written += len(items)
		if next == "" || written >= maxKBExportRows {
			return
		}
		filter.Cursor = next
		if items, next, err = a.store.ListKBArticles(request.Context(), who.Workspace.ID, filter); err != nil {
			slog.Error("knowledge base export: read a page", "error", err, "workspace", who.Workspace.ID)
			panic(http.ErrAbortHandler)
		}
	}
}

// writeKBExportPage writes one page of articles with their vote counts and sends it at once.
func (a *API) writeKBExportPage(response http.ResponseWriter, request *http.Request, writer *csv.Writer, rows kbExportRows, items []domain.KBArticleSummary) error {
	ids := make([]string, len(items))
	for index, item := range items {
		ids[index] = item.ID
	}
	votes, err := a.store.KBArticleVoteTotals(request.Context(), principal(request).Workspace.ID, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := writer.Write(rows.row(item, votes[item.ID])); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	if err := http.NewResponseController(response).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// agentLocation is the timezone a team member reads times in: their own, else the workspace's.
func agentLocation(workspace string, override *string) *time.Location {
	name := workspace
	if override != nil {
		name = *override
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}

// listKBTags lists the tags the article list filters by, with their article counts.
func (a *API) listKBTags(response http.ResponseWriter, request *http.Request) {
	tags, err := a.store.ListKBTags(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": tags})
}
