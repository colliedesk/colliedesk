package mcpserver_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

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

// A refused MCP call must leave a trace: the same access log line and X-Request-Id as an API request,
// so a person diagnosing a connector that cannot sign in finds the call in the server's log.
func TestMCPRequestsAreLoggedOnceWithTheirRequestID(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{mcpserver.Path, mcpserver.OperatorPath} {
		logs := captureLogs(t)
		response, err := http.Post(e.server.URL+path+"?token=secret-query-words", "application/json",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		id := response.Header.Get("X-Request-Id")
		access := logLines(t, logs, "api request")
		if response.StatusCode != http.StatusUnauthorized || id == "" {
			t.Errorf("POST %s: status=%d X-Request-Id=%q, want 401 with a request id", path, response.StatusCode, id)
		}
		if len(access) != 1 || access[0]["request_id"] != id || access[0]["method"] != "POST" || access[0]["path"] != path ||
			access[0]["status"] != float64(401) || access[0]["level"] != "WARN" {
			t.Errorf("POST %s: access log lines %v, want one WARN line for POST %s with status 401 and request_id %q", path, access, path, id)
		}
		if strings.Contains(logs.String(), "secret-query-words") {
			t.Errorf("POST %s: the query string reached the log: %s", path, logs.String())
		}
	}
	// A request Mount hands on to the API is logged by the API alone, not a second time.
	logs := captureLogs(t)
	response, err := http.Get(e.server.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if access := logLines(t, logs, "api request"); len(access) != 1 || access[0]["path"] != "/api/v1/auth/me" {
		t.Errorf("GET /api/v1/auth/me: access log lines %v, want exactly one", access)
	}
}
