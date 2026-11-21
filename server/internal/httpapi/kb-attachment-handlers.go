package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// readKBAttachmentList reads the body of a PUT …/attachments; it reports false when it answered.
func readKBAttachmentList(response http.ResponseWriter, request *http.Request) ([]string, *time.Time, bool) {
	var input struct {
		MediaIDs  *[]string  `json:"media_ids"`
		UpdatedAt *time.Time `json:"updated_at"`
	}
	if err := decodeJSON(request, &input); err != nil || input.MediaIDs == nil {
		writeError(response, http.StatusBadRequest, "invalid_request",
			"Send media_ids: the ids of the files in the order to list them ([] removes them all), and the updated_at you last read when you have it.")
		return nil, nil, false
	}
	return *input.MediaIDs, input.UpdatedAt, true
}

// writeKBAttachmentError answers the refusals of a list of files, and every other error as writeKBError.
func writeKBAttachmentError(response http.ResponseWriter, err error) {
	var missing *domain.KBAttachmentMissingError
	switch {
	case errors.As(err, &missing):
		writeError(response, http.StatusNotFound, "file_not_found", "The media library has no file "+missing.ID+
			" outside the trash. GET /api/v1/kb/media?kind=file lists the files; upload one with POST /api/v1/kb/media-uploads and kind file.")
	case errors.Is(err, domain.ErrKBAttachmentsTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "too_large",
			"A file is at most 25 MiB and the files of one article come to at most 100 MiB. Remove a file from the list, or link a larger one from the text.")
	default:
		writeKBError(response, err)
	}
}

// setKBArticleAttachments sets the files an article lists; a published article keeps them in its draft.
func (a *API) setKBArticleAttachments(response http.ResponseWriter, request *http.Request) {
	ids, seen, ok := readKBAttachmentList(response, request)
	if !ok {
		return
	}
	who := principal(request)
	article, err := a.store.SetKBArticleAttachments(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), ids, seen)
	if err != nil {
		writeKBAttachmentError(response, err)
		return
	}
	a.answerKBArticle(response, request, article)
}

// setKBTranslationAttachments sets the files a translation lists. A translation has no draft, so on a
// published one the help center changes at once and the call needs the publish permission.
func (a *API) setKBTranslationAttachments(response http.ResponseWriter, request *http.Request) {
	ids, seen, ok := readKBAttachmentList(response, request)
	if !ok {
		return
	}
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesPublish, a.translationIsPublished(request, who)) {
		return
	}
	translation, err := a.store.SetKBTranslationAttachments(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), request.PathValue("locale"), ids, seen)
	if err != nil {
		writeKBAttachmentError(response, err)
		return
	}
	a.writeKBTranslation(response, request, http.StatusOK, translation)
}
