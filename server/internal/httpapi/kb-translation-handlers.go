package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/helpcenter"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Translations are language versions of an article (docs/internals/knowledge-base.md,
// "Translations"). Their routes follow the article's: the same permissions, kb_articles:publish for
// anything that changes what the help center may show, the updated_at of the last read on every change.

// addressKBTranslation fills in the public address of a translation of article number: its page in
// its language while customers see that language, else the article's page in the primary language
// (which moves to its current address). A hidden language's address is the help center's 404, so it is
// never handed out: a person or an agent could paste it in a reply.
func addressKBTranslation(home string, shown map[string]bool, number int64, translation *domain.KBTranslationSummary) {
	if shown[translation.Locale] {
		translation.URL = home + helpcenter.TranslationPath(translation.Locale, number, translation.Slug)
		return
	}
	translation.URL = home + "/a/" + strconv.FormatInt(number, 10)
}

// shownLanguages reads the languages customers see, for addressKBTranslation; an error is answered.
func (a *API) shownLanguages(response http.ResponseWriter, request *http.Request) (map[string]bool, bool) {
	shown, err := a.store.ShownHelpCenterLanguages(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return nil, false
	}
	return shown, true
}

// addressKBArticleTranslations fills in the addresses of an article's translations.
func (a *API) addressKBArticleTranslations(response http.ResponseWriter, request *http.Request, home string, article *domain.KBArticle) bool {
	if len(article.Translations) == 0 {
		return true
	}
	shown, ok := a.shownLanguages(response, request)
	if !ok {
		return false
	}
	for index := range article.Translations {
		addressKBTranslation(home, shown, article.Number, &article.Translations[index])
	}
	return true
}

func (a *API) writeKBTranslation(response http.ResponseWriter, request *http.Request, status int, translation domain.KBArticleTranslation) {
	if a.addressedKBTranslation(response, request, &translation) {
		writeJSON(response, status, translation)
	}
}

// addressedKBTranslation gives the translation its help center address; false when it answered a refusal.
func (a *API) addressedKBTranslation(response http.ResponseWriter, request *http.Request, translation *domain.KBArticleTranslation) bool {
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return false
	}
	shown, ok := a.shownLanguages(response, request)
	if !ok {
		return false
	}
	addressKBTranslation(home, shown, translation.Original.Number, &translation.KBTranslationSummary)
	return true
}

func (a *API) getHelpCenterLanguages(response http.ResponseWriter, request *http.Request) {
	languages, err := a.store.GetHelpCenterLanguages(request.Context(), principal(request).Workspace.ID)
	if err != nil {
		writeStoreError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, languages)
}

