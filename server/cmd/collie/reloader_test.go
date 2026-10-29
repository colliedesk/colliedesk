package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
)

// fakeGeneration counts what the reloader does to it; its handler answers its name, and can be held.
type fakeGeneration struct {
	name    string
	started atomic.Int32
	stopped atomic.Int32
	busy    atomic.Value
	hold    chan struct{}
}

func newFake(name string) *fakeGeneration {
	fake := &fakeGeneration{name: name, hold: make(chan struct{})}
	close(fake.hold)
	fake.busy.Store("")
	return fake
}

func (f *fakeGeneration) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-f.hold
		_, _ = io.WriteString(w, f.name)
	})
}
func (f *fakeGeneration) Start(context.Context)   { f.started.Add(1) }
func (f *fakeGeneration) Stop(time.Duration) bool { f.stopped.Add(1); return true }
func (f *fakeGeneration) Busy() string            { return f.busy.Load().(string) }
func (f *fakeGeneration) setBusy(what string)     { f.busy.Store(what) }
func (f *fakeGeneration) holdRequests() (release func()) {
	f.hold = make(chan struct{})
	return func() { close(f.hold) }
}

type reloadEnv struct {
	t      *testing.T
	store  *serverconfig.Store
	swap   *swapHandler
	hot    *httpapi.Hot
	first  *fakeGeneration
	builds []*fakeGeneration
	mu     sync.Mutex
	reload *reloader
}

