// Package mcpbridge is `collie mcp`: it carries MCP between a host that only starts stdio servers
// (Claude Desktop) and the Streamable HTTP endpoint of a Colliedesk server. It is a thin proxy: it
// forwards each JSON-RPC line as it is and registers no tools, so it never drifts from the catalog
// of the server it talks to. It uses the standard library only, so the client build stays small.
package mcpbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config is what a run needs. Token never comes from the command line (see Parse).
type Config struct {
	Endpoint string // the /mcp URL
	Token    string
	Timeout  time.Duration // per request
	Version  string        // of collie, for User-Agent
	Client   *http.Client  // nil: one keep-alive client of its own
	Stderr   io.Writer     // one line per event; never a header, a body or the token
}

const (
	protocolVersionMeta = "io.modelcontextprotocol/protocolVersion"
	// From this revision on, a POST carries Mcp-Method and Mcp-Name; the server refuses it otherwise.
	standardHeadersVersion = "2026-07-28"
	// maxInFlight requests at once; reading stdin waits beyond it.
	maxInFlight = 16
	// codeBridge is the JSON-RPC error code of an answer the bridge makes itself (implementation range).
	codeBridge = -32000
)

type bridge struct {
	cfg     Config
	client  *http.Client
	out     *bufio.Writer
	outMu   sync.Mutex
	errMu   sync.Mutex // requests fail at once; one line each on stderr
	version struct {
		sync.Mutex
		negotiated string // from the initialize result, for the requests after it
	}
}

// message is the part of a JSON-RPC message the bridge reads. ID stays raw, so a large number or a
// string comes back exactly as the host sent it.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

// Run reads JSON-RPC lines from stdin until EOF and answers each request on stdout, one line per
// message. It returns when stdin ends and every request has been answered, or when ctx ends.
func Run(ctx context.Context, cfg Config, stdin io.Reader, stdout io.Writer) error {
	b := &bridge{cfg: cfg, client: cfg.Client, out: bufio.NewWriter(stdout)}
	if b.client == nil {
		b.client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment, MaxIdleConnsPerHost: maxInFlight, IdleConnTimeout: 90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true,
			},
			// Never follow a redirect: Go keeps Authorization when only the scheme or port changes,
			// so https to http on the same host would send the token in clear text.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if b.cfg.Timeout <= 0 {
		b.cfg.Timeout = 60 * time.Second
	}
	b.logf("collie mcp %s: forwarding to %s", cfg.Version, hostOf(cfg.Endpoint))

	// Reading stdin blocks with no way to cancel it, so it runs on its own and Run waits for a line
	// or for ctx: SIGTERM must end the bridge while the host sends nothing. A stop is not an error.
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		reader := bufio.NewReaderSize(stdin, 64*1024)
		for {
			// ReadBytes has no line limit: an image upload is one line of several megabytes.
			line, err := reader.ReadBytes('\n')
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
				select {
				case lines <- trimmed:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()
	slots := make(chan struct{}, maxInFlight)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read stdin: %w", err)
		case line := <-lines:
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			wg.Add(1)
			go func() {
				defer func() { <-slots; wg.Done() }()
				b.forward(ctx, line)
			}()
		}
	}
}

func (b *bridge) forward(ctx context.Context, line []byte) {
	var msg message
	single := line[0] == '{'
	if single {
		if err := json.Unmarshal(line, &msg); err != nil {
			b.reply(nil, codeBridge, "collie mcp received a line that is not JSON-RPC")
			b.logf("collie mcp: a line from the host is not JSON")
			return
		}
	}
	isCall := single && msg.Method != "" && len(msg.ID) > 0 && string(msg.ID) != "null"

	ctx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.cfg.Endpoint, bytes.NewReader(line))
	if err != nil {
		b.fail(msg, isCall, "Colliedesk at "+hostOf(b.cfg.Endpoint)+" is unreachable or unavailable; try again.", err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+b.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", "collie-mcp/"+b.cfg.Version)
	if single && msg.Method != "" {
		b.setProtocolHeaders(req.Header, msg)
	}

	resp, err := b.client.Do(req)
	if err != nil {
		reason := "network error"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = fmt.Sprintf("no answer within %s", b.cfg.Timeout)
		}
		b.fail(msg, isCall, "Colliedesk at "+hostOf(b.cfg.Endpoint)+" is unreachable or unavailable; try again.", reason)
		return
	}
	defer resp.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))

	switch {
	case resp.StatusCode == http.StatusAccepted:
		return // a notification or a response the server took; nothing to answer
	case resp.StatusCode == http.StatusUnauthorized:
		b.fail(msg, isCall, "Colliedesk refused the token: it is revoked, expired or mistyped. Create a new agent token in Settings → Sessions.", "HTTP 401")
		return
	case mediaType == "text/event-stream" && resp.StatusCode == http.StatusOK:
		b.copyEvents(resp.Body, msg, isCall)
		return
	case mediaType == "application/json":
		// 200, and also the JSON-RPC errors the server answers with 400 (an unsupported protocol
		// version, a header mismatch): they carry the id, so the host gets them as they are.
		body, err := io.ReadAll(resp.Body)
		if err == nil && b.writeJSON(body, msg) {
			if resp.StatusCode != http.StatusOK {
				b.logf("collie mcp: HTTP %d", resp.StatusCode)
			}
			return
		}
	}
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		b.fail(msg, isCall, fmt.Sprintf("%s redirects to %s; use that address in the settings.", b.cfg.Endpoint, resp.Header.Get("Location")), fmt.Sprintf("HTTP %d", resp.StatusCode))
	case resp.StatusCode == http.StatusNotFound || mediaType == "text/html":
		b.fail(msg, isCall, b.cfg.Endpoint+" is not a Colliedesk MCP endpoint; check the URL.", fmt.Sprintf("HTTP %d", resp.StatusCode))
	case resp.StatusCode >= 500:
		b.fail(msg, isCall, "Colliedesk at "+hostOf(b.cfg.Endpoint)+" is unreachable or unavailable; try again.", fmt.Sprintf("HTTP %d", resp.StatusCode))
	default:
		b.fail(msg, isCall, fmt.Sprintf("Colliedesk answered HTTP %d to %s.", resp.StatusCode, msg.Method), fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
}

