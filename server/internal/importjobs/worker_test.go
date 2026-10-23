package importjobs_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/colliedesk/colliedesk/server/internal/importjobs"
)

func (e *env) worker(t *testing.T, keys *importjobs.Keys) *importjobs.Worker {
	t.Helper()
	return importjobs.NewWorker(importjobs.WorkerOptions{DB: e.db, Keys: keys, Interval: time.Hour,
		Heartbeat: 20 * time.Millisecond, Flush: 10 * time.Millisecond, Lease: time.Minute})
}

// tick runs one claim and waits for the jobs it started.
func tick(t *testing.T, w *importjobs.Worker) {
	t.Helper()
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Wait()
}

func (e *env) get(t *testing.T, id string) importjobs.Job {
	t.Helper()
	job, err := e.service.Get(context.Background(), e.a.id, id)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func code(job importjobs.Job) string {
	if job.ErrorCode == nil {
		return ""
	}
	return *job.ErrorCode
}

// started is an import queued for its recent pass.
func (e *env) started(t *testing.T) importjobs.Job {
	t.Helper()
	job := e.previewed(t, e.a)
	started, err := e.service.Start(context.Background(), e.a.id, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return started
}

func TestThePreviewRunsInTheBackgroundAndKeepsTheKeyForTheStart(t *testing.T) {
	e := newEnv(t, true)
	job := e.preview(t, e.a)
	w := e.worker(t, e.keys)
	var seen freshdesk.Options
	importjobs.StubImporter(w, nil, func(_ context.Context, o freshdesk.Options) (*freshdesk.PreviewResult, error) {
		seen = o
		return &freshdesk.PreviewResult{Domain: "acme.freshdesk.com", AccountID: 777, Source: "freshdesk:account:777", Agents: []freshdesk.PreviewAgent{}}, nil
	}, nil)
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StatePreviewed || got.AccountSource == nil || *got.AccountSource != "freshdesk:account:777" || !got.HasKey ||
		!strings.Contains(string(got.Preview), `"account_id": 777`) {
		t.Fatalf("job = %+v preview=%s", got, got.Preview)
	}
	if seen.APIKey != testKey || seen.BaseURL != "https://acme.freshdesk.com" || !seen.ReserveRateLimit {
		t.Fatalf("importer options = key %q base %q reserve %v", seen.APIKey, seen.BaseURL, seen.ReserveRateLimit)
	}
}

func TestAPreviewThatFailsSaysWhyAndErasesTheKey(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		code string
	}{
		"wrong key":   {freshdesk.ErrKeyRejected, importjobs.CodeKeyRejected},
		"not admin":   {freshdesk.ErrNeedsAdminKey, importjobs.CodeNeedsAdminKey},
		"free plan":   {freshdesk.ErrPlanNoAPI, importjobs.CodePlanNoAPI},
		"unavailable": {freshdesk.ErrUnavailable, importjobs.CodeUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, true)
			job := e.preview(t, e.a)
			w := e.worker(t, e.keys)
			importjobs.StubImporter(w, nil, func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error) { return nil, c.err }, nil)
			tick(t, w)
			got := e.get(t, job.ID)
			if got.State != importjobs.StateFailed || code(got) != c.code || got.HasKey || !e.keyErased(t, job.ID) {
				t.Fatalf("job = %+v code %s", got, code(got))
			}
			if got.ErrorMessage == nil || strings.Contains(*got.ErrorMessage, testKey) {
				t.Fatalf("message = %v", got.ErrorMessage)
			}
		})
	}
}