func newReloadEnv(t *testing.T, databaseURL string, instance string) *reloadEnv {
	t.Helper()
	t.Setenv("MAILBOX_TOKEN_KEYS", cliTestRing)
	store, err := openConfigStore(database.New(cliPool(t, databaseURL)))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	env := &reloadEnv{t: t, store: store, swap: &swapHandler{}, hot: httpapi.NewHot(httpapi.HotSettings{}), first: newFake("v0")}
	env.swap.Store(env.first.Handler())
	build := func(next serverconfig.Loaded, _ builtConfig) (served, error) {
		env.mu.Lock()
		defer env.mu.Unlock()
		fake := newFake(fmt.Sprintf("v%d", next.Version))
		env.builds = append(env.builds, fake)
		return fake, nil
	}
	env.reload = newReloader(store, build, env.hot, env.swap, env.first, loaded,
		serverconfig.Process{InstanceID: instance, Hostname: "test", BinaryVersion: "test", StartedAt: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return env
}

func (e *reloadEnv) get() string {
	recorder := httptest.NewRecorder()
	e.swap.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	return recorder.Body.String()
}

func (e *reloadEnv) save(set map[string]string) int64 {
	e.t.Helper()
	version, err := e.store.Save(context.Background(), serverconfig.Change{Set: set, Source: "cli", Actor: serverconfig.Actor{Name: "t"}})
	if err != nil {
		e.t.Fatal(err)
	}
	return version
}

func TestReloadSwapsAGenerationWithoutDroppingARequest(t *testing.T) {
	env := newReloadEnv(t, cliDatabase(t), "a")
	release := env.first.holdRequests()
	inFlight := make(chan string)
	go func() { inFlight <- env.get() }()
	time.Sleep(50 * time.Millisecond) // the request is now inside the old generation's handler
	version := env.save(map[string]string{"smtp.host": "smtp.example.net"})
	env.reload.Reload(context.Background())
	if len(env.builds) != 1 || env.builds[0].started.Load() != 1 || env.first.stopped.Load() != 1 {
		t.Fatalf("builds %d, new started %d, old stopped %d", len(env.builds), env.builds[0].started.Load(), env.first.stopped.Load())
	}
	if got := env.get(); got != fmt.Sprintf("v%d", version) {
		t.Fatalf("a new request reached %q", got)
	}
	release()
	if got := <-inFlight; got != "v0" {
		t.Fatalf("the request in flight got %q, want the old generation's answer", got)
	}
	// Nothing new: nothing happens.
	env.reload.Reload(context.Background())
	if len(env.builds) != 1 || env.first.stopped.Load() != 1 {
		t.Fatal("a reload without a new version rebuilt or stopped again")
	}
	processes, err := env.store.Processes(context.Background())
	if err != nil || len(processes) != 1 || processes[0].RunningVersion != version {
		t.Fatalf("processes = %+v, %v", processes, err)
	}
}

func TestReloadKeepsTheRunningGenerationWhenTheNewVersionDoesNotBuild(t *testing.T) {
	env := newReloadEnv(t, cliDatabase(t), "a")
	// Written past the checks: billing set in part.
	bad := env.save(map[string]string{"polar.access_token": "polar_oat_secret-canary"})
	env.reload.Reload(context.Background())
	env.reload.Reload(context.Background())
	if len(env.builds) != 0 || env.first.stopped.Load() != 0 || env.get() != "v0" {
		t.Fatal("a version that does not build replaced the running one")
	}
	history, _ := env.store.History(context.Background(), 1)
	if history[0].ID != bad || history[0].Status != "rejected" || strings.Contains(history[0].Rejected, "canary") {
		t.Fatalf("history = %+v", history[0])
	}
	processes, _ := env.store.Processes(context.Background())
	if !strings.Contains(processes[0].LastError, fmt.Sprintf("v%d refused", bad)) || strings.Contains(processes[0].LastError, "canary") {
		t.Fatalf("last error = %q", processes[0].LastError)
	}
	audit, _ := env.store.AuditLog(context.Background(), "", "", time.Time{}, 10)
	rejections := 0
	for _, entry := range audit {
		if entry.Action == "config.rejected" {
			rejections++
		}
	}
	if rejections != 1 {
		t.Fatalf("%d config.rejected lines, want 1", rejections)
	}
}

func TestReloadAppliesHotSettingsWithoutAReServe(t *testing.T) {
	env := newReloadEnv(t, cliDatabase(t), "a")
	env.save(map[string]string{"internal_signup.emails": "@hclabs.co", "problem_report.workspace": "acme"})
	env.reload.Reload(context.Background())
	if len(env.builds) != 0 || env.first.stopped.Load() != 0 {
		t.Fatal("a change of settings read at each use rebuilt the generation")
	}
	if got := env.hot.Load().ProblemReportWorkspace; got != "acme" {
		t.Fatalf("hot problem report workspace = %q", got)
	}
}

func TestReloadWaitsForAnImportThenApplies(t *testing.T) {
	env := newReloadEnv(t, cliDatabase(t), "a")
	env.first.setBusy("an import running in the app")
	version := env.save(map[string]string{"smtp.host": "smtp.example.net", "problem_report.workspace": "acme"})
	env.reload.Reload(context.Background())
	if len(env.builds) != 0 || env.first.stopped.Load() != 0 {
		t.Fatal("the generation running an import was replaced")
	}
	if env.hot.Load().ProblemReportWorkspace != "acme" {
		t.Fatal("the settings read at each use waited for the import too")
	}
	processes, _ := env.store.Processes(context.Background())
	if processes[0].WaitingFor != "an import running in the app" || processes[0].RunningVersion == version {
		t.Fatalf("process = %+v", processes[0])
	}
	env.first.setBusy("")
	env.reload.Reload(context.Background())
	if len(env.builds) != 1 || env.get() != fmt.Sprintf("v%d", version) {
		t.Fatal("the new configuration did not apply once the import ended")
	}
}

func TestTwoProcessesReachTheSameVersion(t *testing.T) {
	databaseURL := cliDatabase(t)
	a, b := newReloadEnv(t, databaseURL, "a"), newReloadEnv(t, databaseURL, "b")
	version := a.save(map[string]string{"smtp.host": "smtp.example.net"})
	a.reload.Reload(context.Background())
	b.reload.Reload(context.Background())
	processes, err := a.store.Processes(context.Background())
	if err != nil || len(processes) != 2 {
		t.Fatalf("processes = %+v, %v", processes, err)
	}
	for _, process := range processes {
		if process.RunningVersion != version {
			t.Fatalf("%s runs v%d, want v%d", process.InstanceID, process.RunningVersion, version)
		}
	}
}

// The real server applies a change saved with the CLI while it serves, without a restart.
func TestServeAppliesASavedChangeWithoutARestart(t *testing.T) {
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
	serverProvider := func() (bool, error) {
		response, err := http.Get("http://" + address + "/api/v1/config")
		if err != nil {
			return false, err
		}
		defer response.Body.Close()
		var config struct {
			AI struct {
				ServerProvider bool `json:"server_provider"`
			} `json:"ai"`
		}
		return config.AI.ServerProvider, json.NewDecoder(response.Body).Decode(&config)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		on, err := serverProvider()
		if err == nil && !on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not start: %v %v", on, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"config", "set", "ai.server.provider=openai_compatible", "ai.server.base_url=http://127.0.0.1:9/v1",
		"ai.server.models=draft=local-model"}, &output, databaseURL, "http://"+address); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(reloadEvery + 10*time.Second)
	for {
		if on, _ := serverProvider(); on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the running server did not apply the saved AI provider")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A worker started with a bare go statement would outlive its generation and run twice after a re-serve:
// every worker of serve goes through generation.Go or, when no setting changes it, process.Go.
func TestServeStartsWorkersOnlyThroughAGenerationOrTheProcess(t *testing.T) {
	allowed := map[string]bool{
		"generation.go: (*process).Go": true, "generation.go: (*generation).Start": true, "generation.go: (*generation).Stop": true,
		"reloader.go: (*reloader).Run": true,
		// The HTTP server itself, and the wait for GitHub sends at shutdown.
		"serve.go: serve": true,
	}
	fset := token.NewFileSet()
	for _, file := range []string{"serve.go", "generation.go", "reloader.go"} {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				var receiver strings.Builder
				_ = printer.Fprint(&receiver, fset, fn.Recv.List[0].Type)
				name = "(" + receiver.String() + ")." + name
			}
			ast.Inspect(fn, func(node ast.Node) bool {
				if _, ok := node.(*ast.GoStmt); ok && !allowed[file+": "+name] {
					t.Errorf("%s: %s starts a goroutine; start workers with generation.Go or process.Go", fset.Position(node.Pos()), name)
				}
				return true
			})
		}
	}
}
