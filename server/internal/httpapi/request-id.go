package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"time"
)

const requestIDHeader = "X-Request-Id"

// proxyRequestID is the shape of an id a reverse proxy may hand us: printable and short enough for a log
// line and a header. Anything else is replaced, so a client cannot inject line breaks or markup into logs.
var proxyRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

type requestIDKey struct{}

// newRequestID returns 12 random hex characters, short enough to read out over the phone.
func newRequestID() string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic("httpapi: no entropy for a request id: " + err.Error())
	}
	return hex.EncodeToString(raw[:])
}

// requestIDOf is the id the request was given, or "" outside withRequestID.
func requestIDOf(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// withRequestID gives every request an id (the proxy's X-Request-Id when it is sane, else a generated
// one), returns it in the X-Request-Id response header, and logs one line per request carrying it.
// writeError and writeInternalError read it back from that header, so error bodies and error logs name it.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		id := request.Header.Get(requestIDHeader)
		if !proxyRequestID.MatchString(id) {
			id = newRequestID()
		}
		response.Header().Set(requestIDHeader, id)
		recorder := &statusRecorder{ResponseWriter: response, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, request.WithContext(context.WithValue(request.Context(), requestIDKey{}, id)))
		level := slog.LevelDebug
		switch {
		case recorder.status >= 500:
			level = slog.LevelError
		case recorder.status >= 400:
			level = slog.LevelWarn
		}
		// The path only: a query string can carry a customer's search words.
		slog.Log(request.Context(), level, "api request", "request_id", id, "method", request.Method, "path", request.URL.Path,
			"status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

// statusRecorder remembers the status code. Unwrap keeps http.ResponseController (flush, read deadlines)
// working through it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
