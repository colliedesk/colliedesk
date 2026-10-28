package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// writeKBError maps the refusals of knowledge base writes; anything else is a store failure.
func writeKBError(response http.ResponseWriter, err error) {
	var invalid *domain.InvalidKBError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.Is(err, domain.ErrKBChanged):
		writeError(response, http.StatusConflict, "changed_elsewhere", "This was changed by someone else. Reload to see their version, then try again.")
	case errors.Is(err, domain.ErrKBDuplicateSlug):
		writeError(response, http.StatusConflict, "duplicate_slug", domain.ErrKBDuplicateSlug.Error()+".")
	case errors.Is(err, domain.ErrKBCollectionNotEmpty):
		writeError(response, http.StatusConflict, "collection_not_empty", domain.ErrKBCollectionNotEmpty.Error()+".")
	case errors.Is(err, domain.ErrKBParentInTrash):
		writeError(response, http.StatusConflict, "parent_in_trash", domain.ErrKBParentInTrash.Error()+".")
	case errors.Is(err, domain.ErrKBCollectionInTrash):
		writeError(response, http.StatusConflict, "collection_in_trash", domain.ErrKBCollectionInTrash.Error()+".")
	case errors.Is(err, domain.ErrKBArticleLimit), errors.Is(err, domain.ErrKBCollectionLimit), errors.Is(err, domain.ErrKBMediaLimit):
		writeError(response, http.StatusConflict, "kb_limit", err.Error()+".")
	case errors.Is(err, domain.ErrKBArticleNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The article was not found.")
	case errors.Is(err, domain.ErrKBArticleVersionNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The article has no version with that number.")
	case errors.Is(err, domain.ErrKBCollectionNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The collection was not found.")
	case errors.Is(err, domain.ErrKBImageNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The image was not found.")
	default:
		writeStoreError(response, err)
	}
}

// seenAt reads the updated_at query parameter of a delete.
func seenAt(response http.ResponseWriter, request *http.Request) (time.Time, bool) {
	seen, err := time.Parse(time.RFC3339Nano, request.URL.Query().Get("updated_at"))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read as a query parameter: ?updated_at=<RFC 3339 time>.")
		return time.Time{}, false
	}
	return seen, true
}

// seenInBody reads a body that holds only updated_at, for restores.
func seenInBody(response http.ResponseWriter, request *http.Request) (time.Time, bool) {
	var input struct {
		UpdatedAt *time.Time `json:"updated_at"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read.")
		return time.Time{}, false
	}
	return *input.UpdatedAt, true
}

func (a *API) listKBCollections(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListKBCollections(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listTrashedKBCollections(response http.ResponseWriter, request *http.Request) {
	items, err := a.store.ListTrashedKBCollections(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createKBCollection(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Name        string  `json:"name"`
		Slug        *string `json:"slug"`
		Description string  `json:"description"`
		ParentID    *string `json:"parent_id"`
		Position    int     `json:"position"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A collection needs a name; slug, description, parent_id and position are optional.")
		return
	}
	if input.Slug != nil && *input.Slug == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Leave slug out to make one from the name.")
		return
	}
	slug := ""
	if input.Slug != nil {
		slug = *input.Slug
	}
	who := principal(request)
	parent := ""
	if input.ParentID != nil {
		parent = *input.ParentID
	}
	a.createOnce(response, request, createKBCollectionOperation, []string{input.Name, slug, input.Description, parent, strconv.Itoa(input.Position)}, func(ctx context.Context) (any, error) {
		collection, err := a.store.CreateKBCollection(ctx, who.Workspace.ID, savedReplyActor(ctx, who),
			store.KBCollectionInput{Name: input.Name, Slug: slug, Description: input.Description, ParentID: input.ParentID, Position: input.Position})
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return collection, nil
	})
}

func (a *API) updateKBCollection(response http.ResponseWriter, request *http.Request) {
	// A collection's name, slug and place are the help center's navigation.
	if a.askUnlessHeld(response, request, kbCollectionUpdateRoute, principal(request), auth.PermKBArticlesPublish, nil, nil) {
		return
	}
	var input struct {
		UpdatedAt   *time.Time      `json:"updated_at"`
		Name        *string         `json:"name"`
		Slug        *string         `json:"slug"`
		Description *string         `json:"description"`
		Position    *int            `json:"position"`
		ParentID    json.RawMessage `json:"parent_id"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and what to change.")
		return
	}
	change := store.KBCollectionChange{Name: input.Name, Slug: input.Slug, Description: input.Description, Position: input.Position}
	if input.ParentID != nil {
		change.MoveParent = true
		if string(input.ParentID) != "null" {
			var parent string
			if err := json.Unmarshal(input.ParentID, &parent); err != nil {
				writeError(response, http.StatusBadRequest, "invalid_request", "parent_id is a collection id, or null for the top.")
				return
			}
			change.Parent = &parent
		}
	}
	who := principal(request)
	collection, err := a.store.UpdateKBCollection(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), *input.UpdatedAt, change)
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, collection)
}

func (a *API) deleteKBCollection(response http.ResponseWriter, request *http.Request) {
	if a.askUnlessHeld(response, request, kbCollectionDeleteRoute, principal(request), auth.PermKBArticlesPublish, nil, nil) {
		return
	}
	seen, ok := seenAt(response, request)
	if !ok {
		return
	}
	who := principal(request)
	if err := a.store.DeleteKBCollection(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), seen); err != nil {
		writeKBError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreKBCollection(response http.ResponseWriter, request *http.Request) {
	if a.askUnlessHeld(response, request, kbCollectionUndeleteRoute, principal(request), auth.PermKBArticlesPublish, nil, nil) {
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	who := principal(request)
	collection, err := a.store.UndeleteKBCollection(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), seen)
	if errors.Is(err, domain.ErrKBCollectionNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "No collection with this id is in the trash.")
		return
	}
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, collection)
}

// setKBOrder puts the collections and articles directly under one parent in the order given.
func (a *API) setKBOrder(response http.ResponseWriter, request *http.Request) {
	// The order is public navigation, like a collection's name and place.
	if a.askUnlessHeld(response, request, Route{http.MethodPut, "/api/v1/kb/order", auth.PermissionConversationsWrite}, principal(request), auth.PermKBArticlesPublish, nil, nil) {
		return
	}
	var input struct {
		ParentID      json.RawMessage `json:"parent_id"`
		CollectionIDs *[]string       `json:"collection_ids"`
		ArticleIDs    *[]string       `json:"article_ids"`
	}
	if err := decodeJSON(request, &input); err != nil || input.ParentID == nil || input.CollectionIDs == nil || input.ArticleIDs == nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"Send parent_id (null for the top), collection_ids and article_ids: everything directly under that parent, first to last.")
		return
	}
	order := domain.KBOrder{CollectionIDs: *input.CollectionIDs, ArticleIDs: *input.ArticleIDs}
	if string(input.ParentID) != "null" {
		var parent string
		if err := json.Unmarshal(input.ParentID, &parent); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "parent_id is a collection id, or null for the top.")
			return
		}
		order.ParentID = &parent
	}
	order, err := a.store.SetKBOrder(request.Context(), principal(request).Workspace.ID, order)
	var stale *domain.KBOrderStaleError
	if errors.As(err, &stale) {
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": "order_stale",
			"message":        "Something was added, moved or deleted here since you read it. Nothing changed; read the order again and send it whole.",
			"collection_ids": stale.Current.CollectionIDs, "article_ids": stale.Current.ArticleIDs})})
		return
	}
	if errors.Is(err, domain.ErrKBCollectionNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The parent collection was not found, or it is in the trash.")
		return
	}
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, order)
}

func (a *API) listKBArticles(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	filter := store.KBArticleFilter{Query: query.Get("q"), Status: query.Get("status"), CollectionID: query.Get("collection_id"), Sort: query.Get("sort"),
		Limit: limit, Cursor: query.Get("cursor")}
	if filter.Sort != "" && filter.Sort != store.KBSortRecent && filter.Sort != store.KBSortManual {
		writeError(response, http.StatusBadRequest, "invalid_request", "sort is recent or manual.")
		return
	}
	if len([]rune(filter.Query)) > 200 {
		writeError(response, http.StatusBadRequest, "invalid_request", "q holds at most 200 characters.")
		return
	}
	if filter.Status != "" && domain.CheckKBStatus(filter.Status) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "status is draft or published.")
		return
	}
	items, next, err := a.store.ListKBArticles(request.Context(), principal(request).Workspace.ID, filter)
	if errors.Is(err, store.ErrBadCursor) {
		writeError(response, http.StatusBadRequest, "invalid_request", "cursor must be the next_cursor of the previous page.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	for index := range items {
		addressKBArticle(home, &items[index])
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalString(next)})
}

// addressKBArticle fills in the article's public address below home, the workspace's help center
// address (helpCenterHome).
func addressKBArticle(home string, article *domain.KBArticleSummary) {
	article.URL = home + helpcenter.ArticlePath(article.Number, article.Slug)
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (a *API) listTrashedKBArticles(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	items, next, err := a.store.ListTrashedKBArticles(request.Context(), principal(request).Workspace.ID, limit, request.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrBadCursor) {
		writeError(response, http.StatusBadRequest, "invalid_request", "cursor must be the next_cursor of the previous page.")
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	for index := range items {
		addressKBArticle(home, &items[index].KBArticleSummary)
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalString(next)})
}

func (a *API) getKBArticle(response http.ResponseWriter, request *http.Request) {
	article, err := a.store.GetKBArticle(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	addressKBArticle(home, &article.KBArticleSummary)
	drafts, err := a.ai.ArticleDraftsThisMonth(request.Context(), principal(request).Workspace.ID, article.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	article.AIDraftsThisMonth = &drafts
	writeJSON(response, http.StatusOK, article)
}

// Publishing changes what the public help center shows, which reaches customers: a token without
// kb_articles:publish may write drafts, and a change to what the help center shows (a published
// article, the media in one, a collection) waits for a person (an agent request), however the route
// itself is classified.
var (
	kbArticleCreateRoute        = Route{http.MethodPost, "/api/v1/kb/articles", auth.PermissionConversationsWrite}
	kbArticleUpdateRoute        = Route{http.MethodPatch, "/api/v1/kb/articles/{id}", auth.PermissionConversationsWrite}
	kbArticleVersionRoute       = Route{http.MethodPost, "/api/v1/kb/articles/{id}/versions/{version}/restore", auth.PermissionConversationsWrite}
	kbArticleDeleteRoute        = Route{http.MethodDelete, "/api/v1/kb/articles/{id}", auth.PermissionConversationsWrite}
	kbArticleUndeleteRoute      = Route{http.MethodPost, "/api/v1/kb/articles/{id}/restore", auth.PermissionConversationsWrite}
	kbCollectionUpdateRoute     = Route{http.MethodPatch, "/api/v1/kb/collections/{id}", auth.PermissionConversationsWrite}
	kbCollectionDeleteRoute     = Route{http.MethodDelete, "/api/v1/kb/collections/{id}", auth.PermissionConversationsWrite}
	kbCollectionUndeleteRoute   = Route{http.MethodPost, "/api/v1/kb/collections/{id}/restore", auth.PermissionConversationsWrite}
	kbMediaReplaceRoute         = Route{http.MethodPost, "/api/v1/kb/media/{id}/replace", auth.PermissionConversationsWrite}
	kbMediaReplacementUndoRoute = Route{http.MethodPost, "/api/v1/kb/media-replacements/{id}/undo", auth.PermissionConversationsWrite}
)

// askUnlessHeld files the call as an agent request and reports true when the token lacks permission
// and the change applies (public reports whether it would alter what people outside see). body is
// the call as it came, nil to read it now. A failed check is answered and reported true.
func (a *API) askUnlessHeld(response http.ResponseWriter, request *http.Request, route Route, who auth.Principal, permission string, body json.RawMessage,
	public func() (bool, error)) bool {
	if who.Allows(permission) {
		return false
	}
	if public != nil {
		applies, err := public()
		if err != nil {
			writeKBError(response, err)
			return true
		}
		if !applies {
			return false
		}
	}
	if body == nil {
		body = keepBody(request)
	}
	a.fileAgentRequest(response, request, route, who, domain.AgentRequestAction, body, "")
	return true
}

// articleIsPublished is the check askUnlessHeld runs for a change to one article.
func (a *API) articleIsPublished(request *http.Request, who auth.Principal) func() (bool, error) {
	return func() (bool, error) {
		return a.store.KBArticlePublished(request.Context(), who.Workspace.ID, request.PathValue("id"))
	}
}

// keepBody reads the request body so a handler can both decode it and, when it must wait for a
// person, file it as it came.
func keepBody(request *http.Request) json.RawMessage {
	if request.Body == nil {
		return json.RawMessage{}
	}
	raw, _ := io.ReadAll(io.LimitReader(request.Body, 1<<20))
	request.Body = io.NopCloser(bytes.NewReader(raw))
	return raw
}

func (a *API) createKBArticle(response http.ResponseWriter, request *http.Request) {
	raw := keepBody(request)
	var input struct {
		CollectionID string  `json:"collection_id"`
		Title        string  `json:"title"`
		Slug         *string `json:"slug"`
		BodyMD       *string `json:"body_md"`
		Status       string  `json:"status"`
		SEOTitle     string  `json:"seo_title"`
		SEODesc      string  `json:"seo_description"`
	}
	if err := decodeJSON(request, &input); err != nil || input.BodyMD == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "An article needs a collection_id, a title and body_md; slug and status are optional.")
		return
	}
	if input.Slug != nil && *input.Slug == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Leave slug out to make one from the title.")
		return
	}
	slug := ""
	if input.Slug != nil {
		slug = *input.Slug
	}
	who := principal(request)
	if input.Status == "published" && a.askUnlessHeld(response, request, kbArticleCreateRoute, who, auth.PermKBArticlesPublish, raw, nil) {
		return
	}
	a.createOnce(response, request, createKBArticleOperation, []string{input.CollectionID, input.Title, slug, *input.BodyMD, input.Status, input.SEOTitle, input.SEODesc}, func(ctx context.Context) (any, error) {
		article, err := a.store.CreateKBArticle(ctx, who.Workspace.ID, savedReplyActor(ctx, who),
			store.KBArticleInput{CollectionID: input.CollectionID, Title: input.Title, Slug: slug, BodyMD: *input.BodyMD, Status: input.Status,
				SEOTitle: input.SEOTitle, SEODescription: input.SEODesc})
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		a.warnMissingKBMedia(ctx, who.Workspace.ID, &article)
		home, ok := a.helpCenterHome(response, request)
		if !ok {
			return nil, errAnswered
		}
		addressKBArticle(home, &article.KBArticleSummary)
		return article, nil
	})
}

func (a *API) updateKBArticle(response http.ResponseWriter, request *http.Request) {
	raw := keepBody(request)
	var input struct {
		UpdatedAt    *time.Time `json:"updated_at"`
		Title        *string    `json:"title"`
		Slug         *string    `json:"slug"`
		BodyMD       *string    `json:"body_md"`
		CollectionID *string    `json:"collection_id"`
		Status       *string    `json:"status"`
		SEOTitle     *string    `json:"seo_title"`
		SEODesc      *string    `json:"seo_description"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil ||
		(input.Title == nil && input.Slug == nil && input.BodyMD == nil && input.CollectionID == nil && input.Status == nil && input.SEOTitle == nil && input.SEODesc == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new title, slug, body_md, collection_id, status, seo_title or seo_description.")
		return
	}
	who := principal(request)
	// Making an article published, or changing one that is, changes the help center.
	publishes := func() (bool, error) {
		if input.Status != nil && *input.Status == "published" {
			return true, nil
		}
		return a.store.KBArticlePublished(request.Context(), who.Workspace.ID, request.PathValue("id"))
	}
	if a.askUnlessHeld(response, request, kbArticleUpdateRoute, who, auth.PermKBArticlesPublish, raw, publishes) {
		return
	}
	article, err := a.store.UpdateKBArticle(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), *input.UpdatedAt,
		store.KBArticleChange{Title: input.Title, Slug: input.Slug, BodyMD: input.BodyMD, CollectionID: input.CollectionID, Status: input.Status,
			SEOTitle: input.SEOTitle, SEODescription: input.SEODesc})
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.warnMissingKBMedia(request.Context(), who.Workspace.ID, &article)
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	addressKBArticle(home, &article.KBArticleSummary)
	writeJSON(response, http.StatusOK, article)
}

func (a *API) deleteKBArticle(response http.ResponseWriter, request *http.Request) {
	// Trashing a published article takes it off the help center.
	if a.askUnlessHeld(response, request, kbArticleDeleteRoute, principal(request), auth.PermKBArticlesPublish, nil, a.articleIsPublished(request, principal(request))) {
		return
	}
	seen, ok := seenAt(response, request)
	if !ok {
		return
	}
	who := principal(request)
	if err := a.store.DeleteKBArticle(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), seen); err != nil {
		writeKBError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreKBArticle(response http.ResponseWriter, request *http.Request) {
	// A published article back from the trash is back on the help center.
	if a.askUnlessHeld(response, request, kbArticleUndeleteRoute, principal(request), auth.PermKBArticlesPublish, nil, a.articleIsPublished(request, principal(request))) {
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	who := principal(request)
	article, err := a.store.UndeleteKBArticle(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), seen)
	if errors.Is(err, domain.ErrKBArticleNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "No article with this id is in the trash.")
		return
	}
	if err != nil {
		writeKBError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	addressKBArticle(home, &article.KBArticleSummary)
	writeJSON(response, http.StatusOK, article)
}

func (a *API) excludeKBArticleFromAI(response http.ResponseWriter, request *http.Request) {
	a.setKBArticleAIExcluded(response, request, true)
}

func (a *API) includeKBArticleInAI(response http.ResponseWriter, request *http.Request) {
	a.setKBArticleAIExcluded(response, request, false)
}

func (a *API) setKBArticleAIExcluded(response http.ResponseWriter, request *http.Request, excluded bool) {
	who := principal(request)
	article, err := a.store.SetKBArticleAIExcluded(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), excluded)
	if err != nil {
		writeKBError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	addressKBArticle(home, &article.KBArticleSummary)
	writeJSON(response, http.StatusOK, article)
}

func (a *API) listKBArticleVersions(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	before := 0
	if raw := request.URL.Query().Get("before_version"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(response, http.StatusBadRequest, "invalid_request", "before_version must be a version number, 1 or more.")
			return
		}
		before = parsed
	}
	items, next, err := a.store.KBArticleVersions(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), limit, before)
	if err != nil {
		writeKBError(response, err)
		return
	}
	var nextVersion *int
	if next > 0 {
		nextVersion = &next
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_before_version": nextVersion})
}

func (a *API) restoreKBArticleVersion(response http.ResponseWriter, request *http.Request) {
	version, err := strconv.Atoi(request.PathValue("version"))
	if err != nil || version < 1 {
		writeError(response, http.StatusNotFound, "not_found", "The article has no version with that number.")
		return
	}
	raw := keepBody(request)
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	who := principal(request)
	// Restoring a version of a published article changes what the help center shows.
	if a.askUnlessHeld(response, request, kbArticleVersionRoute, who, auth.PermKBArticlesPublish, raw, a.articleIsPublished(request, who)) {
		return
	}
	article, err := a.store.RestoreKBArticleVersion(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), version, seen)
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.warnMissingKBMedia(request.Context(), who.Workspace.ID, &article)
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	addressKBArticle(home, &article.KBArticleSummary)
	writeJSON(response, http.StatusOK, article)
}

