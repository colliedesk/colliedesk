// Package health answers the two probes a proxy, an orchestrator or an uptime monitor asks, on every host
// the server answers for (app, help centers, operator console):
//
//   - GET /livez: the process answers HTTP. It checks no dependency, so a database outage never gets a
//     working process restarted.
//   - GET /readyz: the process can serve a request: the request login reaches PostgreSQL and the database
//     holds every migration this binary needs. 200 when every check passes, 503 otherwise; the body names
//     each check, never why it failed (that goes to the log).
//
// Without them these paths fell through to the web app, which answers any path with 200.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
)

const (
	LivePath  = "/livez"
	ReadyPath = "/readyz"
)

// checkTimeout bounds /readyz: a monitor waits about this long, never for a hung connection pool.
const checkTimeout = 2 * time.Second

type check struct {
	name string
	run  func(context.Context) error
}

// Wrap answers /livez and /readyz itself and passes every other request to next.
func Wrap(next http.Handler, db *database.DB) http.Handler {
	checks := []check{
		{"postgres", db.Ping},
		{"migrations", func(ctx context.Context) error {
			pending, err := db.PendingMigrations(ctx)
			if err == nil && len(pending) > 0 {
				err = fmt.Errorf("migrations not applied (run collie migrate): %s", strings.Join(pending, ", "))
			}
			return err
		}},
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		path := request.URL.Path
		if path != LivePath && path != ReadyPath {
			next.ServeHTTP(response, request)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			response.Header().Set("Allow", "GET, HEAD")
			http.Error(response, "Use GET or HEAD.", http.StatusMethodNotAllowed)
			return
		}
		if path == LivePath {
			write(response, request, http.StatusOK, map[string]any{"live": true})
			return
		}
		results := run(request.Context(), checks)
		ready := true
		for _, ok := range results {
			ready = ready && ok
		}
		status := http.StatusOK
		if !ready {
			status = http.StatusServiceUnavailable
		}
		write(response, request, status, map[string]any{"ready": ready, "checks": results})
	})
}

// run starts every check at once under one deadline, so /readyz takes as long as its slowest check.
func run(ctx context.Context, checks []check) map[string]bool {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	results := make(map[string]bool, len(checks))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := c.run(ctx)
			if err != nil {
				slog.Warn("readiness check failed", "check", c.name, "error", err)
			}
			mu.Lock()
			results[c.name] = err == nil
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

func write(response http.ResponseWriter, request *http.Request, status int, body map[string]any) {
	header := response.Header()
	header.Set("Content-Type", "application/json")
	// A cache must never answer for a server that has since gone down.
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	if request.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(response).Encode(body)
}
