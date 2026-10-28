package mcpbridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mcpbridge"
)

const token = "cdk_agent_secret-value-never-printed"

// fake is an MCP endpoint whose answer each test writes; it records the requests.
type fake struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	answer   func(w http.ResponseWriter, body []byte)
}

func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.bodies = append(f.bodies, body)
	f.mu.Unlock()
	f.answer(w, body)
}

// echo answers each request with a result that names the method, as JSON.
func echo(w http.ResponseWriter, body []byte) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(body, &msg)
	if len(msg.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	result := `{"method":"` + msg.Method + `"}`
	if msg.Method == "initialize" {
		result = `{"protocolVersion":"2025-11-25"}`
	}
	_, _ = io.WriteString(w, "{\n  \"jsonrpc\": \"2.0\",\n  \"id\": "+string(msg.ID)+",\n  \"result\": "+result+"\n}\n")
}

// run sends lines through the bridge to the fake and returns stdout and stderr.
func run(t *testing.T, f *fake, lines ...string) (stdout, stderr string) {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return runAt(t, server.URL+"/mcp", lines...)
}

func runAt(t *testing.T, endpoint string, lines ...string) (string, string) {
	t.Helper()
	var out, errs bytes.Buffer
	cfg := mcpbridge.Config{Endpoint: endpoint, Token: token, Timeout: 5 * time.Second, Version: "test", Stderr: &errs}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := mcpbridge.Run(ctx, cfg, strings.NewReader(strings.Join(lines, "\n")), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), token) || strings.Contains(errs.String(), token) {
		t.Fatalf("the token appeared in the output:\nstdout %s\nstderr %s", out.String(), errs.String())
	}
	return out.String(), errs.String()
}

// answers splits stdout into messages, failing on a line that is not one JSON object.
func answers(t *testing.T, stdout string) map[string]map[string]any {
	t.Helper()
	byID := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		if line == "" {
			continue
		}
		var msg map[string]any
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.UseNumber()
		if err := decoder.Decode(&msg); err != nil {
			t.Fatalf("stdout line is not one JSON message: %q", line)
		}
		id, _ := json.Marshal(msg["id"])
		byID[string(id)] = msg
	}
	return byID
}

func TestEachAnswerIsOneLineAndNotificationsGetNone(t *testing.T) {
	f := &fake{answer: echo}
	stdout, _ := run(t, f,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		``,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"two","method":"tools/list"}`)
	got := answers(t, stdout)
	if len(got) != 2 || got[`"two"`] == nil || got[`1`] == nil {
		t.Fatalf("want answers to 1 and \"two\" only, got %q", stdout)
	}
	if strings.Count(stdout, "\n") != 2 {
		t.Errorf("pretty JSON from the server must leave as one line each: %q", stdout)
	}
}

// lockedBuffer is a stdout the test reads while the bridge writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) lines() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), "\n")
}

func TestHeadersFollowTheNegotiatedVersion(t *testing.T) {
	f := &fake{answer: echo}
	server := httptest.NewServer(f)
	defer server.Close()
	out := &lockedBuffer{}
	stdin, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- mcpbridge.Run(context.Background(), mcpbridge.Config{Endpoint: server.URL, Token: token, Version: "9.9"}, stdin, out)
	}()
	// A host waits for each answer, as a client waits for initialize before its next request.
	for i, line := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"colliedesk_whoami","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`,
	} {
		_, _ = io.WriteString(writer, line+"\n")
		for deadline := time.Now().Add(5 * time.Second); out.lines() <= i && time.Now().Before(deadline); {
			time.Sleep(5 * time.Millisecond)
		}
	}
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 3 {
		t.Fatalf("%d requests", len(f.requests))
	}
	initialize, list, call := f.requests[0].Header, f.requests[1].Header, f.requests[2].Header
	if initialize.Get("MCP-Protocol-Version") != "" {
		t.Errorf("initialize names its version in the body; header=%q", initialize.Get("MCP-Protocol-Version"))
	}
	if list.Get("MCP-Protocol-Version") != "2025-11-25" || list.Get("Mcp-Method") != "" {
		t.Errorf("after initialize: version %q, method %q", list.Get("MCP-Protocol-Version"), list.Get("Mcp-Method"))
	}
	if call.Get("MCP-Protocol-Version") != "2026-07-28" || call.Get("Mcp-Method") != "tools/call" || call.Get("Mcp-Name") != "colliedesk_whoami" {
		t.Errorf("2026-07-28 call headers: %v", call)
	}
	for _, header := range []http.Header{initialize, list, call} {
		if header.Get("Authorization") != "Bearer "+token || header.Get("User-Agent") != "collie-mcp/9.9" ||
			header.Get("Accept") != "application/json, text/event-stream" || header.Get("Content-Type") != "application/json" {
			t.Errorf("request headers: %v", header)
		}
	}
}

