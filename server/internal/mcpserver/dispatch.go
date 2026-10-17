package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxRESTResponse bounds what one in-process call may buffer.
const maxRESTResponse = 16 << 20

// errTooLarge is an answer longer than maxRESTResponse.
var errTooLarge = errors.New("response too large")

// errEnough stops a download once the tool has the bytes it needs; the handler stops streaming.
var errEnough = errors.New("the MCP tool read enough of the body")

// restClient serves requests through the REST API handler without a network hop.
type restClient struct{ api http.Handler }

// restError is a non-2xx answer of the REST API.
type restError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter string
}

func (e *restError) Error() string {
	return fmt.Sprintf("REST %d %s: %s", e.Status, e.Code, e.Message)
}

// do calls the REST API with the caller's bearer token and decodes a 2xx JSON answer into into
// (which may be nil). A call Colliedesk filed as an agent request (202) comes back as an
// *agentRequested; any other status as a *restError.
func (r *restClient) do(ctx context.Context, token, method, path string, query url.Values, body, into any) error {
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := &recorder{header: http.Header{}}
	r.api.ServeHTTP(recorder, request)
	if recorder.overflow {
		return errTooLarge
	}
	if recorder.status == http.StatusAccepted {
		var filed agentRequested
		if json.Unmarshal(recorder.body.Bytes(), &filed) == nil && filed.RequestID != "" {
			return &filed
		}
	}
	if recorder.status < 200 || recorder.status > 299 {
		return parseRESTError(recorder)
	}
	if into == nil || recorder.body.Len() == 0 {
		return nil
	}
	if err := json.Unmarshal(recorder.body.Bytes(), into); err != nil {
		return fmt.Errorf("decode %s %s answer: %w", method, path, err)
	}
	return nil
}

// download fetches path as the caller and returns the headers and at most limit bytes of a 2xx body,
// so a large file is never buffered whole. Any other status comes back as a *restError.
func (r *restClient) download(ctx context.Context, token, path string, limit int) (http.Header, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := &recorder{header: http.Header{}, limit: limit}
	r.api.ServeHTTP(recorder, request)
	if recorder.status < 200 || recorder.status > 299 {
		return nil, nil, parseRESTError(recorder)
	}
	return recorder.header, recorder.body.Bytes(), nil
}

// agentRequested is a call Colliedesk did not run: above the token's tier, or one that needs a
// person's approval for every tier, it was filed as an agent request. Nothing changed yet.
type agentRequested struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	ExpiresAt string `json:"expires_at"`
}

func (e *agentRequested) Error() string { return "filed as agent request " + e.RequestID }

func parseRESTError(r *recorder) *restError {
	failure := &restError{Status: r.status, RetryAfter: r.header.Get("Retry-After")}
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(r.body.Bytes(), &envelope) == nil {
		failure.Code, failure.Message = envelope.Error.Code, strings.TrimSpace(envelope.Error.Message)
	}
	return failure
}

// recorder is the smallest http.ResponseWriter that keeps what a handler wrote.
type recorder struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
	// limit, when above zero, keeps only the first limit bytes and then stops the handler with errEnough.
	limit int
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *recorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.limit > 0 && r.body.Len()+len(data) > r.limit {
		room := r.limit - r.body.Len()
		r.body.Write(data[:room])
		return room, errEnough
	}
	if r.body.Len()+len(data) > maxRESTResponse {
		r.overflow = true
		return 0, errTooLarge
	}
	return r.body.Write(data)
}
