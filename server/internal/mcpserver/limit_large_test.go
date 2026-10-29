package mcpserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// largeCall sends a large body as a request of workspace through handler.
func largeCall(handler http.Handler, workspace string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(strings.Repeat("x", largeRequest+1)))
	handler.ServeHTTP(recorder, request.WithContext(context.WithValue(request.Context(), workspaceKey{}, workspace)))
	return recorder
}

// Calls with a large body take a slot each, at most perWorkspace for one workspace; one more waits, and
// is refused with 503 when none frees in time. A small call never waits for them.
func TestLimitLargeLetsOnlySomeFileCallsInAtOnce(t *testing.T) {
	release := make(chan struct{})
	var entered sync.WaitGroup
	handler := limitLarge(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.ContentLength > largeRequest {
			entered.Done()
			<-release
		}
		response.WriteHeader(http.StatusOK)
	}), 3, 2, 100*time.Millisecond, time.Minute)
	busy := func(label string, got *httptest.ResponseRecorder) {
		t.Helper()
		if got.Code != http.StatusServiceUnavailable || got.Header().Get("Retry-After") == "" || !strings.Contains(got.Body.String(), "Nothing was sent") {
			t.Errorf("%s: %d %s, want 503 with Retry-After", label, got.Code, got.Body.String())
		}
	}

	var running sync.WaitGroup
	start := func(workspace string) {
		entered.Add(1)
		running.Add(1)
		go func() { defer running.Done(); largeCall(handler, workspace) }()
		entered.Wait()
	}
	start("alpha")
	start("alpha")
	busy("a third large call of one workspace", largeCall(handler, "alpha"))
	start("bravo") // another workspace still gets the last slot
	busy("a large call while every slot is taken", largeCall(handler, "charlie"))
	small := httptest.NewRecorder()
	handler.ServeHTTP(small, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`)))
	if small.Code != http.StatusOK {
		t.Errorf("a small call while the slots are taken: %d, want it served at once", small.Code)
	}
	close(release)
	running.Wait()
	entered.Add(1)
	if done := largeCall(handler, "alpha"); done.Code != http.StatusOK {
		t.Errorf("a large call after the slots freed: %d", done.Code)
	}
}

// A client that announces a large body and stalls loses its slot once the read time is over.
func TestLimitLargeFreesTheSlotOfAStalledClient(t *testing.T) {
	handler := limitLarge(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, err := io.ReadAll(request.Body); err != nil {
			return
		}
		response.WriteHeader(http.StatusOK)
	}), 1, 1, 50*time.Millisecond, 200*time.Millisecond)
	server := httptest.NewServer(handler)
	defer server.Close()

	stalled, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	fmt.Fprintf(stalled, "POST /mcp HTTP/1.1\r\nHost: test\r\nContent-Length: %d\r\n\r\nxx", largeRequest+1)
	time.Sleep(50 * time.Millisecond) // the stalled request takes the only slot

	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(strings.Repeat("x", largeRequest+1)))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stalled client still holds the slot: last status %d", response.StatusCode)
		}
	}
}
