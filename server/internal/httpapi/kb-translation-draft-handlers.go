package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// publishKBTranslationDraft puts the unpublished changes of a published translation on the help center.
func (a *API) publishKBTranslationDraft(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, nil) {
		return
	}
	seen, ok := seenInBody(response, request)
	if !ok {
		return
	}
	// A repeated press (same Idempotency-Key) answers the first publish again and adds no version.
	articleID, locale := request.PathValue("id"), request.PathValue("locale")
	a.runOnce(response, request, http.StatusOK, publishKBTranslationDraftOperation, []string{articleID, locale, seen.UTC().Format(time.RFC3339Nano)},
		func(ctx context.Context) (any, error) {
			translation, err := a.store.PublishKBTranslationDraft(ctx, who.Workspace.ID, savedReplyActor(ctx, who), articleID, locale, seen)
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

// discardKBTranslationDraft throws away the unpublished changes of a published translation; the help
// center already shows the published text, so it needs no publish permission.
func (a *API) discardKBTranslationDraft(response http.ResponseWriter, request *http.Request) {
	seen, ok := seenAt(response, request)
	if !ok {
		return
	}
	who := principal(request)
	translation, err := a.store.DiscardKBTranslationDraft(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who),
		request.PathValue("id"), request.PathValue("locale"), seen)
	if err != nil {
		writeKBError(response, err)
		return
	}
	a.writeKBTranslation(response, request, http.StatusOK, translation)
}
