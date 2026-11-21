package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// publishKBArticleDraft puts the unpublished changes of a published article on the help center.
func (a *API) publishKBArticleDraft(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	// A repeated press (same Idempotency-Key) answers the first publish again and adds no version.
	articleID := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, publishKBArticleDraftOperation, []string{articleID, seen.UTC().Format(time.RFC3339Nano)},
		func(ctx context.Context) (any, error) {
			article, err := a.store.PublishKBArticleDraft(ctx, who.Workspace.ID, savedReplyActor(ctx, who), articleID, seen)
			if err != nil {
				writeKBError(response, err)
				return nil, errAnswered
			}
			a.warnMissingKBMedia(ctx, who.Workspace.ID, &article)
			if !a.addressedKBArticle(response, request, &article) {
				return nil, errAnswered
			}
			return article, nil
		})
}

// discardKBArticleDraft throws away the unpublished changes of a published article; the help center
// already shows the published text, so it needs no publish permission.
func (a *API) discardKBArticleDraft(response http.ResponseWriter, request *http.Request) {
	seen, ok := seenAt(response, request)
	if !ok {
		return
	}
	who := principal(request)
	article, err := a.store.DiscardKBArticleDraft(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), seen)
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.answerKBArticle(response, request, article)
}

// draftedKBArticle answers GET ?version=draft: the article as it will be once its unpublished changes
// are published. It reports false when it answered with a refusal.
func draftedKBArticle(response http.ResponseWriter, request *http.Request, article *domain.KBArticle) bool {
	switch request.URL.Query().Get("version") {
	case "":
		return true
	case "draft":
		if article.Draft == nil {
			writeKBError(response, domain.ErrKBDraftNotFound)
			return false
		}
		*article = article.AsDrafted()
		return true
	default:
		writeError(response, http.StatusBadRequest, "invalid_request", "version is draft, or leave it out for the published article.")
		return false
	}
}

// answerKBArticle addresses the article and its translations below the help center home and writes it.
func (a *API) answerKBArticle(response http.ResponseWriter, request *http.Request, article domain.KBArticle) {
	if a.addressedKBArticle(response, request, &article) {
		writeJSON(response, http.StatusOK, article)
	}
}

// addressedKBArticle gives the article and its translations their help center addresses; it reports false
// when it answered with an error.
func (a *API) addressedKBArticle(response http.ResponseWriter, request *http.Request, article *domain.KBArticle) bool {
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return false
	}
	addressKBArticle(home, &article.KBArticleSummary)
	return a.addressKBArticleTranslations(response, request, home, article)
}
