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
	// MailImageLifetime is how long a cached image is served before the proxy asks the source again, and
	// the longest it is kept: the sweep deletes older rows. The browser may keep it as long, since its
	// address is signed and never changes.
	MailImageLifetime = 30 * 24 * time.Hour
	// DefaultMailImageCacheBytes caps the cache (one workspace's share and, in the sweep, the whole).
	DefaultMailImageCacheBytes int64 = 1 << 30

	mailImageKeyName        = "mail_image_key"
	mailImageRetryAfterSecs = "2"
)

// mailImageKey loads the key that signs proxy addresses from the database, where the first process to
// start stored it: every replica, deploy and restart signs and verifies with the same key. If the database
// cannot give it, this process signs with a key of its own and says so; its images then fail on other
// processes until the database answers at the next start.
func (a *API) mailImageKey() []byte {
	key, err := a.store.ServerSecret(context.Background(), mailImageKeyName)
	if err == nil {
		return key
	}
	log.Printf("mail image: signing key from the database failed, using a key private to this process: %v", err)
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("mail image key: " + err.Error())
	}
	return key
}

func (a *API) mailImageCacheBytes() int64 {
	if a.config.MailImageCacheBytes > 0 {
		return a.config.MailImageCacheBytes
	}
	return DefaultMailImageCacheBytes
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
	if found && a.now().Sub(cached.FetchedAt) < MailImageLifetime && a.serveCachedImage(response, request, workspaceID, urlHash, cached) {
		return
	}
	release, ok := a.mailImageLimits.acquire(request.Context(), workspaceID)
	if !ok {
		if request.Context().Err() == nil {
			response.Header().Set("Retry-After", mailImageRetryAfterSecs)
			writeError(response, http.StatusServiceUnavailable, "busy", "Too many images are being fetched; try again in a moment.")
		}
		return
	}
	defer release()
	image, err := a.mailImageFetcher.Fetch(request.Context(), target)
	if err != nil {
		// A source that went away must not hide an image the team has already seen (until the sweep
		// removes it at the end of its lifetime).
		if found && a.serveCachedImage(response, request, workspaceID, urlHash, cached) {
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

// cacheMailImage keeps the image unless the workspace's cache would pass its cap: then the image is
// served this once and fetched again next time.
func (a *API) cacheMailImage(ctx context.Context, workspaceID, urlHash string, image mailimage.Image) error {
	used, err := a.store.CachedMailImageBytes(ctx, workspaceID)
	if err != nil {
		return err
	}
	if used+int64(len(image.Data)) > a.mailImageCacheBytes() {
		return nil
	}
	object, err := a.config.Attachments.Put(ctx, workspaceID, bytes.NewReader(image.Data), int64(len(image.Data)))
	if err != nil {
		return err
	}
	return a.store.SaveCachedMailImage(ctx, workspaceID, urlHash, image.ContentType, object)
}

// serveCachedImage streams a cached image and reports false when its bytes are gone from storage, so
// the caller fetches it again.
func (a *API) serveCachedImage(response http.ResponseWriter, request *http.Request, workspaceID, urlHash string, cached store.CachedMailImage) bool {
	if a.config.Attachments == nil {
		return false
	}
	body, err := a.config.Attachments.Open(request.Context(), cached.StorageKey)
	if err != nil {
		return false
	}
	defer body.Close()
	if err := a.store.TouchCachedMailImage(request.Context(), workspaceID, urlHash); err != nil {
		log.Printf("mail image: note use: %v", err)
	}
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
	header.Set("Cache-Control", "private, max-age="+strconv.Itoa(int(MailImageLifetime/time.Second))+", immutable")
	response.WriteHeader(http.StatusOK)
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("mail image: stream: %v", err)
	}
}

func writeMailImageError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, netguard.ErrBlockedAddress), errors.Is(err, mailimage.ErrPortRefused):
		writeError(response, http.StatusForbidden, "address_refused", "This image is on a private or internal address, or not on a web port.")
	case errors.Is(err, mailimage.ErrNotImage):
		writeError(response, http.StatusUnsupportedMediaType, "not_an_image", "Only PNG, JPEG, GIF and WebP images are shown.")
	case errors.Is(err, mailimage.ErrTooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "image_too_large", "This image is over the size limit.")
	default:
		writeError(response, http.StatusBadGateway, "image_unavailable", "The image could not be fetched.")
	}
}
