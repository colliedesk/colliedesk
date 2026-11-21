package httpapi

import (
	"bytes"
	"context"
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
	var raise *domain.KBVisibilityRaiseError
	switch {
	case errors.As(err, &invalid):
		writeError(response, http.StatusBadRequest, "invalid_request", invalid.Message)
	case errors.As(err, &raise):
		writeError(response, http.StatusConflict, "approval_required", raise.Error())
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
	case errors.Is(err, domain.ErrKBTranslationNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The article has no translation in that language outside the trash. GET /api/v1/kb/articles/{id} lists its translations; GET /api/v1/kb/translations/trash those in the trash.")
	case errors.Is(err, domain.ErrKBDraftNotFound):
		writeError(response, http.StatusNotFound, "not_found", "There are no unpublished changes: the published text is the current one.")
	case errors.Is(err, domain.ErrKBTranslationVersionNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The translation has no version with that number.")
	case errors.Is(err, domain.ErrKBTranslationExists):
		writeError(response, http.StatusConflict, "translation_exists", "The article already has a translation in this language, perhaps in the trash. Change that one (PATCH), or restore it from the trash.")
	case errors.Is(err, domain.ErrKBTemplateInTrash):
		writeError(response, http.StatusConflict, "template_in_trash", "The "+domain.ErrKBTemplateInTrash.Error()+". GET /api/v1/kb/templates lists the others; POST /api/v1/kb/templates/{id}/restore undeletes it.")
	case errors.Is(err, domain.ErrKBTemplateNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The template was not found. GET /api/v1/kb/templates lists them.")
	case errors.Is(err, domain.ErrKBLanguageNotListed):
		writeError(response, http.StatusBadRequest, "language_not_listed", "This language is not one of the help center's languages (or it is the primary language, the article's own). Add it with PUT /api/v1/help-center/languages first.")
	default:
		if !writeKBReviewError(response, err) {
			writeStoreError(response, err)
		}
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
		Template    *string `json:"default_template_id"`
		Visibility  string  `json:"visibility"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "A collection needs a name; slug, description, parent_id, position, default_template_id and visibility are optional.")
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
	template := ""
	if input.Template != nil {
		template = *input.Template
	}
	a.createOnce(response, request, createKBCollectionOperation, []string{input.Name, slug, input.Description, parent, strconv.Itoa(input.Position), template, input.Visibility}, func(ctx context.Context) (any, error) {
		collection, err := a.store.CreateKBCollection(ctx, who.Workspace.ID, savedReplyActor(ctx, who),
			store.KBCollectionInput{Name: input.Name, Slug: slug, Description: input.Description, ParentID: input.ParentID, Position: input.Position,
				DefaultTemplateID: input.Template, Visibility: input.Visibility})
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		return collection, nil
	})
}

func (a *API) updateKBCollection(response http.ResponseWriter, request *http.Request) {
	var input struct {
		UpdatedAt   *time.Time      `json:"updated_at"`
		Name        *string         `json:"name"`
		Slug        *string         `json:"slug"`
		Description *string         `json:"description"`
		Position    *int            `json:"position"`
		ParentID    json.RawMessage `json:"parent_id"`
		Template    json.RawMessage `json:"default_template_id"`
		Approval    *string         `json:"require_approval"`
		Visibility  *string         `json:"visibility"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and what to change.")
		return
	}
	// A collection's name, slug and place are the help center's navigation, and showing it to more
	// people publishes what is in it; keeping it from more people is an edit.
	onlyRestricts := input.Name == nil && input.Slug == nil && input.Description == nil && input.Position == nil &&
		input.ParentID == nil && input.Template == nil && input.Approval == nil && input.Visibility != nil
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, func() (bool, error) {
		if !onlyRestricts {
			return true, nil
		}
		return a.store.KBCollectionVisibilityRaises(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), *input.Visibility)
	}) {
		return
	}
	// Whether a collection needs approval is a help center setting, like the workspace's own.
	if input.Approval != nil && a.refuseUnlessHeld(response, request, principal(request), auth.PermHelpCenterUpdate, nil) {
		return
	}
	change := store.KBCollectionChange{Name: input.Name, Slug: input.Slug, Description: input.Description, Position: input.Position, RequireApproval: input.Approval, Visibility: input.Visibility}
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
	if input.Template != nil {
		change.SetDefaultTemplate = true
		if string(input.Template) != "null" {
			var template string
			if err := json.Unmarshal(input.Template, &template); err != nil {
				writeError(response, http.StatusBadRequest, "invalid_request", "default_template_id is a template id, or null for none.")
				return
			}
			change.DefaultTemplateID = &template
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
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, nil) {
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
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, nil) {
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
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, nil) {
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
		Limit: limit, Cursor: query.Get("cursor"), ReviewState: query.Get("state"), Visibility: query.Get("visibility")}
	if filter.Visibility != "" && domain.CheckKBVisibility(filter.Visibility) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "visibility is everyone, help_chat or agents: who can see the article now.")
		return
	}
	if filter.ReviewState != "" && filter.ReviewState != store.KBInReview && filter.ReviewState != store.KBApproved {
		writeError(response, http.StatusBadRequest, "invalid_request", "state is in_review or approved.")
		return
	}
	switch query.Get("has_draft") {
	case "", "false":
	case "true":
		filter.HasDraft = true
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "has_draft is true or false.")
		return
	}
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
	if !draftedKBArticle(response, request, &article) {
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	addressKBArticle(home, &article.KBArticleSummary)
	if !a.addressKBArticleTranslations(response, request, home, &article) {
		return
	}
	drafts, err := a.ai.ArticleDraftsThisMonth(request.Context(), principal(request).Workspace.ID, article.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	article.AIDraftsThisMonth = &drafts
	writeJSON(response, http.StatusOK, article)
}

// Publishing changes what the public help center shows, which reaches customers: a token without
// kb_articles:publish may write drafts and save or discard the unpublished changes of a published
// article, and a change to what the help center shows (publishing those changes, the media in a
// published article, a collection) waits for a person (an agent request), however the route itself
// is classified.
var (
	kbArticleCreateRoute        = Route{http.MethodPost, "/api/v1/kb/articles", auth.PermissionSession}
	kbArticleUpdateRoute        = Route{http.MethodPatch, "/api/v1/kb/articles/{id}", auth.PermissionSession}
	kbArticleVersionRoute       = Route{http.MethodPost, "/api/v1/kb/articles/{id}/versions/{version}/restore", auth.PermissionSession}
	kbArticleDeleteRoute        = Route{http.MethodDelete, "/api/v1/kb/articles/{id}", auth.PermissionSession}
	kbArticleUndeleteRoute      = Route{http.MethodPost, "/api/v1/kb/articles/{id}/restore", auth.PermissionSession}
	kbCollectionUpdateRoute     = Route{http.MethodPatch, "/api/v1/kb/collections/{id}", auth.PermissionSession}
	kbCollectionDeleteRoute     = Route{http.MethodDelete, "/api/v1/kb/collections/{id}", auth.PermissionSession}
	kbCollectionUndeleteRoute   = Route{http.MethodPost, "/api/v1/kb/collections/{id}/restore", auth.PermissionSession}
	kbMediaReplaceRoute         = Route{http.MethodPost, "/api/v1/kb/media/{id}/replace", auth.PermissionSession}
	kbMediaReplacementUndoRoute = Route{http.MethodPost, "/api/v1/kb/media-replacements/{id}/undo", auth.PermissionSession}
)

// refuseUnlessHeld answers 403 permission_missing and reports true when the caller lacks permission and
// the change applies (public reports whether it would alter what people outside see; nil: it always
// does). A failed check is answered and reported true.
func (a *API) refuseUnlessHeld(response http.ResponseWriter, request *http.Request, who auth.Principal, permission string, public func() (bool, error)) bool {
	if who.Can(permission) {
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
	a.writePermissionMissing(response, request, who, permission)
	return true
}

// articleIsPublished is the check refuseUnlessHeld runs for a change to one article.
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

// kbArticleCollections reads collection_ids, the collections an article is listed in with its home
// first, into the home and the others; collection_id, when sent too, must be that home. Without
// collection_ids both stay as sent. A refusal says what to send.
func kbArticleCollections(collectionID *string, collectionIDs *[]string) (*string, *[]string, string) {
	if collectionIDs == nil {
		return collectionID, nil, ""
	}
	ids := *collectionIDs
	if len(ids) == 0 {
		return nil, nil, "collection_ids names at least one collection: the article's home first, then the others it is listed in."
	}
	if collectionID != nil && !strings.EqualFold(*collectionID, ids[0]) {
		return nil, nil, "collection_id is the article's home, the first of collection_ids; send only collection_ids, or put that collection first."
	}
	extras := ids[1:]
	return &ids[0], &extras, ""
}

func (a *API) createKBArticle(response http.ResponseWriter, request *http.Request) {
	var input struct {
		CollectionID  string    `json:"collection_id"`
		CollectionIDs *[]string `json:"collection_ids"`
		Title         string    `json:"title"`
		Slug          *string   `json:"slug"`
		BodyMD        *string   `json:"body_md"`
		Status        string    `json:"status"`
		SEOTitle      string    `json:"seo_title"`
		SEODesc       string    `json:"seo_description"`
		ShowContents  *bool     `json:"show_contents"`
		TemplateID    *string   `json:"template_id"`
		Visibility    string    `json:"visibility"`
		Tags          []string  `json:"tags"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "An article needs a collection_id and a title; body_md or template_id, slug, status and visibility are optional.")
		return
	}
	if input.BodyMD != nil && input.TemplateID != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send body_md or template_id, not both: a template gives the article its text.")
		return
	}
	fromCollection := input.BodyMD == nil && input.TemplateID == nil
	body, templateID := "", ""
	if input.BodyMD != nil {
		body = *input.BodyMD
	}
	if input.TemplateID != nil {
		templateID = *input.TemplateID
	}
	if input.Slug != nil && *input.Slug == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "Leave slug out to make one from the title.")
		return
	}
	var sentHome *string
	if input.CollectionID != "" {
		sentHome = &input.CollectionID
	}
	homeCollection, extras, problem := kbArticleCollections(sentHome, input.CollectionIDs)
	if problem != "" {
		writeError(response, http.StatusBadRequest, "invalid_request", problem)
		return
	}
	if homeCollection != nil {
		input.CollectionID = *homeCollection
	}
	var extraIDs []string
	if extras != nil {
		extraIDs = *extras
	}
	slug := ""
	if input.Slug != nil {
		slug = *input.Slug
	}
	who := principal(request)
	if input.Status == "published" && a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	a.createOnce(response, request, createKBArticleOperation, []string{input.CollectionID, strings.Join(extraIDs, ","), input.Title, slug, body, templateID, strconv.FormatBool(fromCollection), input.Status, input.SEOTitle, input.SEODesc, showContentsKey(input.ShowContents), input.Visibility, strings.Join(input.Tags, "\n")}, func(ctx context.Context) (any, error) {
		article, err := a.store.CreateKBArticle(ctx, who.Workspace.ID, savedReplyActor(ctx, who),
			store.KBArticleInput{CollectionID: input.CollectionID, ExtraCollectionIDs: extraIDs, Title: input.Title, Slug: slug, BodyMD: body, Status: input.Status,
				SEOTitle: input.SEOTitle, SEODescription: input.SEODesc, ShowContents: input.ShowContents, TemplateID: templateID, BodyFromCollection: fromCollection,
				Visibility: input.Visibility, Tags: input.Tags})
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
		if !a.addressKBArticleTranslations(response, request, home, &article) {
			return nil, errAnswered
		}
		return article, nil
	})
}

func (a *API) updateKBArticle(response http.ResponseWriter, request *http.Request) {
	var input struct {
		UpdatedAt     *time.Time `json:"updated_at"`
		Title         *string    `json:"title"`
		Slug          *string    `json:"slug"`
		BodyMD        *string    `json:"body_md"`
		CollectionID  *string    `json:"collection_id"`
		CollectionIDs *[]string  `json:"collection_ids"`
		Status        *string    `json:"status"`
		SEOTitle      *string    `json:"seo_title"`
		SEODesc       *string    `json:"seo_description"`
		ShowContents  *bool      `json:"show_contents"`
		Visibility    *string    `json:"visibility"`
		Tags          *[]string  `json:"tags"`
		AuthorAgentID *string    `json:"author_agent_id"`
		Autosave      bool       `json:"autosave"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil ||
		(input.Title == nil && input.Slug == nil && input.BodyMD == nil && input.CollectionID == nil && input.CollectionIDs == nil && input.Status == nil && input.SEOTitle == nil &&
			input.SEODesc == nil && input.ShowContents == nil && input.Visibility == nil && input.Tags == nil && input.AuthorAgentID == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new title, slug, body_md, collection_id, collection_ids, status, seo_title, seo_description, show_contents, visibility, tags or author_agent_id.")
		return
	}
	if input.Autosave && (input.Slug != nil || input.CollectionID != nil || input.CollectionIDs != nil || input.Status != nil || input.SEOTitle != nil || input.SEODesc != nil || input.ShowContents != nil ||
		input.Visibility != nil || input.Tags != nil || input.AuthorAgentID != nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "An autosave changes only title and body_md; save the other fields without autosave.")
		return
	}
	homeCollection, extras, problem := kbArticleCollections(input.CollectionID, input.CollectionIDs)
	if problem != "" {
		writeError(response, http.StatusBadRequest, "invalid_request", problem)
		return
	}
	who := principal(request)
	// Publishing or unpublishing changes the help center, and so does showing a published article to
	// more people; a change to a published article is saved as its draft, which the help center does
	// not show, and keeping it from more people applies at once.
	publishes := func() (bool, error) {
		if input.Visibility != nil {
			raises, err := a.store.KBArticleVisibilityRaises(request.Context(), who.Workspace.ID, request.PathValue("id"), *input.Visibility)
			if raises || err != nil {
				return raises, err
			}
		}
		if input.Status == nil {
			return false, nil
		}
		published, err := a.store.KBArticlePublished(request.Context(), who.Workspace.ID, request.PathValue("id"))
		return (*input.Status == domain.KBPublished) != published, err
	}
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, publishes) {
		return
	}
	article, err := a.store.UpdateKBArticle(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), *input.UpdatedAt,
		store.KBArticleChange{Title: input.Title, Slug: input.Slug, BodyMD: input.BodyMD, CollectionID: homeCollection, ExtraCollectionIDs: extras, Status: input.Status,
			SEOTitle: input.SEOTitle, SEODescription: input.SEODesc, ShowContents: input.ShowContents, Visibility: input.Visibility, Tags: input.Tags, AuthorAgentID: input.AuthorAgentID, Autosave: input.Autosave})
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
	if !a.addressKBArticleTranslations(response, request, home, &article) {
		return
	}
	writeJSON(response, http.StatusOK, article)
}

