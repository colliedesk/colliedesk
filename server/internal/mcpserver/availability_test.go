package mcpserver_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// A database outage is not a bad token: answering 401 would make a client throw a good token away.
func TestMCPAnswers503WhileTheDatabaseIsDown(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Outage Desk", "admin@outage.example.com")
	pool := testPool(t)
	pool.Close()
	handler := mcpserver.New(mcpserver.Config{API: e.api, Auth: auth.New(pool, testOrigin), Version: testVersion})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	req.Header.Set("Authorization", "Bearer "+w.read)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	body, _ := io.ReadAll(recorder.Body)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("status=%d WWW-Authenticate=%q body=%s, want 503 without a Bearer challenge", recorder.Code, recorder.Header().Get("WWW-Authenticate"), body)
	}
}