func TestTheRecentPassRunsFirstThenTheFullPassAndTheKeyIsErased(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	var modes []freshdesk.Mode
	var since *time.Time
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		modes = append(modes, o.Mode)
		if o.Mode == freshdesk.ModeRecent {
			since = o.Since
			o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 1, Total: 2, APICalls: 3})
			time.Sleep(50 * time.Millisecond)
			return &freshdesk.Report{Status: freshdesk.StatusInterim, Interim: true}, nil
		}
		if o.APIKey != testKey || o.WorkspaceSlug != e.a.slug || !o.ReserveRateLimit {
			t.Errorf("options = %+v", o)
		}
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	tick(t, w)
	got := e.get(t, job.ID)
	if len(modes) != 2 || modes[0] != freshdesk.ModeRecent || modes[1] != freshdesk.ModeFull {
		t.Fatalf("modes = %v", modes)
	}
	if want := job.StartedAt.AddDate(0, 0, -90); since == nil || !since.Equal(want) {
		t.Fatalf("since = %v, want %v (started_at - 90 days)", since, want)
	}
	if got.State != importjobs.StateFinished || got.Status == nil || *got.Status != "OK" || got.Phase != importjobs.PhaseFull ||
		!got.HasInterimReport || !got.HasReport || got.HasKey || !e.keyErased(t, job.ID) || got.FinishedAt == nil {
		t.Fatalf("job = %+v", got)
	}
	if got.Progress == nil || got.Progress.Step != "importing tickets" || got.Progress.APICalls != 3 {
		t.Fatalf("progress = %+v", got.Progress)
	}
	report, markdown, ok, err := e.service.Report(context.Background(), e.a.id, job.ID)
	if err != nil || !ok || !strings.Contains(string(report), `"status": "OK"`) && !strings.Contains(string(report), `"status":"OK"`) || markdown == nil {
		t.Fatalf("report = %s %v %v %v", report, markdown, ok, err)
	}
}

func TestARestartAfterTheRecentPassResumesWithTheFullPass(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET state='running', phase='full', interim_report='{}', claim_token=gen_random_uuid(), lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	w := e.worker(t, e.keys)
	var modes []freshdesk.Mode
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		modes = append(modes, o.Mode)
		return &freshdesk.Report{Status: freshdesk.StatusUnverified}, nil
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); len(modes) != 1 || modes[0] != freshdesk.ModeFull || got.State != importjobs.StateFinished || *got.Status != "UNVERIFIED" {
		t.Fatalf("modes=%v job=%+v", modes, got)
	}
}

func TestErrorsThatWaitingCannotFixEndTheJobAndErasesTheKey(t *testing.T) {
	for name, c := range map[string]struct {
		err  error
		code string
	}{
		"plan without API":  {freshdesk.ErrPlanNoAPI, importjobs.CodePlanNoAPI},
		"key revoked":       {freshdesk.ErrKeyRejected, importjobs.CodeKeyRejected},
		"knowledge base":    {&freshdesk.KnowledgeBaseLimitError{Problems: []string{"too many"}}, importjobs.CodeKnowledgeBase},
		"anything else":     {errors.New("list tickets: more than 300 pages share updated_at"), importjobs.CodeImportError},
		"cap on the recent": {&freshdesk.StorageCapError{Plan: "solo", Needed: 10, Used: 0, Limit: 5}, importjobs.CodeStorageCap},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, true)
			job := e.started(t)
			w := e.worker(t, e.keys)
			importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) { return nil, c.err }, nil, nil)
			tick(t, w)
			got := e.get(t, job.ID)
			if got.State != importjobs.StateFailed || code(got) != c.code || !e.keyErased(t, job.ID) || got.NextRunAt != nil {
				t.Fatalf("job = %+v code=%s", got, code(got))
			}
			if c.code == importjobs.CodePlanNoAPI && !strings.Contains(*got.ErrorMessage, "trial of a higher Freshdesk plan") {
				t.Fatalf("message = %s", *got.ErrorMessage)
			}
		})
	}
}

func TestACapRefusalOfTheFullPassSaysTheRecentTicketsStay(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		if o.Mode == freshdesk.ModeRecent {
			return &freshdesk.Report{Status: freshdesk.StatusInterim, Interim: true}, nil
		}
		return nil, &freshdesk.StorageCapError{Plan: "solo", Needed: 20 << 30, Used: 1 << 30, Limit: 10 << 30}
	}, nil, nil)
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StateFailed || code(got) != importjobs.CodeStorageCap || !got.HasInterimReport ||
		!strings.Contains(*got.ErrorMessage, "recent tickets were imported") || strings.Contains(*got.ErrorMessage, "Nothing was imported") {
		t.Fatalf("job = %+v message=%v", got, got.ErrorMessage)
	}
}

func TestAnUnavailableFreshdeskIsRetriedLaterThenGivenUpAfterADay(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return nil, freshdesk.ErrUnavailable
	}, nil, nil)
	before := time.Now()
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StateRetryWait || code(got) != importjobs.CodeUnavailable || !got.HasKey || got.NextRunAt == nil ||
		got.NextRunAt.Before(before.Add(59*time.Second)) {
		t.Fatalf("job = %+v", got)
	}
	tick(t, w) // not due yet
	if n := e.column(t, job.ID, "attempts").(int32); n != 1 {
		t.Fatalf("attempts = %d: a job in retry_wait ran before next_run_at", n)
	}
	e.exec(t, `UPDATE import_jobs SET next_run_at=now(), retrying_since=now()-interval '25 hours' WHERE id=$1`, job.ID)
	tick(t, w)
	got = e.get(t, job.ID)
	if got.State != importjobs.StateFailed || code(got) != importjobs.CodeUnavailable || !e.keyErased(t, job.ID) {
		t.Fatalf("after a day = %+v", got)
	}
}

