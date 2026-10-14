package freshdesk_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
)

func TestRenamedFreshdeskSubdomainDoesNotDuplicateTheImport(t *testing.T) {
	env := newImportEnv(t)
	first := env.mustRun(t)
	before := env.tableCounts(t)
	renamed := httptest.NewTLSServer(http.HandlerFunc(env.fake.serveAPI))
	t.Cleanup(renamed.Close)
	env.fake.roots.AddCert(renamed.Certificate())

	second, err := env.run(t, context.Background(), func(o *freshdesk.Options) {
		o.BaseURL = renamed.URL
		freshdesk.UseTestNetwork(o, env.fake.roots, renamed.Listener.Addr().String(), env.fake.files.Listener.Addr().String())
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != first.Status || env.tableCounts(t) != before {
		t.Fatalf("after renaming the subdomain: status %s -> %s, rows %s -> %s", first.Status, second.Status, before, env.tableCounts(t))
	}
	var sources []string
	rows, err := env.pool.Query(context.Background(), `SELECT DISTINCT source FROM import_items WHERE workspace_id=$1`, env.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var source string
		_ = rows.Scan(&source)
		sources = append(sources, source)
	}
	rows.Close()
	if strings.Join(sources, ",") != "freshdesk:account:777" {
		t.Fatalf("ledger sources = %v, want the Freshdesk account id", sources)
	}
}

func TestSecondConcurrentRunFailsFastAndDoesNotDuplicate(t *testing.T) {
	env := newImportEnv(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once bool
	env.fake.onRequest = func(path string) {
		if path == "/api/v2/agents" && !once {
			once = true
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := env.run(t, context.Background(), nil)
		done <- err
	}()
	<-entered
	_, err := env.run(t, context.Background(), nil)
	close(release)
	if !errors.Is(err, freshdesk.ErrImportRunning) || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second run error = %v, want ErrImportRunning", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("first run: %v", err)
	}
	env.fake.onRequest = nil
	if got := env.tableCounts(t); got != fullImportRows {
		t.Fatalf("rows = %s, want one import: %s", got, fullImportRows)
	}
}
