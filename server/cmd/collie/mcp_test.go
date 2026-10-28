package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// collie mcp runs where there is no database (a person's computer); stdout carries only MCP.
func TestMCPRunsWithoutADatabase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cdk_agent_cli" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	}))
	defer server.Close()
	t.Setenv("COLLIEDESK_URL", server.URL)
	t.Setenv("COLLIEDESK_TOKEN", "cdk_agent_cli")
	previous := stdin
	stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	t.Cleanup(func() { stdin = previous })

	var output bytes.Buffer
	if err := run(context.Background(), []string{"mcp"}, &output, "", ""); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`+"\n" {
		t.Errorf("stdout = %q, want the server's answer and nothing else", got)
	}

	t.Setenv("COLLIEDESK_TOKEN", "")
	if err := run(context.Background(), []string{"mcp"}, io.Discard, "", ""); err == nil || !strings.Contains(err.Error(), "COLLIEDESK_TOKEN") {
		t.Errorf("without a token: %v", err)
	}
}
