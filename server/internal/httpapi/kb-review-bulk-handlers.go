package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbBulkLimit is the most ids one bulk approve or bulk publish takes.
const kbBulkLimit = 50

// kbBulkResult answers one id of a bulk request: what it became, or the error a call for it alone gives.
type kbBulkResult struct {
	ID      string             `json:"id"`
	Article *domain.KBArticle  `json:"article,omitempty"`
	Review  *domain.KBReview   `json:"review,omitempty"`
	Error   *kbBulkResultError `json:"error,omitempty"`
}

type kbBulkResultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// itemErrorWriter keeps the error a handler helper writes for one item instead of sending it.
type itemErrorWriter struct {
	header http.Header
	body   []byte
}

func (w *itemErrorWriter) Header() http.Header { return w.header }
func (w *itemErrorWriter) WriteHeader(int)     {}
func (w *itemErrorWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}

// kbItemError is the code and message writeKBError answers err with.
func kbItemError(err error) *kbBulkResultError {
	writer := &itemErrorWriter{header: http.Header{}}
	writeKBError(writer, err)
	return parsedItemError(writer)
}

// parsedItemError is the code and message of the error written to writer.
func parsedItemError(writer *itemErrorWriter) *kbBulkResultError {
	var answer struct {
		Error kbBulkResultError `json:"error"`
	}
	if json.Unmarshal(writer.body, &answer) != nil || answer.Error.Code == "" {
		return &kbBulkResultError{Code: "internal_error", Message: "This item could not be done."}
	}
	return &answer.Error
}

// kbBulkIDs reads 1 to kbBulkLimit distinct ids from the list named field; false when it answered 400.
func kbBulkIDs(response http.ResponseWriter, ids []string, field string) bool {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	if len(ids) == 0 || len(ids) > kbBulkLimit || len(slices.Compact(sorted)) != len(ids) {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send "+field+": 1 to 50 different ids.")
		return false
	}
	return true
}

// approveKBReviews approves several open reviews, each as POST /api/v1/kb/reviews/{id}/approve would.
func (a *API) approveKBReviews(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	var input struct {
		ReviewIDs []string `json:"review_ids"`
		Note      string   `json:"note"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send review_ids (1 to 50) and an optional one-line note.")
		return
	}
	if !kbBulkIDs(response, input.ReviewIDs, "review_ids") {
		return
	}
	a.runOnce(response, request, http.StatusOK, approveKBReviewsOperation, append([]string{input.Note}, input.ReviewIDs...), func(ctx context.Context) (any, error) {
		results := make([]kbBulkResult, 0, len(input.ReviewIDs))
		for _, id := range input.ReviewIDs {
			review, err := a.store.DecideKBReview(ctx, who.Workspace.ID, savedReplyActor(ctx, who), id,
				store.KBReviewDecision{State: domain.KBReviewApproved, Note: input.Note})
			if err != nil {
				results = append(results, kbBulkResult{ID: id, Error: kbItemError(err)})
				continue
			}
			results = append(results, kbBulkResult{ID: id, Review: &review})
		}
		return map[string]any{"results": results}, nil
	})
}

// publishKBArticles publishes several articles: a draft article goes live, a published one's unpublished
// changes replace its text. Each is checked on its own, approval included.
func (a *API) publishKBArticles(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	var input struct {
		ArticleIDs []string `json:"article_ids"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send article_ids (1 to 50).")
		return
	}
	if !kbBulkIDs(response, input.ArticleIDs, "article_ids") {
		return
	}
	home, ok := a.helpCenterHome(response, request)
	if !ok {
		return
	}
	a.runOnce(response, request, http.StatusOK, publishKBArticlesOperation, input.ArticleIDs, func(ctx context.Context) (any, error) {
		results := make([]kbBulkResult, 0, len(input.ArticleIDs))
		for _, id := range input.ArticleIDs {
			article, err := a.publishOneKBArticle(ctx, who, id)
			if err != nil {
				results = append(results, kbBulkResult{ID: id, Error: kbItemError(err)})
				continue
			}
			if article == nil {
				results = append(results, kbBulkResult{ID: id, Error: &kbBulkResultError{Code: "nothing_to_publish",
					Message: "There are no unpublished changes: the published text is the current one."}})
				continue
			}
			a.warnMissingKBMedia(ctx, who.Workspace.ID, article)
			addressKBArticle(home, &article.KBArticleSummary)
			if !a.addressKBArticleTranslations(response, request, home, article) {
				return nil, errAnswered
			}
			results = append(results, kbBulkResult{ID: id, Article: article})
		}
		return map[string]any{"results": results}, nil
	})
}

// publishOneKBArticle publishes the article id as it is now; nil, nil when nothing is unpublished. The
// store refuses it without an approval of its current text where one is needed, so the updated_at read
// here cannot let a later edit through.
func (a *API) publishOneKBArticle(ctx context.Context, who auth.Principal, id string) (*domain.KBArticle, error) {
	current, err := a.store.GetKBArticle(ctx, who.Workspace.ID, id)
	if err != nil {
		return nil, err
	}
	actor := savedReplyActor(ctx, who)
	var article domain.KBArticle
	switch {
	case current.Status != domain.KBPublished:
		published := domain.KBPublished
		article, err = a.store.UpdateKBArticle(ctx, who.Workspace.ID, actor, id, current.UpdatedAt, store.KBArticleChange{Status: &published})
	case current.HasDraft:
		article, err = a.store.PublishKBArticleDraft(ctx, who.Workspace.ID, actor, id, current.UpdatedAt)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &article, nil
}