// setProtocolHeaders sets MCP-Protocol-Version and, from 2026-07-28, Mcp-Method and Mcp-Name. A
// stdio host sends no HTTP headers, so the bridge derives them from the message, the way an HTTP
// client of the SDK would after initialize.
func (b *bridge) setProtocolHeaders(header http.Header, msg message) {
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
		Name string                     `json:"name"`
		URI  string                     `json:"uri"`
	}
	_ = json.Unmarshal(msg.Params, &params)
	var version string
	if raw, ok := params.Meta[protocolVersionMeta]; ok {
		_ = json.Unmarshal(raw, &version)
	}
	if version == "" && msg.Method != "initialize" {
		b.version.Lock()
		version = b.version.negotiated
		b.version.Unlock()
	}
	if version == "" {
		return // the server accepts a request without it (an initialize names its version in the body)
	}
	header.Set("MCP-Protocol-Version", version)
	if version < standardHeadersVersion {
		return
	}
	header.Set("Mcp-Method", msg.Method)
	switch msg.Method {
	case "tools/call", "prompts/get":
		header.Set("Mcp-Name", params.Name)
	case "resources/read":
		header.Set("Mcp-Name", params.URI)
	}
}

// copyEvents writes the data of each server-sent event as one line.
func (b *bridge) copyEvents(body io.Reader, request message, isCall bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 64<<20)
	var data []string
	answered := false
	flush := func() {
		if len(data) > 0 && b.writeJSON([]byte(strings.Join(data, "\n")), request) {
			answered = true
		}
		data = nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	flush()
	if err := scanner.Err(); err != nil && !answered {
		b.fail(request, isCall, "Colliedesk at "+hostOf(b.cfg.Endpoint)+" is unreachable or unavailable; try again.", "the event stream broke")
	}
}

// writeJSON writes one JSON message as one line and learns the protocol version from an initialize
// result. It reports false when body is not JSON.
func (b *bridge) writeJSON(body []byte, request message) bool {
	var compact bytes.Buffer
	if err := json.Compact(&compact, body); err != nil || compact.Len() == 0 {
		return false
	}
	if request.Method == "initialize" {
		var answer struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(compact.Bytes(), &answer) == nil && answer.Result.ProtocolVersion != "" {
			b.version.Lock()
			b.version.negotiated = answer.Result.ProtocolVersion
			b.version.Unlock()
		}
	}
	compact.WriteByte('\n')
	b.outMu.Lock()
	defer b.outMu.Unlock()
	_, _ = b.out.Write(compact.Bytes())
	_ = b.out.Flush()
	return true
}

// fail answers a request with a JSON-RPC error, so the host shows why instead of "server
// disconnected", and logs the reason. A notification has no id to answer; only the log has it.
func (b *bridge) fail(msg message, isCall bool, text, reason string) {
	b.logf("collie mcp: %s failed: %s", methodOr(msg.Method), reason)
	if isCall {
		b.reply(msg.ID, codeBridge, text)
	}
}

func (b *bridge) reply(id json.RawMessage, code int, text string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	body, _ := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: id, Error: struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{code, text}})
	b.writeJSON(body, message{})
}

func (b *bridge) logf(format string, args ...any) {
	if b.cfg.Stderr != nil {
		b.errMu.Lock()
		defer b.errMu.Unlock()
		fmt.Fprintf(b.cfg.Stderr, format+"\n", args...)
	}
}

func methodOr(method string) string {
	if method == "" {
		return "a message"
	}
	return method
}
