package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var generatedRequestID = regexp.MustCompile(`^[0-9a-f]{12}$`)

// captureLogs sends the default logger's output to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buffer
}

// logLines returns the JSON log records of buffer whose message is message.
func logLines(t *testing.T, buffer *bytes.Buffer, message string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		var line map[string]any
		if json.Unmarshal([]byte(raw), &line) == nil && line["msg"] == message {
			lines = append(lines, line)
		}
	}
	return lines
}

var requestIDField = regexp.MustCompile(`,"request_id":"[^"]*"`)

// withoutRequestID drops error.request_id, which differs on every response, so two bodies that must be
// indistinguishable (a foreign id and a missing id) can be compared.
func withoutRequestID(body string) string { return requestIDField.ReplaceAllString(body, "") }

func errorRequestID(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	decode(t, response, &body)
	return body.Error.RequestID
}

func TestEveryResponseCarriesAGeneratedRequestID(t *testing.T) {
	api, _ := testAPI(t)
	first := request(t, api, http.MethodGet, "/api/v1/config", nil, "", nil).Header().Get("X-Request-Id")
	second := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, "", nil).Header().Get("X-Request-Id")
	if !generatedRequestID.MatchString(first) || !generatedRequestID.MatchString(second) {
		t.Fatalf("request ids %q and %q, want 12 lowercase hex characters", first, second)
	}
	if first == second {
		t.Fatalf("two requests share the id %q", first)
	}
}

func TestSaneIncomingRequestIDIsKeptAndOthersAreReplaced(t *testing.T) {
	api, _ := testAPI(t)
	call := func(id string) string {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
		req.Header["X-Request-Id"] = []string{id}
		return serve(api, req).Header().Get("X-Request-Id")
	}
	if got := call("edge-7f3a9c21.b"); got != "edge-7f3a9c21.b" {
		t.Errorf("a proxy's id was replaced by %q", got)
	}
	for name, id := range map[string]string{
		"empty": "", "too short": "ab", "too long": strings.Repeat("a", 65), "space": "abc def ghi", "newline": "abcdefgh\nX-Evil: 1",
		"quote": `abcdefgh"`, "non-ASCII": "abcdefghé",
	} {
		if got := call(id); got == id || !generatedRequestID.MatchString(got) {
			t.Errorf("%s: id %q answered %q, want a generated id", name, id, got)
		}
	}
}

func TestErrorBodiesNameTheRequestID(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Request Id Desk", "admin@request-id.example.com")

	unauthorized := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, "", nil)
	if unauthorized.Code != http.StatusUnauthorized || errorRequestID(t, unauthorized) != unauthorized.Header().Get("X-Request-Id") || errorRequestID(t, unauthorized) == "" {
		t.Errorf("401 body %s, header %q", unauthorized.Body.String(), unauthorized.Header().Get("X-Request-Id"))
	}

	req := newRequest(t, http.MethodGet, "/api/v1/auth/me", nil, "", nil)
	req.Header.Set("X-Request-Id", "proxy-id-0042")
	if echoed := serve(api, req); errorRequestID(t, echoed) != "proxy-id-0042" {
		t.Errorf("the proxy's id is not in the error body: %s", echoed.Body.String())
	}

	invalid := submitForm(api, "no-such-desk", `{"email":"x"}`, "", "")
	if invalid.Code != http.StatusNotFound || errorRequestID(t, invalid) != invalid.Header().Get("X-Request-Id") {
		t.Errorf("404 body %s", invalid.Body.String())
	}

	okay := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
	if strings.Contains(okay.Body.String(), "request_id") {
		t.Errorf("a success body carries a request id: %s", okay.Body.String())
	}
}

func TestInternalErrorsAreLoggedWithTheRequestID(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "Logged Desk", "admin@logged.example.com")
	if _, err := pool.Exec(context.Background(), `DROP TABLE web_form_channels CASCADE`); err != nil {
		t.Fatal(err)
	}
	logs := captureLogs(t)
	req := newRequest(t, http.MethodGet, "/api/v1/channels/web-form", nil, token, nil)
	req.Header.Set("X-Request-Id", "crash-trace-0007")
	response := serve(api, req)
	if response.Code != http.StatusInternalServerError || errorRequestID(t, response) != "crash-trace-0007" {
		t.Fatalf("status=%d body=%s, want 500 naming crash-trace-0007", response.Code, response.Body.String())
	}
	failed := logLines(t, logs, "request failed")
	if len(failed) != 1 || failed[0]["request_id"] != "crash-trace-0007" {
		t.Errorf("error log lines %v, want one with request_id crash-trace-0007", failed)
	}
	access := logLines(t, logs, "api request")
	if len(access) != 1 || access[0]["request_id"] != "crash-trace-0007" || access[0]["status"] != float64(500) || access[0]["method"] != "GET" || access[0]["path"] != "/api/v1/channels/web-form" {
		t.Errorf("access log lines %v, want one for GET /api/v1/channels/web-form with status 500", access)
	}
}

func TestAccessLogCarriesNoQueryString(t *testing.T) {
	api, _ := testAPI(t)
	logs := captureLogs(t)
	request(t, api, http.MethodGet, "/api/v1/conversations?q=secret+customer+words", nil, "", nil)
	access := logLines(t, logs, "api request")
	if len(access) != 1 || strings.Contains(logs.String(), "secret") || access[0]["status"] != float64(401) {
		t.Fatalf("access log %s", logs.String())
	}
}
