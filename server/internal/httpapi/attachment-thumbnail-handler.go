package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/media"
)

// maxThumbnailSource is the most bytes of an attachment read to make its thumbnail: more than a message
// carries (25 MiB), so every stored picture is read whole.
const maxThumbnailSource = 32 << 20

var errNoAttachmentThumbnail = errors.New("no thumbnail")

// getAttachmentThumbnail serves a small picture of an image attachment: what the timeline and the gallery's
// strip show, instead of the original. It is made on the first request from the bytes (never the name or the
// declared type) and kept with the attachment; a file that is no readable picture is remembered as such.
func (a *API) getAttachmentThumbnail(response http.ResponseWriter, request *http.Request) {
	notFound := func() {
		writeError(response, http.StatusNotFound, "not_found", "The requested resource was not found.")
	}
	if a.config.Attachments == nil {
		notFound()
		return
	}
	ctx := request.Context()
	workspaceID := principal(request).Workspace.ID
	id := request.PathValue("id")
	_, key, thumb, err := a.store.AttachmentThumbnail(ctx, workspaceID, id)
	// Storage keys start with the owning workspace id; a row pointing at another workspace's bytes is never read.
	if err != nil || !strings.HasPrefix(key, workspaceID+"/") {
		notFound()
		return
	}
	var data []byte
	switch {
	case thumb != nil && *thumb == "":
		err = errNoAttachmentThumbnail
	case thumb != nil && strings.HasPrefix(*thumb, workspaceID+"/"):
		data, err = a.readStored(ctx, *thumb, maxThumbnailSource)
	default:
		data, err = a.makeAttachmentThumbnail(ctx, workspaceID, id, key)
	}
	if errors.Is(err, errNoAttachmentThumbnail) {
		writeError(response, http.StatusNotFound, "no_thumbnail", "This file has no thumbnail: it is not a picture the server can read. Show or download the original instead.")
		return
	}
	if err != nil {
		log.Printf("attachment %s: thumbnail: %v", id, err)
		notFound()
		return
	}
	// The server encoded it as a JPEG, or a PNG when the picture has transparency; the bytes say which.
	contentType := "image/jpeg"
	if bytes.HasPrefix(data, []byte("\x89PNG")) {
		contentType = "image/png"
	}
	header := response.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(data)))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("Cache-Control", "private, no-store")
	response.WriteHeader(http.StatusOK)
	if _, err := response.Write(data); err != nil {
		log.Printf("attachment %s: write thumbnail: %v", id, err)
	}
}

// makeAttachmentThumbnail makes, stores and records the thumbnail of an attachment. A file that is no
// picture the decoder reads, is over its size limits (8,192 pixels on a side, 40 megapixels), or would take
// more memory to decode than the thumbnail budget, is recorded as
// having none (errNoAttachmentThumbnail) so it is not read again on every view.
func (a *API) makeAttachmentThumbnail(ctx context.Context, workspaceID, id, key string) ([]byte, error) {
	original, err := a.readStored(ctx, key, maxThumbnailSource)
	if err != nil {
		return nil, err
	}
	info, err := media.InspectImage(original)
	var thumb []byte
	if err == nil {
		thumb, _, err = media.Thumbnail(ctx, info, original)
	}
	if errors.Is(err, media.ErrNotImage) || errors.Is(err, media.ErrImageTooLarge) || errors.Is(err, media.ErrNoThumbnail) {
		if err := a.store.SetAttachmentThumbnail(ctx, workspaceID, id, key, ""); err != nil {
			return nil, err
		}
		return nil, errNoAttachmentThumbnail
	}
	if err != nil {
		return nil, err
	}
	object, err := a.config.Attachments.Put(ctx, workspaceID, bytes.NewReader(thumb), int64(len(thumb)))
	if err != nil {
		return nil, err
	}
	if err := a.store.SetAttachmentThumbnail(ctx, workspaceID, id, key, object.Key); err != nil {
		return nil, err
	}
	return thumb, nil
}

// readStored reads stored bytes of at most limit.
func (a *API) readStored(ctx context.Context, key string, limit int64) ([]byte, error) {
	body, err := a.config.Attachments.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, limit))
}