func TestEventStreamAnswersBecomeLines(t *testing.T) {
	f := &fake{answer: func(w http.ResponseWriter, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"+
			"data: {\"jsonrpc\":\"2.0\",\n"+"data:  \"id\":7,\"result\":{}}\n\n")
	}}
	stdout, _ := run(t, f, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"x"}}`)
	if lines := strings.Split(strings.TrimSpace(stdout), "\n"); len(lines) != 2 || !strings.Contains(lines[1], `"id":7`) {
		t.Errorf("want the progress notification and the answer, one line each: %q", stdout)
	}
}

func TestFailuresAnswerTheWaitingIDAndTheBridgeGoesOn(t *testing.T) {
	cases := []struct {
		name   string
		answer func(w http.ResponseWriter, body []byte)
		want   string
	}{
		{"revoked token", func(w http.ResponseWriter, _ []byte) { http.Error(w, "unauthorized", http.StatusUnauthorized) },
			"Create a new agent token in Settings → Sessions"},
		{"wrong URL", func(w http.ResponseWriter, _ []byte) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>a web page</html>")
		}, "is not a Colliedesk MCP endpoint"},
		{"missing path", func(w http.ResponseWriter, _ []byte) { http.NotFound(w, nil) }, "is not a Colliedesk MCP endpoint"},
		{"database down", func(w http.ResponseWriter, _ []byte) { http.Error(w, "unavailable", http.StatusServiceUnavailable) },
			"is unreachable or unavailable; try again"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A large integer id and a string id come back exactly as sent.
			stdout, stderr := run(t, &fake{answer: c.answer},
				`{"jsonrpc":"2.0","id":12345678901234567890,"method":"tools/list"}`,
				`{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"name":"colliedesk_whoami"}}`,
				`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
			got := answers(t, stdout)
			if len(got) != 2 {
				t.Fatalf("want an error for each of the two requests and nothing for the notification: %q", stdout)
			}
			for _, id := range []string{`12345678901234567890`, `"abc"`} {
				message, _ := got[id]["error"].(map[string]any)["message"].(string)
				if !strings.Contains(message, c.want) {
					t.Errorf("id %s: message %q, want %q", id, message, c.want)
				}
			}
			if lines := strings.Count(stderr, "\n"); lines < 3 || strings.Contains(stderr, "<html>") {
				t.Errorf("stderr must have one line per failure (the notification's included) and no body: %q", stderr)
			}
		})
	}
}

func TestJSONRPCErrorsOfTheServerPassThrough(t *testing.T) {
	f := &fake{answer: func(w http.ResponseWriter, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":5,"error":{"code":-32022,"message":"protocol version not supported"}}`)
	}}
	stdout, _ := run(t, f, `{"jsonrpc":"2.0","id":5,"method":"tools/list"}`)
	if got := answers(t, stdout)["5"]; got == nil || !strings.Contains(stdout, "-32022") {
		t.Errorf("the server's own JSON-RPC error must reach the host: %q", stdout)
	}
}

