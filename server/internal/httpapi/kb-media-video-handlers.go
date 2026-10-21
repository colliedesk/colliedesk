package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/media"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	// kbPartReadDeadline is the most one part may take to arrive (8 MiB in 120 s is about 0.55 Mbit/s),
	// so a client sending a byte now and then cannot hold a connection.
	kbPartReadDeadline = 120 * time.Second
	// maxKBPartsAtOnce is the most parts a workspace sends at the same moment.
	maxKBPartsAtOnce = 6
)

// kbUploadGate serializes the parts of one upload and counts the parts each workspace is sending.
// The server runs as one process (docs/internals/knowledge-base.md), so memory is enough.
type kbUploadGate struct {
	mu      sync.Mutex
	writing map[string]bool
	parts   map[string]int
}

// enter claims upload id of workspace for one part; false means another part of it, or too many parts
// of the workspace, are being written.
func (g *kbUploadGate) enter(workspaceID, id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.writing == nil {
		g.writing, g.parts = map[string]bool{}, map[string]int{}
	}
	if g.writing[id] || g.parts[workspaceID] >= maxKBPartsAtOnce {
		return false
	}
	g.writing[id] = true
	g.parts[workspaceID]++
	return true
}

func (g *kbUploadGate) leave(workspaceID, id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.writing, id)
	if g.parts[workspaceID]--; g.parts[workspaceID] <= 0 {
		delete(g.parts, workspaceID)
	}
}

func (a *API) kbVideoMaxBytes() int64 {
	if a.config.KBVideoMaxBytes > 0 {
		return a.config.KBVideoMaxBytes
	}
	return domain.MaxKBVideoBytes
}

// kbStaging is the attachment storage's staging of uploads in parts; nil when the backend has none.
func (a *API) kbStaging() attachments.Staging {
	staging, _ := a.config.Attachments.(attachments.Staging)
	return staging
}

func writeKBUploadError(response http.ResponseWriter, err error) {
	var offset *domain.KBMediaOffsetError
	var codec *media.UnsupportedCodecError
	switch {
	case errors.As(err, &offset):
		response.Header().Set("Upload-Offset", strconv.FormatInt(offset.Offset, 10))
		writeJSON(response, http.StatusConflict, map[string]any{"error": errorBody(response, map[string]any{"code": "upload_offset_mismatch",
			"message": sentence(err) + ".", "offset": offset.Offset})})
	case errors.Is(err, domain.ErrKBMediaUploadNotFound):
		writeError(response, http.StatusNotFound, "not_found", "The upload was not found; it may have expired after 24 hours.")
	case errors.Is(err, domain.ErrKBMediaUploadLimit):
		writeError(response, http.StatusTooManyRequests, "too_many_uploads", sentence(err)+".")
	case errors.Is(err, domain.ErrKBMediaUploadIncomplete):
		writeError(response, http.StatusConflict, "upload_incomplete", sentence(err)+".")
	case errors.Is(err, media.ErrHEVC), errors.As(err, &codec):
		writeError(response, http.StatusBadRequest, "unsupported_codec", sentence(err)+".")
	case errors.Is(err, media.ErrNotVideo), errors.Is(err, media.ErrVideoDamaged), errors.Is(err, media.ErrVideoUnsafeTrack), errors.Is(err, media.ErrVideoTooLarge):
		writeError(response, http.StatusBadRequest, "invalid_request", sentence(err)+"; nothing was stored.")
	default:
		writeKBError(response, err)
	}
}

