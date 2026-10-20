package attachments

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Served describes stored bytes that never change under their key, as a response sends them.
type Served struct {
	Key, ContentType string
	// Size is sent as Content-Length when the backend cannot seek (and ServeContent cannot count).
	Size int64
	// ETag is the content's hash, unquoted; Cache is the Cache-Control (private for the app, public
	// for the help center); Disposition is the whole Content-Disposition, or "".
	ETag, Cache, Disposition string
}

// Serve sends stored bytes with ranges when the backend can seek (local disk), and nothing in them
// able to run. It reports false, having written nothing, when the bytes cannot be opened.
func Serve(response http.ResponseWriter, request *http.Request, objects Storage, served Served) bool {
	body, err := objects.Open(request.Context(), served.Key)
	if err != nil {
		log.Printf("stored file %s: open: %v", served.Key, err)
		return false
	}
	defer body.Close()
	header := response.Header()
	header.Set("Content-Type", served.ContentType)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("Cache-Control", served.Cache)
	header.Set("ETag", `"`+served.ETag+`"`)
	if served.Disposition != "" {
		header.Set("Content-Disposition", served.Disposition)
	}
	if seeker, ok := body.(io.ReadSeeker); ok {
		http.ServeContent(response, request, "", time.Time{}, seeker)
		return true
	}
	header.Set("Content-Length", strconv.FormatInt(served.Size, 10))
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return true
	}
	if _, err := io.Copy(response, body); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("stored file %s: stream: %v", served.Key, err)
	}
	return true
}