func TestAnImportRunningFromTheCLIMakesTheJobWait(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return nil, freshdesk.ErrImportRunning
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateRetryWait || code(got) != importjobs.CodeRunningElsewhere {
		t.Fatalf("job = %+v", got)
	}
}

func TestAFailedFullPassIsRetriedWhileFailuresGoDown(t *testing.T) {
	e := newEnv(t, true)
	none := 0
	job, err := e.service.Preview(context.Background(), e.a.id, e.a.agentID, "acme.freshdesk.com", testKey, importjobs.Options{RecentDays: &none})
	if err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE import_jobs SET state='previewed', account_source='freshdesk:account:777' WHERE id=$1`, job.ID)
	if _, err := e.service.Start(context.Background(), e.a.id, job.ID); err != nil {
		t.Fatal(err)
	}
	w := e.worker(t, e.keys)
	failures := [][]freshdesk.Failure{make([]freshdesk.Failure, 3), make([]freshdesk.Failure, 3)}
	runs := 0
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		report := &freshdesk.Report{Status: freshdesk.StatusFailed, Failures: failures[runs]}
		runs++
		return report, nil
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateRetryWait || code(got) != importjobs.CodeRetryingFailures || !got.HasReport {
		t.Fatalf("after the first FAILED run = %+v", got)
	}
	e.exec(t, `UPDATE import_jobs SET next_run_at=now() WHERE id=$1`, job.ID)
	tick(t, w)
	got := e.get(t, job.ID)
	if runs != 2 || got.State != importjobs.StateFinished || *got.Status != "FAILED" || !e.keyErased(t, job.ID) {
		t.Fatalf("the same failures again must finish FAILED: runs=%d job=%+v", runs, got)
	}
}

func TestCancelStopsARunningImportWithinAHeartbeatAndKeepsWhatItImported(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	running := make(chan struct{})
	importjobs.StubImporter(w, func(ctx context.Context, _ freshdesk.Options) (*freshdesk.Report, error) {
		close(running)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-running
	if _, err := e.service.Cancel(context.Background(), e.a.id, job.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { w.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not stop after the cancel")
	}
	got := e.get(t, job.ID)
	if got.State != importjobs.StateCancelled || got.CancelRequested || !e.keyErased(t, job.ID) || got.FinishedAt == nil {
		t.Fatalf("job = %+v", got)
	}
}

func TestAWorkerThatLostItsLeaseStopsAndWritesNothing(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	running := make(chan struct{})
	importjobs.StubImporter(w, func(ctx context.Context, _ freshdesk.Options) (*freshdesk.Report, error) {
		close(running)
		<-ctx.Done()
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-running
	e.exec(t, `UPDATE import_jobs SET claim_token=gen_random_uuid() WHERE id=$1`, job.ID)
	w.Wait()
	if got := e.get(t, job.ID); got.State != importjobs.StateRunning || got.HasReport {
		t.Fatalf("a worker without the lease wrote: %+v", got)
	}
}

func TestShutdownLeavesTheJobToRunAgain(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	running := make(chan struct{})
	importjobs.StubImporter(w, func(ctx context.Context, _ freshdesk.Options) (*freshdesk.Report, error) {
		close(running)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	ctx, stop := context.WithCancel(context.Background())
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	<-running
	stop()
	w.Wait()
	got := e.get(t, job.ID)
	if got.State != importjobs.StateRunning || !got.HasKey || e.column(t, job.ID, "lease_until IS NULL AND claim_token IS NULL").(bool) != true {
		t.Fatalf("after shutdown = %+v", got)
	}
	next := e.worker(t, e.keys)
	importjobs.StubImporter(next, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return &freshdesk.Report{Status: freshdesk.StatusOK}, nil
	}, nil, nil)
	tick(t, next)
	if got := e.get(t, job.ID); got.State != importjobs.StateFinished {
		t.Fatalf("the next worker did not finish it: %+v", got)
	}
}

func TestWithoutAKeyRingARestartAsksForTheKeyAgain(t *testing.T) {
	e := newEnv(t, false)
	job := e.started(t)
	// The process that took the key is gone: a new one (new instance, empty memory) finds the job.
	e.exec(t, `UPDATE import_jobs SET updated_at=now()-interval '5 minutes', next_run_at=now()-interval '5 minutes' WHERE id=$1`, job.ID)
	restarted := importjobs.NewKeys(ringless())
	w := e.worker(t, restarted)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		t.Error("the import ran without its key")
		return nil, nil
	}, nil, nil)
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StateWaitingKey || code(got) != importjobs.CodeKeyUnavailable || got.Phase != importjobs.PhaseRecent {
		t.Fatalf("job = %+v", got)
	}
	service := importjobs.NewService(e.db, restarted)
	if _, err := service.SetKey(context.Background(), e.a.id, job.ID, testKey); err != nil {
		t.Fatal(err)
	}
	var key string
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		key = o.APIKey
		return &freshdesk.Report{Status: freshdesk.StatusOK, Interim: o.Mode == freshdesk.ModeRecent}, nil
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateFinished || key != testKey {
		t.Fatalf("after the new key: %+v key=%q", got, key)
	}
}

func TestWithoutAKeyRingAnotherLiveProcessLeavesTheJobAlone(t *testing.T) {
	e := newEnv(t, false)
	job := e.started(t)
	other := e.worker(t, importjobs.NewKeys(ringless()))
	importjobs.StubImporter(other, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		t.Error("another process ran a job whose key it does not hold")
		return nil, nil
	}, nil, nil)
	tick(t, other)
	if got := e.get(t, job.ID); got.State != importjobs.StateQueued {
		t.Fatalf("job = %+v", got)
	}
}

func TestAJobFoundRunningAfterFiveCrashesIsGivenUp(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET state='running', attempts_without_progress=4, claim_token=gen_random_uuid(), lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		t.Error("ran a job that keeps crashing")
		return nil, nil
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); got.State != importjobs.StateFailed || code(got) != importjobs.CodeNoProgress || !e.keyErased(t, job.ID) {
		t.Fatalf("job = %+v", got)
	}
}

func TestProgressClearsTheCrashCount(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET state='running', attempts_without_progress=3, claim_token=gen_random_uuid(), lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(ctx context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 1, Total: 3})
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 2, Total: 3})
		time.Sleep(50 * time.Millisecond)
		if n := e.column(t, job.ID, "attempts_without_progress").(int32); n != 0 {
			t.Errorf("attempts_without_progress = %d after progress", n)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	ctx, stop := context.WithCancel(context.Background())
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	stop()
	w.Wait()
}

// unflushed is a worker whose flusher never runs while a test lasts: only the run's own ends write progress.
func (e *env) unflushed() *importjobs.Worker {
	return importjobs.NewWorker(importjobs.WorkerOptions{DB: e.db, Keys: e.keys, Interval: time.Hour,
		Heartbeat: time.Hour, Flush: time.Hour, Lease: time.Minute})
}

func TestProgressBeforeAShutdownClearsTheCrashCount(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET state='running', attempts_without_progress=3, claim_token=gen_random_uuid(), lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	w := e.unflushed()
	progressed := make(chan struct{})
	importjobs.StubImporter(w, func(ctx context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 1, Total: 3})
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 2, Total: 3})
		close(progressed)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	ctx, stop := context.WithCancel(context.Background())
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	<-progressed
	stop()
	w.Wait()
	if n := e.column(t, job.ID, "attempts_without_progress").(int32); n != 0 {
		t.Errorf("attempts_without_progress = %d after progress and a shutdown", n)
	}
	if p := e.get(t, job.ID).Progress; p == nil || p.Done != 2 {
		t.Errorf("progress = %+v", p)
	}
}

func TestAShutdownWaitsForTheProgressTheFlusherIsWriting(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET state='running', attempts_without_progress=3, claim_token=gen_random_uuid(), lease_until=now()-interval '1 second' WHERE id=$1`, job.ID)
	// The flusher reads the clock between taking the progress and writing it: holding the clock there
	// holds it mid-flush. The importer's own clock reads are let through.
	var armed, inImporter atomic.Bool
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	w := importjobs.NewWorker(importjobs.WorkerOptions{DB: e.db, Keys: e.keys, Interval: time.Hour,
		Heartbeat: time.Hour, Flush: 5 * time.Millisecond, Lease: time.Minute, Now: func() time.Time {
			if armed.Load() && !inImporter.Load() {
				once.Do(func() { close(held); <-release })
			}
			return time.Now()
		}})
	importjobs.StubImporter(w, func(ctx context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		inImporter.Store(true)
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 1, Total: 3})
		armed.Store(true)
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 2, Total: 3})
		inImporter.Store(false)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	ctx, stop := context.WithCancel(context.Background())
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	<-held
	stop()
	// Without a wait for the flusher, the shutdown releases the claim now and the held write is lost.
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline) && e.column(t, job.ID, "claim_token") != nil; {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	w.Wait()
	if n := e.column(t, job.ID, "attempts_without_progress").(int32); n != 0 {
		t.Errorf("attempts_without_progress = %d: the shutdown did not wait for the progress being written", n)
	}
	if p := e.get(t, job.ID).Progress; p == nil || p.Done != 2 {
		t.Errorf("progress = %+v", p)
	}
}

