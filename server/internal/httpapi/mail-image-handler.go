package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
	"github.com/colliedesk/colliedesk/server/internal/netguard"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	// mailImageLifetime is how long a cached image is served before the proxy asks the source again.
	// The browser may keep it as long, since its address is signed and never changes.
	mailImageLifetime       = 30 * 24 * time.Hour
	defaultMailImageSlots   = 8
	mailImageRetryAfterSecs = "2"
)

// newMailImageKey makes the key that signs proxy addresses. It lives in this process: a restart
// invalidates the addresses of open tabs, which reload them with the next timeline read.
func newMailImageKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("mail image key: " + err.Error())
	}
	return key
}

// presentMessageHTML points the remote images of incoming mail at the image proxy, for a person's
// session. An agent token reads the stored HTML as it arrived.
func (a *API) presentMessageHTML(workspaceID string, markup *string) *string {
	if markup == nil {
		return nil
	}
	rewritten := a.mailImageSigner.Rewrite(workspaceID, *markup)
	return &rewritten
}

// mailImage serves one remote image of an email on the viewer's behalf, so the sender sees the
// server's address and not the agent's. The address is signed, only a signed-in member may use it, and
// the fetch refuses private networks (see mailimage.Fetcher). A fetched image is kept in attachment
// storage and served from there afterwards.
func (a *API) mailImage(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	target, signature := request.URL.Query().Get("u"), request.URL.Query().Get("s")
	if target == "" || !a.mailImageSigner.Valid(workspaceID, target, signature) {
		writeError(response, http.StatusForbidden, "forbidden", "This image link is not valid.")
		return
	}
	hash := sha256.Sum256([]byte(target))
	urlHash := hex.EncodeToString(hash[:])
	cached, found, err := a.store.GetCachedMailImage(request.Context(), workspaceID, urlHash)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if found && a.now().Sub(cached.FetchedAt) < mailImageLifetime && a.serveCachedImage(response, request, cached) {
		return
	}
	select {
	case a.mailImageSlots <- struct{}{}:
		defer func() { <-a.mailImageSlots }()
	default:
		response.Header().Set("Retry-After", mailImageRetryAfterSecs)
		writeError(response, http.StatusServiceUnavailable, "busy", "Too many images are being fetched; try again in a moment.")
		return
	}
	image, err := a.mailImageFetcher.Fetch(request.Context(), target)
	if err != nil {
		// A source that went away must not hide an image the team has already seen.
		if found && a.serveCachedImage(response, request, cached) {
			return
		}
		writeMailImageError(response, err)
		return
	}
	if a.config.Attachments != nil {
		// A failed save costs the next view another fetch; this view still gets its image.
		if err := a.cacheMailImage(request.Context(), workspaceID, urlHash, image); err != nil {
			log.Printf("mail image: cache: %v", err)
		}
	}
	writeMailImage(response, image.ContentType, int64(len(image.Data)), bytes.NewReader(image.Data))
}

func (a *API) cacheMailImage(ctx context.Context, workspaceID, urlHash string, image mailimage.Image) error {
	object, err := a.config.Attachments.Put(ctx, workspaceID, bytes.NewReader(image.Data), int64(len(image.Data)))
	if err != nil {
		return err
	}
	return a.store.SaveCachedMailImage(ctx, workspaceID, urlHash, image.ContentType, object)
}

// serveCachedImage streams a cached image and reports false when its bytes are gone from storage, so
// the caller fetches it again.
func (a *API) serveCachedImage(response http.ResponseWriter, request *http.Request, cached store.CachedMailImage) bool {
	if a.config.Attachments == nil {
		return false
	}
	body, err := a.config.Attachments.Open(request.Context(), cached.StorageKey)
	if err != nil {
		return false
	}
	defer body.Close()
	writeMailImage(response, cached.ContentType, cached.Size, body)
	return true
}

func writeMailImage(response http.ResponseWriter, contentType string, size int64, body io.Reader) {
	header := response.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.FormatInt(size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	// Nothing the image holds may run or load, even if a browser opened it as a page.
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Private: the address works only with a session. Immutable: the signed address never changes meaning.
	header.Set("Cache-Control", "private, max-age="+strconv.Itoa(int(mailImageLifetime/time.Second))+", immutable")
	response.WriteHeader(http.StatusOK)
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("mail image: stream: %v", err)
	}
}

func writeMailImageError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, netguard.ErrBlockedAddress):
		writeError(response, http.StatusForbidden, "address_refused", "This image is on a private or internal address.")
	case errors.Is(err, mailimage.ErrNotImage):
		writeError(response, http.StatusUnsupportedMediaType, "not_an_image", "Only PNG, JPEG, GIF and WebP images are shown.")
	case errors.Is(err, mailimage.ErrTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "image_too_large", "This image is over the size limit.")
	default:
		writeError(response, http.StatusBadGateway, "image_unavailable", "The image could not be fetched.")
	}
}