func (a *API) uploadKBImage(response http.ResponseWriter, request *http.Request) {
	if a.config.Attachments == nil {
		writeError(response, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server.")
		return
	}
	who := principal(request)
	if request.ContentLength > domain.MaxKBImageBytes {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "An image holds at most 5 MiB; nothing was stored.")
		return
	}
	query := request.URL.Query()
	upload := domain.KBImageUpload{Name: query.Get("name"), Alt: query.Get("alt"), Keep: query.Get("keep") == "true"}
	data, err := io.ReadAll(io.LimitReader(request.Body, domain.MaxKBImageBytes+1))
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "The image could not be read.")
		return
	}
	if len(data) > domain.MaxKBImageBytes {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "An image holds at most 5 MiB; nothing was stored.")
		return
	}
	// The bytes decide the type, never the declared Content-Type, so nothing else is served as an image.
	info, err := media.InspectImage(data)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", sentence(err)+"; nothing was stored.")
		return
	}
	// Location, camera and comments leave before anything is stored or compared.
	data, err = media.StripMetadata(info, data)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", sentence(err)+"; nothing was stored.")
		return
	}
	sum := sha256.Sum256(data)
	if image, found, err := a.store.FindKBImage(request.Context(), who.Workspace.ID, hex.EncodeToString(sum[:]), upload); err != nil {
		writeKBError(response, err)
		return
	} else if found {
		// Bytes the workspace already holds take no room: a full workspace can still use them again.
		writeJSON(response, http.StatusCreated, image)
		return
	}
	if err := a.store.CheckKBImageAdmission(request.Context(), who.Workspace.ID, int64(len(data))); err != nil {
		writeStoreError(response, err)
		return
	}
	object, err := a.config.Attachments.Put(request.Context(), who.Workspace.ID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeInternalError(response, err)
		return
	}
	input := store.NewKBImage{Object: object, ContentType: info.ContentType, Width: info.Width, Height: info.Height, KBImageUpload: upload}
	input.Thumb = a.storeKBThumbnail(request.Context(), who.Workspace.ID, info, data)
	image, created, err := a.store.CreateKBImage(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), input)
	discard := func(keys ...string) {
		for _, key := range keys {
			if discardErr := a.store.DiscardObject(request.Context(), who.Workspace.ID, key); discardErr != nil {
				log.Printf("knowledge base image: discard unrecorded bytes: %v", discardErr)
			}
		}
	}
	if err != nil {
		discard(object.Key)
		if input.Thumb != nil {
			discard(input.Thumb.Key)
		}
		writeKBError(response, err)
		return
	}
	if !created && input.Thumb != nil {
		discard(input.Thumb.Key)
	}
	writeJSON(response, http.StatusCreated, image)
}