func (a *API) putHelpCenterLanguages(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Languages *[]domain.HelpCenterLanguageInput `json:"languages"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Languages == nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			`Send languages: every language besides the primary one, in menu order, as {"locale": "fr", "visible": false}; an empty list removes them all.`)
		return
	}
	who := principal(request)
	// Showing or hiding a language changes the public help center.
	shown := func() (bool, error) {
		return a.store.HelpCenterLanguagesShownChange(request.Context(), who.Workspace.ID, *input.Languages)
	}
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, shown) {
		return
	}
	languages, err := a.store.SetHelpCenterLanguages(request.Context(), who.Workspace.ID, *input.Languages)
	var inUse *store.HelpCenterLanguagesInUseError
	if errors.As(err, &inUse) {
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": "language_in_use",
			"message": "These languages still hold translations or collection names (the trash counts): " + joinLocales(inUse.Locales) +
				". Nothing changed. Keep them in the list with visible false to hide them, or delete what is written in them first.",
			"locales": inUse.Locales})})
		return
	}
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, languages)
}

func joinLocales(locales []string) string {
	out := ""
	for index, locale := range locales {
		if index > 0 {
			out += ", "
		}
		out += locale
	}
	return out
}

func (a *API) listKBLanguage(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	query := request.URL.Query()
	filter := store.KBLanguageFilter{Locale: query.Get("locale"), State: query.Get("state"), Limit: limit, Cursor: query.Get("cursor")}
	if domain.CheckLocale(filter.Locale) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "locale is a language of the help center, such as fr or pt-BR (GET /api/v1/help-center/languages).")
		return
	}
	items, next, err := a.store.ListKBLanguage(request.Context(), principal(request).Workspace.ID, filter)
	if errors.Is(err, store.ErrBadCursor) {
		writeError(response, http.StatusBadRequest, "invalid_request", "cursor must be the next_cursor of the previous page.")
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
	shown, ok := a.shownLanguages(response, request)
	if !ok {
		return
	}
	for index := range items {
		if items[index].Translation != nil {
			addressKBTranslation(home, shown, items[index].Number, items[index].Translation)
		}
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalString(next)})
}

func (a *API) listTrashedKBTranslations(response http.ResponseWriter, request *http.Request) {
	limit, ok := savedReplyPage(response, request)
	if !ok {
		return
	}
	items, next, err := a.store.ListTrashedKBTranslations(request.Context(), principal(request).Workspace.ID, limit, request.URL.Query().Get("cursor"))
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
	shown, ok := a.shownLanguages(response, request)
	if !ok {
		return
	}
	for index := range items {
		addressKBTranslation(home, shown, items[index].ArticleNumber, &items[index].KBTranslationSummary)
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "next_cursor": optionalString(next)})
}

func (a *API) createKBTranslation(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Locale   string  `json:"locale"`
		Title    string  `json:"title"`
		Slug     *string `json:"slug"`
		BodyMD   *string `json:"body_md"`
		Status   string  `json:"status"`
		SEOTitle string  `json:"seo_title"`
		SEODesc  string  `json:"seo_description"`
	}
	if err := decodeJSON(request, &input); err != nil || input.BodyMD == nil || input.Locale == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A translation needs a locale (a language of the help center), a title and body_md; slug, status, seo_title and seo_description are optional.")
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
	if input.Status == domain.KBPublished && a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	articleID := request.PathValue("id")
	a.createOnce(response, request, createKBTranslationOperation, []string{articleID, input.Locale, input.Title, slug, *input.BodyMD, input.Status, input.SEOTitle, input.SEODesc},
		func(ctx context.Context) (any, error) {
			translation, err := a.store.CreateKBTranslation(ctx, who.Workspace.ID, savedReplyActor(ctx, who), articleID, store.KBTranslationInput{Locale: input.Locale, Title: input.Title,
				Slug: slug, BodyMD: *input.BodyMD, Status: input.Status, SEOTitle: input.SEOTitle, SEODescription: input.SEODesc})
			if err != nil {
				writeKBError(response, err)
				return nil, errAnswered
			}
			home, ok := a.helpCenterHome(response, request)
			if !ok {
				return nil, errAnswered
			}
			shown, err := a.store.ShownHelpCenterLanguages(ctx, who.Workspace.ID)
			if err != nil {
				writeStoreError(response, err)
				return nil, errAnswered
			}
			addressKBTranslation(home, shown, translation.Original.Number, &translation.KBTranslationSummary)
			return translation, nil
		})
}

func (a *API) getKBTranslation(response http.ResponseWriter, request *http.Request) {
	translation, err := a.store.GetKBTranslation(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("locale"))
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.writeKBTranslation(response, request, http.StatusOK, translation)
}

// translationIsPublished is the check refuseUnlessHeld runs for a change to one translation.
func (a *API) translationIsPublished(request *http.Request, who auth.Principal) func() (bool, error) {
	return func() (bool, error) {
		return a.store.KBTranslationPublished(request.Context(), who.Workspace.ID, request.PathValue("id"), request.PathValue("locale"))
	}
}

func (a *API) updateKBTranslation(response http.ResponseWriter, request *http.Request) {
	var input struct {
		UpdatedAt *time.Time `json:"updated_at"`
		Title     *string    `json:"title"`
		Slug      *string    `json:"slug"`
		BodyMD    *string    `json:"body_md"`
		Status    *string    `json:"status"`
		SEOTitle  *string    `json:"seo_title"`
		SEODesc   *string    `json:"seo_description"`
		Outdated  *bool      `json:"outdated"`
	}
	if err := decodeJSON(request, &input); err != nil || input.UpdatedAt == nil ||
		(input.Title == nil && input.Slug == nil && input.BodyMD == nil && input.Status == nil && input.SEOTitle == nil && input.SEODesc == nil && input.Outdated == nil) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the updated_at you last read, and a new title, slug, body_md, status, seo_title, seo_description or outdated.")
		return
	}
	who := principal(request)
	publishes := func() (bool, error) {
		if input.Status != nil && *input.Status == domain.KBPublished {
			return true, nil
		}
		return a.store.KBTranslationPublished(request.Context(), who.Workspace.ID, request.PathValue("id"), request.PathValue("locale"))
	}
	// Marking it up to date or outdated changes nothing customers read.
	contentChanges := input.Title != nil || input.Slug != nil || input.BodyMD != nil || input.Status != nil || input.SEOTitle != nil || input.SEODesc != nil
	if contentChanges && a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, publishes) {
		return
	}
	translation, err := a.store.UpdateKBTranslation(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), request.PathValue("locale"),
		*input.UpdatedAt, store.KBTranslationChange{Title: input.Title, Slug: input.Slug, BodyMD: input.BodyMD, Status: input.Status, SEOTitle: input.SEOTitle,
			SEODescription: input.SEODesc, Outdated: input.Outdated})
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.writeKBTranslation(response, request, http.StatusOK, translation)
}

func (a *API) deleteKBTranslation(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, a.translationIsPublished(request, who)) {
		return
	}
	seen, ok := seenAt(response, request)
	if !ok {
		return
	}
	if err := a.store.DeleteKBTranslation(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), request.PathValue("locale"), seen); err != nil {
		writeKBError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreKBTranslation(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, a.translationIsPublished(request, who)) {
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	// A repeated press (same Idempotency-Key) answers the first restore again.
	articleID, locale := request.PathValue("id"), request.PathValue("locale")
	a.runOnce(response, request, http.StatusOK, restoreKBTranslationOperation, []string{articleID, locale, seen.UTC().Format(time.RFC3339Nano)}, func(ctx context.Context) (any, error) {
		translation, err := a.store.UndeleteKBTranslation(ctx, who.Workspace.ID, savedReplyActor(ctx, who), articleID, locale, seen)
		if errors.Is(err, domain.ErrKBTranslationNotFound) {
			writeError(response, http.StatusNotFound, "not_found", "The article has no translation in that language in the trash (GET /api/v1/kb/translations/trash).")
			return nil, errAnswered
		}
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		if !a.addressedKBTranslation(response, request, &translation) {
			return nil, errAnswered
		}
		return translation, nil
	})
}

func (a *API) listKBTranslationVersions(response http.ResponseWriter, request *http.Request) {
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
	items, next, err := a.store.KBTranslationVersions(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("locale"), limit, before)
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

func (a *API) restoreKBTranslationVersion(response http.ResponseWriter, request *http.Request) {
	version, err := strconv.Atoi(request.PathValue("version"))
	if err != nil || version < 1 {
		writeError(response, http.StatusNotFound, "not_found", "The translation has no version with that number.")
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, a.translationIsPublished(request, who)) {
		return
	}
	// A repeated press (same Idempotency-Key) answers the first restore again and adds no version.
	articleID, locale := request.PathValue("id"), request.PathValue("locale")
	a.runOnce(response, request, http.StatusOK, restoreKBTranslationVersionOperation, []string{articleID, locale, strconv.Itoa(version), seen.UTC().Format(time.RFC3339Nano)},
		func(ctx context.Context) (any, error) {
			translation, err := a.store.RestoreKBTranslationVersion(ctx, who.Workspace.ID, savedReplyActor(ctx, who), articleID, locale, version, seen)
			if err != nil {
				writeKBError(response, err)
				return nil, errAnswered
			}
			if !a.addressedKBTranslation(response, request, &translation) {
				return nil, errAnswered
			}
			return translation, nil
		})
}

func (a *API) putKBCollectionTranslation(response http.ResponseWriter, request *http.Request) {
	// A collection's name is the help center's navigation.
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, nil) {
		return
	}
	var input struct {
		Name        *string `json:"name"`
		Description string  `json:"description"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Name == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the collection's name in that language, and its description if it has one.")
		return
	}
	collection, err := a.store.PutKBCollectionTranslation(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("locale"), *input.Name, input.Description)
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, collection)
}

func (a *API) deleteKBCollectionTranslation(response http.ResponseWriter, request *http.Request) {
	if a.refuseUnlessHeld(response, request, principal(request), auth.PermKBArticlesPublish, nil) {
		return
	}
	collection, err := a.store.DeleteKBCollectionTranslation(request.Context(), principal(request).Workspace.ID, request.PathValue("id"), request.PathValue("locale"))
	if errors.Is(err, domain.ErrKBTranslationNotFound) {
		writeError(response, http.StatusNotFound, "not_found", "The collection has no name in that language.")
		return
	}
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, collection)
}
