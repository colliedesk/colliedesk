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
// (which may be nil). Any other status comes back as a *restError.
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
	if r.body.Len()+len(data) > maxRESTResponse {
		r.overflow = true
		return 0, errTooLarge
	}
	return r.body.Write(data)
}