func (a *API) deleteKBArticle(response http.ResponseWriter, request *http.Request) {
	// Trashing a published article takes it off the help center.
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, a.articleIsPublished(request, principal(request))) {
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
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, a.articleIsPublished(request, principal(request))) {
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
	if !a.addressKBArticleTranslations(response, request, home, &article) {
		return
	}
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
	if !a.addressKBArticleTranslations(response, request, home, &article) {
		return
	}
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
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	// On a published article the version becomes its draft, so the help center does not change.
	who := principal(request)
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
	if !a.addressKBArticleTranslations(response, request, home, &article) {
		return
	}
	writeJSON(response, http.StatusOK, article)
}

func (a *API) uploadKBImage(response http.ResponseWriter, request *http.Request) {
	if a.config.Attachments == nil {
		writeError(response, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server.")
		return
	}
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
	image, ok := a.storeKBImage(request.Context(), response, request, data, upload, func(err error) {
		writeError(response, http.StatusBadRequest, "invalid_request", sentence(err)+"; nothing was stored.")
	})
	if ok {
		writeJSON(response, http.StatusCreated, image)
	}
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
		Enabled         *bool   `json:"enabled"`
		Title           *string `json:"title"`
		Locale          *string `json:"locale"`
		RequireApproval *bool   `json:"require_approval"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Enabled == nil || input.Title == nil || input.Locale == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send enabled, title and locale.")
		return
	}
	who := principal(request)
	// Turning approval off lets a draft reach the help center unreviewed, so it is a publish decision.
	if input.RequireApproval != nil && a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	center, err := a.store.PutHelpCenter(request.Context(), who.Workspace.ID,
		store.HelpCenterChange{Enabled: *input.Enabled, Title: *input.Title, Locale: *input.Locale, RequireApproval: input.RequireApproval})
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

// showContentsKey is the contents switch of a create as its idempotency key part: "" when left out.
func showContentsKey(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}
