package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// The real server answers the probes itself on every host, before the app, a help center or the operator
// console could answer a probe path with a page and 200.
func TestServeAnswersTheProbesOnEveryHost(t *testing.T) {
	databaseURL := cliDatabase(t)
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	t.Setenv("ATTACHMENTS_DIR", t.TempDir())
	t.Setenv("DATABASE_REQUEST_URL", "")
	for _, name := range serverconfig.MovedEnvs() {
		t.Setenv(name, "")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--listen", address}, io.Discard, databaseURL, "http://"+address)
	}()
	defer func() {
		cancel()
		<-done
	}()
	probe := func(host, path string) (int, map[string]any, error) {
		request, err := http.NewRequest(http.MethodGet, "http://"+address+path, nil)
		if err != nil {
			return 0, nil, err
		}
		request.Host = host
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		var body map[string]any
		err = json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, body, err := probe(address, "/readyz")
		if err == nil && status == http.StatusOK && body["ready"] == true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("readyz on the app's host: %d %v %v", status, body, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, host := range []string{"help.example.org", "acme.colliedesk.test"} {
		if status, body, err := probe(host, "/readyz"); err != nil || status != http.StatusOK || body["ready"] != true {
			t.Errorf("readyz on %s: %d %v %v", host, status, body, err)
		}
		if status, body, err := probe(host, "/livez"); err != nil || status != http.StatusOK || body["live"] != true {
			t.Errorf("livez on %s: %d %v %v", host, status, body, err)
		}
	}
}
