package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// importKBImage stores an image the editor found by its web address (pasted from another page or a
// document) in the media library, so the article never loads it from the other site. The fetch is
// the email image fetch: it refuses private, loopback and metadata addresses, also after DNS and on
// every redirect, and caps the bytes.
func (a *API) importKBImage(response http.ResponseWriter, request *http.Request) {
	if a.config.Attachments == nil {
		writeError(response, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server.")
		return
	}
	var input struct {
		URL  string `json:"url"`
		Name string `json:"name"`
		Alt  string `json:"alt"`
		Keep bool   `json:"keep"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send JSON with the image's url.")
		return
	}
	target, err := url.Parse(strings.TrimSpace(input.URL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "url is the image's full http or https address; upload a file or a data: image to POST /api/v1/kb/images.")
		return
	}
	upload := domain.KBImageUpload{Name: input.Name, Alt: input.Alt, Keep: input.Keep}
	if upload.Name == "" {
		upload.Name = strings.TrimSuffix(path.Base(target.Path), path.Ext(target.Path))
		if upload.Name == "." || upload.Name == "/" {
			upload.Name = ""
		}
	}
	who := principal(request)
	release, ok := a.mailImageLimits.acquire(request.Context(), who.Workspace.ID)
	if !ok {
		if request.Context().Err() == nil {
			response.Header().Set("Retry-After", mailImageRetryAfterSecs)
			writeError(response, http.StatusServiceUnavailable, "busy", "Too many images are being fetched; try again in a moment.")
		}
		return
	}
	image, err := a.mailImageFetcher.Fetch(request.Context(), target.String())
	release()
	if err != nil {
		writeKBImageImportError(response, err)
		return
	}
	a.storeKBImage(response, request, image.Data, upload, func(error) {
		writeError(response, http.StatusUnsupportedMediaType, "not_an_image", "The address is not a PNG, JPEG, GIF or WebP image the library can read; nothing was stored.")
	})
}

func writeKBImageImportError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, netguard.ErrBlockedAddress), errors.Is(err, mailimage.ErrPortRefused):
		writeError(response, http.StatusForbidden, "address_refused", "This image is on a private or internal address, or not on a web port; nothing was stored.")
	case errors.Is(err, mailimage.ErrNotImage):
		writeError(response, http.StatusUnsupportedMediaType, "not_an_image", "The address is not a PNG, JPEG, GIF or WebP image; nothing was stored.")
	case errors.Is(err, mailimage.ErrTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "An image holds at most 5 MiB; nothing was stored.")
	default:
		writeError(response, http.StatusBadGateway, "image_unavailable", "The image could not be fetched from its address; nothing was stored.")
	}
}

// storeKBImage puts image bytes into the media library: the bytes decide the type, metadata leaves
// before anything is compared, the same bytes give the item already stored, and the plan's storage
// cap applies. badImage answers bytes that are no image the library takes.
func (a *API) storeKBImage(response http.ResponseWriter, request *http.Request, data []byte, upload domain.KBImageUpload, badImage func(error)) {
	who := principal(request)
	if len(data) > domain.MaxKBImageBytes {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "An image holds at most 5 MiB; nothing was stored.")
		return
	}
	// The bytes decide the type, never the declared Content-Type, so nothing else is served as an image.
	info, err := media.InspectImage(data)
	if err != nil {
		badImage(err)
		return
	}
	// Location, camera and comments leave before anything is stored or compared.
	data, err = media.StripMetadata(info, data)
	if err != nil {
		badImage(err)
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
