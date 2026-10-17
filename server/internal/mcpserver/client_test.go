package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if b.token != "" {
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// connect opens an MCP session with the token. version "" negotiates the newest protocol.
func (e *env) connect(token, version string) *mcp.ClientSession {
	e.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "colliedesk-test", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: e.mcpURL(), HTTPClient: &http.Client{Transport: bearer{token}}, DisableStandaloneSSE: true, MaxRetries: -1}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, &mcp.ClientSessionOptions{ProtocolVersion: version})
	if err != nil {
		e.t.Fatalf("connect: %v", err)
	}
	e.t.Cleanup(func() { _ = session.Close() })
	return session
}

func (e *env) call(session *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		e.t.Fatalf("%s: protocol error: %v", tool, err)
	}
	return result
}

// ok calls the tool and requires a successful result; it returns the structured content.
func (e *env) ok(session *mcp.ClientSession, tool string, args map[string]any) map[string]any {
	e.t.Helper()
	result := e.call(session, tool, args)
	if result.IsError {
		e.t.Fatalf("%s failed: %s", tool, text(result))
	}
	out := structured(e.t, result)
	e.requireMatchesOutputSchema(session, tool, out)
	return out
}

// requireMatchesOutputSchema checks the result against the output schema the server advertises.
func (e *env) requireMatchesOutputSchema(session *mcp.ClientSession, tool string, out map[string]any) {
	e.t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, advertised := range listed.Tools {
		if advertised.Name != tool {
			continue
		}
		encoded, err := json.Marshal(advertised.OutputSchema)
		if err != nil {
			e.t.Fatal(err)
		}
		var schema jsonschema.Schema
		if err := json.Unmarshal(encoded, &schema); err != nil {
			e.t.Fatal(err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			e.t.Fatalf("%s: output schema does not resolve: %v", tool, err)
		}
		if err := resolved.Validate(out); err != nil {
			e.t.Errorf("%s: result does not match its output schema: %v\n%v", tool, err, out)
		}
		return
	}
	e.t.Fatalf("%s is not listed", tool)
}

// fails calls the tool and requires a tool error; it returns the message the model sees.
func (e *env) fails(session *mcp.ClientSession, tool string, args map[string]any) string {
	e.t.Helper()
	result := e.call(session, tool, args)
	if !result.IsError {
		e.t.Fatalf("%s succeeded, want a tool error: %s", tool, text(result))
	}
	return text(result)
}

func text(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func structured(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("structured content %s: %v", encoded, err)
	}
	return decoded
}

// at walks a decoded JSON value by keys and array indexes.
func at(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, step := range path {
		switch key := step.(type) {
		case string:
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("path %v: %T is not an object at %q", path, value, key)
			}
			value = object[key]
		case int:
			list, ok := value.([]any)
			if !ok || key >= len(list) {
				t.Fatalf("path %v: no element %d in %v", path, key, value)
			}
			value = list[key]
		}
	}
	return value
}
