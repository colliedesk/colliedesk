package mcpserver_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mcpbridge"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// bridged opens an MCP session the way Claude Desktop does with `collie mcp`: a stdio client whose
// lines the bridge carries to /mcp. It returns the session and what the bridge wrote to stderr.
func (e *env) bridged(token, version string) (*mcp.ClientSession, *syncBuffer) {
	e.t.Helper()
	hostToBridge, bridgeIn := io.Pipe()
	bridgeToHost, bridgeOut := io.Pipe()
	stderr := &syncBuffer{}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		cfg := mcpbridge.Config{Endpoint: e.mcpURL(), Token: token, Timeout: 30 * time.Second, Version: "test", Stderr: stderr}
		done <- mcpbridge.Run(ctx, cfg, hostToBridge, bridgeOut)
		_ = bridgeOut.Close()
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "colliedesk-bridge-test", Version: "0"}, nil)
	connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(connectCtx, &mcp.IOTransport{Reader: bridgeToHost, Writer: bridgeIn}, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		e.t.Fatalf("connect through the bridge (%s): %v; stderr %s", version, err, stderr.String())
	}
	e.t.Cleanup(func() {
		_ = session.Close()
		_ = bridgeIn.Close()
		select {
		case err := <-done:
			if err != nil {
				e.t.Errorf("bridge: %v", err)
			}
		case <-time.After(10 * time.Second):
			e.t.Error("the bridge did not stop when stdin ended")
		}
		stop()
	})
	return session, stderr
}

// Through the bridge a host sees exactly the server's tools and calls them, with every protocol
// version the server speaks; the token never reaches stdout or stderr.
func TestBridgeCarriesEveryProtocolVersionToTheServer(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Bridge Desk", "admin@bridge.example.com")
	for _, version := range []string{"2025-06-18", "2025-11-25", "2026-07-28"} {
		t.Run(version, func(t *testing.T) {
			session, stderr := e.bridged(w.draft, version)
			if got := session.InitializeResult(); got == nil || got.ProtocolVersion != version {
				t.Fatalf("negotiated %+v, want %s", got, version)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var names []string
			for tool, err := range session.Tools(ctx, nil) {
				if err != nil {
					t.Fatalf("tools/list: %v", err)
				}
				names = append(names, tool.Name)
			}
			if want := len(mcpserver.Catalog()); len(names) != want {
				t.Errorf("listed %d tools through the bridge, the catalog has %d", len(names), want)
			}
			me := e.ok(session, "colliedesk_whoami", map[string]any{})
			if me["token_tier"] != "draft" {
				t.Errorf("whoami token_tier = %v", me["token_tier"])
			}
			if strings.Contains(stderr.String(), w.draft) {
				t.Errorf("the token is in stderr: %s", stderr.String())
			}
		})
	}
}

// The bridge adds no reach: a token of one workspace cannot read another workspace's conversation.
func TestBridgeKeepsWorkspacesApart(t *testing.T) {
	e := newEnv(t)
	a := e.newWorkspace("Bridge A", "admin@bridge-a.example.com")
	b := e.newWorkspace("Bridge B", "admin@bridge-b.example.com")
	contact := e.created("/api/v1/contacts", map[string]any{"name": "Bea", "email": "bea@b.example.com"}, b.admin)
	conversation := e.created("/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "B-ONLY subject"}, b.admin)

	session, _ := e.bridged(a.read, "2025-11-25")
	if message := e.fails(session, "colliedesk_conversation_get", map[string]any{"conversation_id": conversation}); strings.Contains(message, "B-ONLY") {
		t.Errorf("workspace A read B's conversation through the bridge: %s", message)
	}
	own, _ := e.bridged(b.read, "2025-11-25")
	if got := e.ok(own, "colliedesk_conversation_get", map[string]any{"conversation_id": conversation}); !strings.Contains(stringify(got), "B-ONLY") {
		t.Errorf("workspace B cannot read its own conversation: %v", got)
	}
}

// A revoked token answers each call with why, and the bridge stays up for the next one.
func TestBridgeExplainsARevokedToken(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Revoked Desk", "admin@revoked.example.com")
	_, stderr := e.bridged(w.read, "2025-11-25") // a working session first, so this is not a startup failure

	hostToBridge, bridgeIn := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- mcpbridge.Run(context.Background(), mcpbridge.Config{Endpoint: e.mcpURL(), Token: "cdk_agent_not-a-real-token", Version: "test", Stderr: stderr}, hostToBridge, &out)
	}()
	_, _ = io.WriteString(bridgeIn, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`+"\n")
	_, _ = io.WriteString(bridgeIn, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	_ = bridgeIn.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := out.String(); strings.Count(got, "Create a new agent token in Settings → Sessions") != 2 || !strings.Contains(got, `"id":1`) || !strings.Contains(got, `"id":2`) {
		t.Errorf("want an explained error for each request: %s", got)
	}
}