func (a *API) createKBMediaUpload(response http.ResponseWriter, request *http.Request) {
	staging := a.kbStaging()
	if staging == nil {
		writeError(response, http.StatusServiceUnavailable, "storage_unavailable", "This server's file storage cannot take videos.")
		return
	}
	var input struct {
		Size *int64 `json:"size"`
		Name string `json:"name"`
		Alt  string `json:"alt"`
		Keep bool   `json:"keep"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Size == nil || *input.Size <= 0 {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send size, the video's bytes, and name, alt and keep when wanted.")
		return
	}
	if max := a.kbVideoMaxBytes(); *input.Size > max {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("A video holds at most %d MiB on this server; nothing was stored.", max>>20))
		return
	}
	who := principal(request)
	a.kbUploadStart.Lock()
	defer a.kbUploadStart.Unlock()
	open, reserved, err := a.store.KBMediaUploadsInProgress(request.Context())
	if err != nil {
		writeKBUploadError(response, err)
		return
	}
	if open >= domain.MaxKBMediaUploadsServer {
		writeError(response, http.StatusServiceUnavailable, "server_busy", "This server has too many videos uploading; try again in a few minutes. Nothing was stored.")
		return
	}
	// The disk keeps room for this video, every upload in progress on the server and a margin.
	if space, ok := a.config.Attachments.(attachments.SpaceReporter); ok {
		if available, err := space.Available(); err == nil && available < *input.Size+reserved+domain.KBMediaDiskMargin {
			writeError(response, http.StatusInsufficientStorage, "insufficient_disk", "The server's disk has no room for this video; nothing was stored.")
			return
		}
	}
	upload, _, err := a.store.CreateKBMediaUpload(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who),
		domain.NewKBMediaUpload{Size: *input.Size, KBImageUpload: domain.KBImageUpload{Name: input.Name, Alt: input.Alt, Keep: input.Keep}})
	if err != nil {
		writeKBUploadError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, upload)
}

func (a *API) getKBMediaUpload(response http.ResponseWriter, request *http.Request) {
	upload, err := a.store.GetKBMediaUpload(request.Context(), principal(request).Workspace.ID, request.PathValue("id"))
	if err != nil {
		writeKBUploadError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, upload)
}

var contentRange = regexp.MustCompile(`^bytes (\d{1,15})-(\d{1,15})/(\d{1,15})$`)

func (a *API) putKBMediaUploadPart(response http.ResponseWriter, request *http.Request) {
	staging := a.kbStaging()
	who := principal(request)
	id := request.PathValue("id")
	match := contentRange.FindStringSubmatch(request.Header.Get("Content-Range"))
	if staging == nil || match == nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "Send the part's bytes with Content-Range: bytes <first>-<last>/<size>.")
		return
	}
	first, _ := strconv.ParseInt(match[1], 10, 64)
	last, _ := strconv.ParseInt(match[2], 10, 64)
	total, _ := strconv.ParseInt(match[3], 10, 64)
	length := last - first + 1
	if length <= 0 || length > domain.KBMediaPartSize || (request.ContentLength >= 0 && request.ContentLength != length) {
		writeError(response, http.StatusBadRequest, "invalid_request", fmt.Sprintf("A part holds 1 to %d bytes, and Content-Length matches its Content-Range.", domain.KBMediaPartSize))
		return
	}
	if !a.kbUploads.enter(who.Workspace.ID, id) {
		writeError(response, http.StatusTooManyRequests, "too_many_parts", "Another part of this upload, or too many parts of this workspace, are being sent; send one part at a time.")
		return
	}
	defer a.kbUploads.leave(who.Workspace.ID, id)
	upload, err := a.store.GetKBMediaUpload(request.Context(), who.Workspace.ID, id)
	if err != nil {
		writeKBUploadError(response, err)
		return
	}
	switch {
	case upload.MediaID != nil:
		writeError(response, http.StatusConflict, "upload_complete", "This upload is complete.")
		return
	case total != upload.Size || last >= upload.Size:
		writeError(response, http.StatusBadRequest, "invalid_request", fmt.Sprintf("The upload is %d bytes; Content-Range must say /%d.", upload.Size, upload.Size))
		return
	case first != upload.Offset:
		writeKBUploadError(response, &domain.KBMediaOffsetError{Offset: upload.Offset})
		return
	}
	// A client sending a byte now and then cannot hold the connection.
	_ = http.NewResponseController(response).SetReadDeadline(time.Now().Add(kbPartReadDeadline))
	path, err := staging.StagingFile(upload.ID)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if err := writeKBPart(path, first, http.MaxBytesReader(response, request.Body, length), length); err != nil {
		a.dropCancelledParts(who.Workspace.ID, upload.ID)
		writeError(response, http.StatusBadRequest, "invalid_request", "The part did not arrive whole; send it again from byte "+strconv.FormatInt(first, 10)+".")
		return
	}
	recorded, err := a.store.RecordKBMediaUploadPart(request.Context(), who.Workspace.ID, upload.ID, first, last+1)
	if errors.Is(err, domain.ErrKBMediaUploadNotFound) {
		a.dropCancelledParts(who.Workspace.ID, upload.ID)
	}
	if err != nil {
		writeKBUploadError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, recorded)
}

// writeKBPart writes exactly length bytes of body into the staged file at offset, first cutting off
// whatever a part that failed after writing left past offset.
func writeKBPart(path string, offset int64, body io.Reader, length int64) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o640)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(offset); err != nil {
		return err
	}
	written, err := io.Copy(io.NewOffsetWriter(file, offset), io.LimitReader(body, length))
	if err == nil && written != length {
		err = io.ErrUnexpectedEOF
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		_ = file.Truncate(offset)
	}
	return err
}

func (a *API) cancelKBMediaUpload(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	id := request.PathValue("id")
	// No wait for a part being written (a page that goes away cannot retry): that part finds the upload
	// gone when it records itself and removes the file it wrote (dropCancelledParts).
	if err := a.store.CancelKBMediaUpload(request.Context(), who.Workspace.ID, id); err != nil {
		writeKBUploadError(response, err)
		return
	}
	if staging := a.kbStaging(); staging != nil {
		if err := staging.RemoveStaged(id); err != nil {
			log.Printf("knowledge base upload %s: remove its parts: %v", id, err)
		}
	}
	response.WriteHeader(http.StatusNoContent)
}

// dropCancelledParts removes the file of an upload cancelled while one of its parts was being written:
// writing the part made the file again after the cancel removed it.
func (a *API) dropCancelledParts(workspaceID, id string) {
	staging := a.kbStaging()
	if staging == nil {
		return
	}
	// A new context: the request's may be over (the client went away), and the check must still run.
	if _, err := a.store.GetKBMediaUpload(context.Background(), workspaceID, id); !errors.Is(err, domain.ErrKBMediaUploadNotFound) {
		return
	}
	if err := staging.RemoveStaged(id); err != nil {
		log.Printf("knowledge base upload %s: remove the parts of a cancelled upload: %v", id, err)
	}
}

func (a *API) completeKBMediaUpload(response http.ResponseWriter, request *http.Request) {
	staging := a.kbStaging()
	who := principal(request)
	id := request.PathValue("id")
	upload, err := a.store.GetKBMediaUpload(request.Context(), who.Workspace.ID, id)
	if err != nil || staging == nil {
		writeKBUploadError(response, domain.ErrKBMediaUploadNotFound)
		return
	}
	actor := savedReplyActor(request.Context(), who)
	if upload.MediaID != nil {
		// A repeated complete answers the item the first one made.
		media, _, err := a.store.CompleteKBMediaUpload(request.Context(), who.Workspace.ID, actor, id, store.NewKBVideo{})
		if err != nil {
			writeKBUploadError(response, err)
			return
		}
		writeJSON(response, http.StatusCreated, media)
		return
	}
	if upload.Offset != upload.Size {
		writeKBUploadError(response, domain.ErrKBMediaUploadIncomplete)
		return
	}
	if !a.kbUploads.enter(who.Workspace.ID, id) {
		writeError(response, http.StatusTooManyRequests, "too_many_parts", "A part of this upload is being sent; complete it once every part arrived.")
		return
	}
	defer a.kbUploads.leave(who.Workspace.ID, id)
	path, err := staging.StagingFile(id)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		_ = a.store.CancelKBMediaUpload(request.Context(), who.Workspace.ID, id)
		writeError(response, http.StatusConflict, "upload_lost", "The parts of this upload are gone from the server; start the upload again.")
		return
	}
	video, err := checkKBVideo(path, upload.Size)
	if err != nil {
		// The bytes cannot become a library item; the upload goes with them.
		_ = a.store.CancelKBMediaUpload(request.Context(), who.Workspace.ID, id)
		_ = staging.RemoveStaged(id)
		writeKBUploadError(response, err)
		return
	}
	// Whatever the database can refuse is checked before the file is renamed into storage, so a refusal
	// keeps the parts for a retry (the check at completion stays authoritative).
	if err := a.store.CheckKBVideoAdmission(request.Context(), who.Workspace.ID, upload.Size); err != nil {
		writeKBUploadError(response, err)
		return
	}
	object, err := staging.Adopt(request.Context(), who.Workspace.ID, path)
	if err != nil {
		writeInternalError(response, err)
		return
	}
	// The same bytes already stored give their item; the rename put an identical file in their place.
	item, _, err := a.store.CompleteKBMediaUpload(request.Context(), who.Workspace.ID, actor, id, store.NewKBVideo{Object: object,
		ContentType: video.ContentType, Width: video.Width, Height: video.Height, DurationMS: video.DurationMS, Codecs: video.Codecs})
	if err != nil {
		if discardErr := a.store.DiscardObject(request.Context(), who.Workspace.ID, object.Key); discardErr != nil {
			log.Printf("knowledge base upload %s: discard unrecorded bytes: %v", id, discardErr)
		}
		writeKBUploadError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, item)
}

// checkKBVideo judges the staged file and neutralises it in place.
func checkKBVideo(path string, size int64) (media.Video, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return media.Video{}, err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || info.Size() != size {
		return media.Video{}, domain.ErrKBMediaUploadIncomplete
	}
	video, err := media.InspectVideo(file, size)
	if err != nil {
		return media.Video{}, err
	}
	if err := media.NeutraliseVideo(file, size); err != nil {
		return media.Video{}, err
	}
	return video, file.Sync()
}

// getKBMediaFile streams the bytes of a library item, with ranges, so a video seeks: an id always
// names the same bytes, so any range of them may be cached for a year.
func (a *API) getKBMediaFile(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	item, err := a.store.GetKBMedia(request.Context(), workspaceID, request.PathValue("id"))
	if err != nil || a.config.Attachments == nil || !strings.HasPrefix(item.StorageKey, workspaceID+"/") {
		if err != nil && !errors.Is(err, domain.ErrKBMediaNotFound) {
			writeStoreError(response, err)
			return
		}
		writeKBMediaError(response, domain.ErrKBMediaNotFound)
		return
	}
	disposition := "inline"
	if request.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	serveKBMediaBytes(response, request, a.config.Attachments, item.StorageKey, item.ContentType, item.Size, item.SHA256, disposition, downloadName(item))
}

// downloadName is the file name a download of an item gets.
func downloadName(item store.StoredKBMedia) string {
	name := strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`"\/`, r) {
			return '_'
		}
		return r
	}, item.Name)
	if name == "" {
		name = item.ID
	}
	return name + map[string]string{"video/mp4": ".mp4", "video/webm": ".webm", "image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[item.ContentType]
}

