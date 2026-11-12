package health_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/health"
	"github.com/jackc/pgx/v5/pgxpool"
)

// app stands for everything behind the probes: the web app answers any path with 200, which is why a
// probe that reaches it proves nothing.
var app = http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "text/html")
	_, _ = response.Write([]byte("<!doctype html>"))
})

type probe struct {
	Live   bool            `json:"live"`
	Ready  bool            `json:"ready"`
	Checks map[string]bool `json:"checks"`
}

func ask(t *testing.T, handler http.Handler, method, path string) (*httptest.ResponseRecorder, probe) {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request.Host = "help.example.org" // a help center's custom domain: the probes answer on every host
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var body probe
	if method != http.MethodHead && recorder.Code != http.StatusMethodNotAllowed {
		if got := recorder.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("%s %s: Content-Type %q, body %s", method, path, got, recorder.Body)
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s: %v in %s", method, path, err, recorder.Body)
		}
	}
	if got := recorder.Header().Get("Cache-Control"); recorder.Code != http.StatusMethodNotAllowed && got != "no-store" {
		t.Errorf("%s %s: Cache-Control %q, want no-store so no cache answers for a dead server", method, path, got)
	}
	return recorder, body
}

func migrated(t *testing.T) (*database.DB, *pgxpool.Pool) {
	t.Helper()
	pool := databasetest.Pool(t)
	return database.New(pool), pool
}

func unreachable(t *testing.T) *database.DB {
	t.Helper()
	// Nothing listens on port 1: every connection is refused, as when PostgreSQL is down.
	pool, err := pgxpool.New(context.Background(), "postgres://colliedesk@127.0.0.1:1/colliedesk?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return database.New(pool)
}

func TestReadyWhenTheDatabaseAnswersAndHoldsEveryMigration(t *testing.T) {
	db, _ := migrated(t)
	recorder, body := ask(t, health.Wrap(app, db), http.MethodGet, "/readyz")
	if recorder.Code != http.StatusOK || !body.Ready || !body.Checks["postgres"] || !body.Checks["migrations"] {
		t.Fatalf("readyz = %d %s, want 200 with ready, postgres and migrations true", recorder.Code, recorder.Body)
	}
	if recorder, _ := ask(t, health.Wrap(app, db), http.MethodHead, "/readyz"); recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("HEAD readyz = %d with %d bytes, want 200 and no body", recorder.Code, recorder.Body.Len())
	}
}

func TestNotReadyWhenTheDatabaseIsDown(t *testing.T) {
	recorder, body := ask(t, health.Wrap(app, unreachable(t)), http.MethodGet, "/readyz")
	if recorder.Code != http.StatusServiceUnavailable || body.Ready || body.Checks["postgres"] || body.Checks["migrations"] {
		t.Fatalf("readyz = %d %s, want 503 with ready, postgres and migrations false", recorder.Code, recorder.Body)
	}
}

func TestNotReadyWhenAMigrationThisBinaryNeedsIsMissing(t *testing.T) {
	db, pool := migrated(t)
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM schema_migrations WHERE name=(SELECT max(name) FROM schema_migrations)`); err != nil {
		t.Fatal(err)
	}
	recorder, body := ask(t, health.Wrap(app, db), http.MethodGet, "/readyz")
	if recorder.Code != http.StatusServiceUnavailable || body.Ready || !body.Checks["postgres"] || body.Checks["migrations"] {
		t.Fatalf("readyz = %d %s, want 503 with postgres true and migrations false", recorder.Code, recorder.Body)
	}
}

func TestLiveEvenWhenTheDatabaseIsDown(t *testing.T) {
	recorder, body := ask(t, health.Wrap(app, unreachable(t)), http.MethodGet, "/livez")
	if recorder.Code != http.StatusOK || !body.Live {
		t.Fatalf("livez = %d %s, want 200 with live true: liveness never waits on a dependency", recorder.Code, recorder.Body)
	}
}

func TestProbesRefuseWritesAndLeaveOtherPathsToTheApp(t *testing.T) {
	handler := health.Wrap(app, unreachable(t))
	for _, path := range []string{"/livez", "/readyz"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("POST %s = %d Allow %q, want 405 with Allow: GET, HEAD", path, recorder.Code, recorder.Header().Get("Allow"))
		}
	}
	for _, path := range []string{"/", "/readyz/", "/livez.json", "/api/v1/config"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Body.String() != "<!doctype html>" {
			t.Errorf("GET %s did not reach the app: %d %s", path, recorder.Code, recorder.Body)
		}
	}
}