// storeKBThumbnail makes and stores the library thumbnail of an image, or returns nil: an image too
// large to decode within the budget has none, and a failure only costs the thumbnail.
func (a *API) storeKBThumbnail(ctx context.Context, workspaceID string, info media.Image, data []byte) *attachments.Object {
	thumb, _, err := media.Thumbnail(ctx, info, data)
	if err != nil {
		if !errors.Is(err, media.ErrNoThumbnail) {
			log.Printf("knowledge base image: thumbnail: %v", err)
		}
		return nil
	}
	object, err := a.config.Attachments.Put(ctx, workspaceID, bytes.NewReader(thumb), int64(len(thumb)))
	if err != nil {
		log.Printf("knowledge base image: store thumbnail: %v", err)
		return nil
	}
	return &object
}

// sentence makes an error message the start of a sentence.
func sentence(err error) string {
	message := err.Error()
	return strings.ToUpper(message[:1]) + message[1:]
}

func (a *API) getKBImage(response http.ResponseWriter, request *http.Request) {
	if a.config.Attachments == nil {
		writeError(response, http.StatusNotFound, "not_found", "The image was not found.")
		return
	}
	workspaceID := principal(request).Workspace.ID
	image, err := a.store.GetKBImage(request.Context(), workspaceID, request.PathValue("id"))
	// Storage keys start with the owning workspace id; a row pointing at another workspace's bytes is never served.
	if err != nil || !strings.HasPrefix(image.StorageKey, workspaceID+"/") {
		writeKBError(response, domain.ErrKBImageNotFound)
		return
	}
	body, err := a.config.Attachments.Open(request.Context(), image.StorageKey)
	if err != nil {
		log.Printf("knowledge base image %s: open stored bytes: %v", image.ID, err)
		writeKBError(response, domain.ErrKBImageNotFound)
		return
	}
	defer body.Close()
	header := response.Header()
	header.Set("Content-Type", image.ContentType)
	header.Set("Content-Length", strconv.FormatInt(image.Size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Content-SHA256", image.SHA256)
	// Nothing the image holds may run or load, even if a browser opened it as a page.
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Private: it needs a session. Immutable: an id always names the same bytes.
	header.Set("Cache-Control", "private, max-age=31536000, immutable")
	response.WriteHeader(http.StatusOK)
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("knowledge base image %s: stream: %v", image.ID, err)
	}
}

func (a *API) getHelpCenter(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	center, err := a.store.GetHelpCenter(request.Context(), who.Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	center.URL = home
	writeJSON(response, http.StatusOK, center)
}

func (a *API) putHelpCenter(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Enabled *bool   `json:"enabled"`
		Title   *string `json:"title"`
		Locale  *string `json:"locale"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Enabled == nil || input.Title == nil || input.Locale == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send enabled, title and locale.")
		return
	}
	who := principal(request)
	center, err := a.store.PutHelpCenter(request.Context(), who.Workspace.ID, *input.Enabled, *input.Title, *input.Locale)
	if err != nil {
		writeKBError(response, err)
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	center.URL = home
	writeJSON(response, http.StatusOK, center)
}
