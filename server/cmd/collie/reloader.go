package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// served is a running generation as the reloader sees it.
type served interface {
	Handler() http.Handler
	Start(ctx context.Context)
	Stop(timeout time.Duration) bool
	// Busy names what the generation must finish before it is replaced, empty when nothing.
	Busy() string
}

// swapHandler serves through the current generation's handler; a request already in an old one finishes
// there.
type swapHandler struct{ current atomic.Pointer[http.Handler] }

func (s *swapHandler) Store(handler http.Handler) { s.current.Store(&handler) }

func (s *swapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.current.Load()).ServeHTTP(w, r)
}

const (
	// reloadEvery is how often a process reads which version it should run, besides LISTEN.
	reloadEvery = 5 * time.Second
	// stopWorkersWithin bounds the wait for the old generation's workers.
	stopWorkersWithin = 30 * time.Second
	// deferredTooLong is how long a re-serve may wait for an import before the log says so in red.
	deferredTooLong = 6 * time.Hour
)

// reloader applies new versions of the configuration in this process: the hot settings at once, the
// others by building a new generation and swapping it in (docs/internals/operator-console.md › Re-serve).
type reloader struct {
	store  *serverconfig.Store
	build  func(serverconfig.Loaded, builtConfig) (served, error)
	hot    *httpapi.Hot
	swap   *swapHandler
	logger *slog.Logger
	now    func() time.Time

	mu       sync.Mutex
	current  served
	running  serverconfig.Loaded
	products string
	process  serverconfig.Process
	rejected map[int64]bool
	deferred time.Time
}

func newReloader(store *serverconfig.Store, build func(serverconfig.Loaded, builtConfig) (served, error), hot *httpapi.Hot, swap *swapHandler,
	first served, running serverconfig.Loaded, process serverconfig.Process, logger *slog.Logger) *reloader {
	process.RunningVersion = running.Version
	return &reloader{store: store, build: build, hot: hot, swap: swap, logger: logger, now: time.Now, current: first, running: running,
		products: serverconfig.Fingerprint(running.Products()), process: process, rejected: map[int64]bool{}}
}

// Run reloads every reloadEvery and whenever the configuration is notified, until ctx ends.
func (r *reloader) Run(ctx context.Context) {
	notify := make(chan struct{}, 1)
	go func() {
		for ctx.Err() == nil {
			if err := r.store.Listen(ctx, notify); err != nil && ctx.Err() == nil {
				r.logger.Warn("listening for configuration changes failed; polling only", "error", err)
				select {
				case <-ctx.Done():
				case <-time.After(reloadEvery):
				}
			}
		}
	}()
	ticker := time.NewTicker(reloadEvery)
	defer ticker.Stop()
	for {
		r.Reload(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-notify:
		}
	}
}

// Reload applies the version the process should run, if it differs from the one it runs, and records
// what the process runs.
func (r *reloader) Reload(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reload(ctx); err != nil && ctx.Err() == nil {
		r.logger.Error("apply the configuration", "error", err)
	}
	if err := r.store.Heartbeat(ctx, r.process); err != nil && ctx.Err() == nil {
		r.logger.Error("record this process", "error", err)
	}
}

func (r *reloader) reload(ctx context.Context) error {
	next, err := r.store.Current(ctx)
	if err != nil {
		return err
	}
	products := serverconfig.Fingerprint(next.Products())
	if next.Version == r.running.Version && products == r.products {
		r.process.WaitingFor = ""
		return nil
	}
	if r.rejected[next.Version] && products == r.products {
		return nil
	}
	built, err := checkSnapshot(next.Snapshot)
	if err != nil {
		r.rejected[next.Version] = true
		r.process.LastError = fmt.Sprintf("v%d refused: %v", next.Version, err)
		r.logger.Error("a new configuration does not make a valid setting; still running the previous one", "version", next.Version, "running", r.running.Version, "error", err)
		return r.store.Reject(ctx, next.Version, err.Error(), serverconfig.Actor{Kind: "server", Name: r.process.Hostname})
	}
	changed := changedSettings(r.running.Snapshot, next.Snapshot)
	if products == r.products && allHot(changed) {
		r.hot.Store(hotSettings(built))
		return r.applied(ctx, next, products, changed, "hot")
	}
	if busy := r.current.Busy(); busy != "" {
		// The settings read at each use apply now; the rest waits for the import, and applies by itself.
		r.hot.Store(hotSettings(built))
		if r.deferred.IsZero() {
			r.deferred = r.now()
		}
		r.process.WaitingFor = busy
		if r.now().Sub(r.deferred) > deferredTooLong {
			r.logger.Error("a new configuration has waited more than 6 hours for an import to end", "version", next.Version, "waiting_for", busy)
		}
		return nil
	}
	fresh, err := r.build(next, built)
	if err != nil {
		r.rejected[next.Version] = true
		r.process.LastError = fmt.Sprintf("v%d refused: %v", next.Version, err)
		return r.store.Reject(ctx, next.Version, err.Error(), serverconfig.Actor{Kind: "server", Name: r.process.Hostname})
	}
	if !r.current.Stop(stopWorkersWithin) {
		r.logger.Warn("the previous configuration's workers did not stop within 30 seconds; starting the new ones", "version", next.Version)
	}
	fresh.Start(ctx)
	r.hot.Store(hotSettings(built))
	r.swap.Store(fresh.Handler())
	r.current = fresh
	return r.applied(ctx, next, products, changed, "re-serve")
}

func (r *reloader) applied(ctx context.Context, next serverconfig.Loaded, products string, changed []string, how string) error {
	r.running, r.products, r.deferred = next, products, time.Time{}
	r.process.RunningVersion, r.process.WaitingFor, r.process.LastError = next.Version, "", ""
	r.logger.Info("applied a new configuration", "version", next.Version, "how", how, "settings", strings.Join(changed, ","))
	if err := r.store.MarkGood(ctx, next.Version); err != nil {
		return err
	}
	return r.store.Audit(ctx, serverconfig.Actor{Kind: "server", Name: r.process.Hostname}, "config.apply", next.Version, changed, map[string]any{"how": how, "instance": r.process.InstanceID})
}

// Stop ends the current generation's workers, for the end of the process.
func (r *reloader) Stop(timeout time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.current.Stop(timeout)
}

// changedSettings lists the keys whose value differs between two snapshots.
func changedSettings(before, after serverconfig.Snapshot) []string {
	old, next := before.Values(), after.Values()
	var changed []string
	for key, value := range next {
		if previous, ok := old[key]; !ok || previous != value {
			changed = append(changed, key)
		}
	}
	for key := range old {
		if _, ok := next[key]; !ok {
			changed = append(changed, key)
		}
	}
	slices.Sort(changed)
	return slices.Compact(changed)
}

// allHot reports whether every changed key is read at each use, so no re-serve is needed.
func allHot(changed []string) bool {
	hot := map[string]bool{}
	for _, key := range serverconfig.Keys() {
		hot[key.Name] = key.Hot
	}
	for _, key := range changed {
		if !hot[key] {
			return false
		}
	}
	return true
}