func TestUnreachableServer(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL + "/mcp"
	server.Close()
	stdout, _ := runAt(t, endpoint, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if !strings.Contains(stdout, "is unreachable or unavailable") {
		t.Errorf("stdout %q", stdout)
	}
}

// An image upload is one JSON-RPC line of megabytes; a line scanner with its default buffer
// would stop the bridge at 64 KiB.
func TestALineOfSeveralMegabytesIsForwardedWhole(t *testing.T) {
	f := &fake{answer: echo}
	data := strings.Repeat("A", 3<<20)
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"colliedesk_kb_image_upload","arguments":{"data":"` + data + `"}}}`
	stdout, _ := run(t, f, line)
	if len(f.bodies) != 1 || string(f.bodies[0]) != line || answers(t, stdout)["1"] == nil {
		t.Fatalf("forwarded %d requests; answer %q", len(f.bodies), stdout[:min(len(stdout), 200)])
	}
}

func TestEndpointNormalisesAndRefusesPlainHTTP(t *testing.T) {
	good := map[string]string{
		"https://desk.example.com":          "https://desk.example.com/mcp",
		"https://desk.example.com/":         "https://desk.example.com/mcp",
		"https://desk.example.com/mcp":      "https://desk.example.com/mcp",
		"https://desk.example.com/mcp/":     "https://desk.example.com/mcp",
		"https://example.com/support":       "https://example.com/support/mcp",
		"http://localhost:8080":             "http://localhost:8080/mcp",
		"http://127.0.0.1:8080/mcp":         "http://127.0.0.1:8080/mcp",
		"http://[::1]:8080":                 "http://[::1]:8080/mcp",
		" https://desk.example.com:8443/  ": "https://desk.example.com:8443/mcp",
	}
	for raw, want := range good {
		if got, err := mcpbridge.Endpoint(raw); err != nil || got != want {
			t.Errorf("Endpoint(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	bad := map[string]string{
		"":                                  "no Colliedesk address",
		"http://desk.example.com":           "unencrypted",
		"http://192.168.1.5:8080":           "unencrypted",
		"desk.example.com":                  "not a web address",
		"ftp://desk.example.com":            "not a web address",
		"https://user:pw@desk.example.com":  "without a user name",
		"https://desk.example.com/?token=x": "without a user name",
	}
	for raw, want := range bad {
		if _, err := mcpbridge.Endpoint(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Endpoint(%q) error %v, want %q", raw, err, want)
		}
	}
}

func TestTheTokenNeverComesFromTheCommandLine(t *testing.T) {
	env := func(values map[string]string) func(string) string { return func(k string) string { return values[k] } }
	withURL := map[string]string{"COLLIEDESK_URL": "https://desk.example.com"}

	if _, err := mcpbridge.Parse([]string{token}, env(withURL)); err == nil || !strings.Contains(err.Error(), "no token on the command line") {
		t.Errorf("a token argument: %v", err)
	}
	if _, err := mcpbridge.Parse([]string{"--token", token}, env(withURL)); err == nil || strings.Contains(err.Error(), token) {
		t.Errorf("--token must not exist, and the error must not repeat the token: %v", err)
	}
	if _, err := mcpbridge.Parse(nil, env(withURL)); err == nil || !strings.Contains(err.Error(), "COLLIEDESK_TOKEN") {
		t.Errorf("no token: %v", err)
	}
	if _, err := mcpbridge.Parse(nil, env(map[string]string{"COLLIEDESK_URL": "https://desk.example.com", "COLLIEDESK_TOKEN": "cdk_session_x"})); err == nil || !strings.Contains(err.Error(), "not an agent token") {
		t.Errorf("a session token: %v", err)
	}
	cfg, err := mcpbridge.Parse([]string{"--timeout", "5s"}, env(map[string]string{"COLLIEDESK_URL": "https://desk.example.com/", "COLLIEDESK_TOKEN": " " + token + "\n"}))
	if err != nil || cfg.Token != token || cfg.Endpoint != "https://desk.example.com/mcp" || cfg.Timeout != 5*time.Second {
		t.Errorf("env: %+v, %v", cfg, err)
	}

	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte(token+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if _, err := mcpbridge.Parse([]string{"--token-file", file}, env(withURL)); err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Errorf("a token file others can read: %v", err)
		}
	}
	if err := os.Chmod(file, 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := mcpbridge.Parse([]string{"--token-file", file}, env(withURL)); err != nil || cfg.Token != token {
		t.Errorf("token file: %+v, %v", cfg, err)
	}
}

// SIGTERM ends `collie mcp` while the host keeps stdin open and sends nothing.
func TestRunStopsWhenItsContextEndsWhileStdinIsIdle(t *testing.T) {
	stdin, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- mcpbridge.Run(ctx, mcpbridge.Config{Endpoint: "https://desk.example.com/mcp", Token: token}, stdin, io.Discard)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a stop is not a failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run kept waiting on stdin after its context ended")
	}
}

// A redirect, even to the same host over http, must not carry the token anywhere.
func TestTheTokenNeverFollowsARedirect(t *testing.T) {
	var leaked sync.Mutex
	got := ""
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Lock()
		got = r.Header.Get("Authorization")
		leaked.Unlock()
		echo(w, nil)
	}))
	defer elsewhere.Close()
	f := &fake{answer: func(w http.ResponseWriter, _ []byte) {
		w.Header().Set("Location", elsewhere.URL+"/mcp")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}}
	stdout, _ := run(t, f, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	leaked.Lock()
	defer leaked.Unlock()
	if got != "" {
		t.Errorf("the redirect target received %q", got)
	}
	if !strings.Contains(stdout, "redirects") {
		t.Errorf("the host must learn the address moved: %q", stdout)
	}
}