func TestProgressBeforeAnUnavailableFreshdeskStartsTheRetryWindowAgain(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET retrying_since=now()-interval '25 hours' WHERE id=$1`, job.ID)
	w := e.unflushed()
	importjobs.StubImporter(w, func(_ context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 1, Total: 3})
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 2, Total: 3})
		return nil, freshdesk.ErrUnavailable
	}, nil, nil)
	before := time.Now()
	tick(t, w)
	got := e.get(t, job.ID)
	if got.State != importjobs.StateRetryWait || code(got) != importjobs.CodeUnavailable || got.Progress == nil || got.Progress.Done != 2 {
		t.Fatalf("job = %+v", got)
	}
	if since := e.column(t, job.ID, "retrying_since").(time.Time); since.Before(before.Add(-time.Second)) {
		t.Fatalf("retrying_since = %v, want the time of this failure", since)
	}
}

func TestAnUndoJobRemovesTheImportOfItsAccount(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewed(t, e.a)
	e.exec(t, `UPDATE import_jobs SET state='finished', status='OK', sealed_key=NULL, sealed_key_id=NULL WHERE id=$1`, job.ID)
	undo, err := e.service.Undo(context.Background(), e.a.id, e.a.agentID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := e.worker(t, e.keys)
	var options freshdesk.UndoOptions
	importjobs.StubImporter(w, nil, nil, func(_ context.Context, o freshdesk.UndoOptions) (freshdesk.UndoResult, error) {
		options = o
		return freshdesk.UndoResult{Source: o.Source, Conversations: 5}, nil
	})
	tick(t, w)
	got := e.get(t, undo.ID)
	if got.State != importjobs.StateFinished || !got.HasReport || options.Source != "freshdesk:account:777" || options.WorkspaceSlug != e.a.slug || options.DryRun {
		t.Fatalf("undo = %+v options=%+v", got, options)
	}
}

func TestAPreviewNobodyStartedIsDiscardedAfterAnHour(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewed(t, e.a)
	fresh := e.previewed(t, e.b)
	e.exec(t, `UPDATE import_jobs SET updated_at=now()-interval '61 minutes' WHERE id=$1`, job.ID)
	tick(t, e.worker(t, e.keys))
	if got := e.get(t, job.ID); got.State != importjobs.StateDiscarded || !e.keyErased(t, job.ID) {
		t.Fatalf("stale preview = %+v", got)
	}
	if got, _ := e.service.Get(context.Background(), e.b.id, fresh.ID); got.State != importjobs.StatePreviewed || !got.HasKey {
		t.Fatalf("fresh preview = %+v", got)
	}
}

func TestAServerWhoseKeyRingCannotOpenTheKeyLeavesTheJobToOneThatCan(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	other := e.worker(t, importjobs.NewKeys(ringWithID(t, "k2")))
	importjobs.StubImporter(other, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		t.Error("a server that cannot open the key ran the job")
		return nil, nil
	}, nil, nil)
	tick(t, other)
	if got := e.get(t, job.ID); got.State != importjobs.StateQueued || !got.HasKey {
		t.Fatalf("job = %+v: another server's ring took it", got)
	}
	// Left alone longer than a lease (no server holds its key any more), any worker takes it and asks for the key.
	e.exec(t, `UPDATE import_jobs SET updated_at=now()-interval '5 minutes', next_run_at=now()-interval '5 minutes' WHERE id=$1`, job.ID)
	tick(t, other)
	if got := e.get(t, job.ID); got.State != importjobs.StateWaitingKey {
		t.Fatalf("orphaned job = %+v", got)
	}
}

func TestARetryThatComesDueStaysWithTheServerThatCanOpenItsKey(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `UPDATE import_jobs SET state='retry_wait', updated_at=now()-interval '10 minutes', next_run_at=now()-interval '1 second' WHERE id=$1`, job.ID)
	other := e.worker(t, importjobs.NewKeys(ringWithID(t, "k2")))
	tick(t, other)
	if got := e.get(t, job.ID); got.State != importjobs.StateRetryWait || !got.HasKey {
		t.Fatalf("job = %+v: a due retry is not abandoned", got)
	}
}

func TestAnUndoReportsWhatItRemovedInMarkdownToo(t *testing.T) {
	e := newEnv(t, true)
	job := e.previewed(t, e.a)
	e.exec(t, `UPDATE import_jobs SET state='finished', status='OK', sealed_key=NULL, sealed_key_id=NULL WHERE id=$1`, job.ID)
	undo, err := e.service.Undo(context.Background(), e.a.id, e.a.agentID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, nil, nil, func(_ context.Context, o freshdesk.UndoOptions) (freshdesk.UndoResult, error) {
		return freshdesk.UndoResult{Source: o.Source, Conversations: 5, Contacts: 3, Tags: 2, SavedReplies: 1, KeptSavedReplies: []string{"9101"}}, nil
	})
	tick(t, w)
	_, markdown, ok, err := e.service.Report(context.Background(), e.a.id, undo.ID)
	if err != nil || !ok || markdown == nil || !strings.Contains(*markdown, "Conversations removed: 5") || !strings.Contains(*markdown, "9101") {
		t.Fatalf("markdown = %v (%v, %v)", markdown, ok, err)
	}
}

func TestAWaitForTheRateLimitKeepsTheCountsOfTheStep(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	w := e.worker(t, e.keys)
	until := time.Now().Add(time.Minute).UTC()
	importjobs.StubImporter(w, func(ctx context.Context, o freshdesk.Options) (*freshdesk.Report, error) {
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", Done: 72, Total: 300, APICalls: 200})
		o.OnProgress(freshdesk.ProgressEvent{Phase: "importing tickets", RateLimitUntil: &until, APICalls: 201})
		time.Sleep(50 * time.Millisecond)
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil, nil)
	ctx, stop := context.WithCancel(context.Background())
	if err := w.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	got := e.get(t, job.ID)
	stop()
	w.Wait()
	if p := got.Progress; p == nil || p.Done != 72 || p.Total != 300 || p.RateLimitUntil == nil {
		t.Fatalf("progress = %+v", p)
	}
}

func TestAnImportThatStopsSaysHowManyConversationsItBroughtIn(t *testing.T) {
	e := newEnv(t, true)
	job := e.started(t)
	e.exec(t, `INSERT INTO import_items(workspace_id,source,entity,source_id,status) SELECT $1,'freshdesk:account:777','ticket',n::text,'done' FROM generate_series(1,7) n`, e.a.id)
	e.exec(t, `INSERT INTO import_items(workspace_id,source,entity,source_id,status) VALUES($1,'freshdesk:account:777','ticket','8','failed')`, e.a.id)
	w := e.worker(t, e.keys)
	importjobs.StubImporter(w, func(context.Context, freshdesk.Options) (*freshdesk.Report, error) {
		return nil, freshdesk.ErrKeyRejected
	}, nil, nil)
	tick(t, w)
	if got := e.get(t, job.ID); got.ImportedConversations == nil || *got.ImportedConversations != 7 {
		t.Fatalf("imported conversations = %v", got.ImportedConversations)
	}
}

func TestAJobStartedInThisProcessRunsWithoutWaitingForTheNextTick(t *testing.T) {
	e := newEnv(t, true)
	w := importjobs.NewWorker(importjobs.WorkerOptions{DB: e.db, Keys: e.keys, Interval: time.Hour, Heartbeat: 20 * time.Millisecond, Flush: 10 * time.Millisecond, Lease: time.Minute})
	previewed := make(chan struct{}, 1)
	importjobs.StubImporter(w, nil, func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error) {
		previewed <- struct{}{}
		return &freshdesk.PreviewResult{Source: "freshdesk:account:777", Agents: []freshdesk.PreviewAgent{}}, nil
	}, nil)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go w.Run(ctx)
	time.Sleep(100 * time.Millisecond) // the first tick, with nothing to do
	e.preview(t, e.a)
	select {
	case <-previewed:
	case <-time.After(3 * time.Second):
		t.Fatal("the preview waited for the next tick (an hour)")
	}
}