// serveKBMediaBytes serves stored bytes that never change under their key, with ranges when the backend
// can seek (local disk), and nothing in them able to run.
func serveKBMediaBytes(response http.ResponseWriter, request *http.Request, objects attachments.Storage, key, contentType string, size int64, sha, disposition, name string) {
	if !attachments.Serve(response, request, objects, attachments.Served{Key: key, ContentType: contentType, Size: size, ETag: sha,
		Cache: "private, max-age=31536000, immutable", Disposition: disposition + `; filename="` + name + `"`}) {
		writeKBMediaError(response, domain.ErrKBMediaNotFound)
	}
}

func (a *API) putKBVideoPoster(response http.ResponseWriter, request *http.Request) {
	if a.config.Attachments == nil {
		writeError(response, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured on this server.")
		return
	}
	who := principal(request)
	data, err := io.ReadAll(io.LimitReader(request.Body, domain.MaxKBImageBytes+1))
	if err != nil || len(data) > domain.MaxKBImageBytes {
		writeError(response, http.StatusRequestEntityTooLarge, "too_large", "A poster holds at most 5 MiB; nothing was stored.")
		return
	}
	info, err := media.InspectImage(data)
	if err == nil {
		data, err = media.StripMetadata(info, data)
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", sentence(err)+"; nothing was stored.")
		return
	}
	poster, err := a.config.Attachments.Put(request.Context(), who.Workspace.ID, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeInternalError(response, err)
		return
	}
	thumb := a.storeKBThumbnail(request.Context(), who.Workspace.ID, info, data)
	item, old, err := a.store.SetKBVideoPoster(request.Context(), who.Workspace.ID, savedReplyActor(request.Context(), who), request.PathValue("id"), poster, info.ContentType, thumb)
	discard := old
	if err != nil {
		discard = []string{poster.Key}
		if thumb != nil {
			discard = append(discard, thumb.Key)
		}
	}
	for _, key := range discard {
		if discardErr := a.store.DiscardObject(request.Context(), who.Workspace.ID, key); discardErr != nil {
			log.Printf("knowledge base poster: discard bytes: %v", discardErr)
		}
	}
	if err != nil {
		writeKBError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, item)
}

func (a *API) getKBVideoPoster(response http.ResponseWriter, request *http.Request) {
	workspaceID := principal(request).Workspace.ID
	item, err := a.store.GetKBMedia(request.Context(), workspaceID, request.PathValue("id"))
	// The version is the end of the poster's content address: an older one is a 404, never another
	// picture under the address a cache kept.
	version := request.PathValue("version")
	if err != nil || a.config.Attachments == nil || item.PosterKey == "" || !strings.HasPrefix(item.PosterKey, workspaceID+"/") ||
		len(version) != 8 || !strings.HasSuffix(item.PosterKey, version) {
		if err != nil && !errors.Is(err, domain.ErrKBMediaNotFound) {
			writeStoreError(response, err)
			return
		}
		writeError(response, http.StatusNotFound, "not_found", "The video has no poster with this version.")
		return
	}
	serveKBMediaBytes(response, request, a.config.Attachments, item.PosterKey, item.PosterContentType, 0, item.PosterKey[strings.LastIndex(item.PosterKey, "/")+1:], "inline", item.ID+"-poster")
}
