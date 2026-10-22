package importjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/freshdesk"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Worker timing defaults; WorkerOptions may shorten them in tests.
const (
	defaultInterval  = 5 * time.Second
	defaultLease     = 2 * time.Minute
	defaultHeartbeat = 30 * time.Second
	defaultFlush     = 2 * time.Second
	defaultWorkers   = 2
	// retryWindow is how long a job may stay unavailable without progress before it fails.
	retryWindow = 24 * time.Hour
	// maxCrashes is how often a job may be found running (its worker gone) without progress in between.
	maxCrashes = 5
	// maxFullRetries is how often a FAILED full pass runs again while its failures go down.
	maxFullRetries = 2
	// etaWindow is the span the remaining-time estimate is computed over.
	etaWindow = 5 * time.Minute
)

// WorkerOptions configure the worker of one process.
type WorkerOptions struct {
	// Pool is the owner pool the importer runs on, as for the CLI.
	Pool    *pgxpool.Pool
	DB      *database.DB
	Keys    *Keys
	Storage attachments.Storage
	// Cloud, PlansOnSale and PaidStorageBytes are what the CLI passes the importer on this server.
	Cloud            bool
	PlansOnSale      []string
	PaidStorageBytes int64
	// BaseURL turns an account host into the API root; nil means https://<host>.
	BaseURL func(host string) string
	// Configure changes the importer options of every run; tests and end-to-end builds point the network at a fake.
	Configure func(*freshdesk.Options)
	// Workers is how many jobs run at once in this process; default 2.
	Workers                           int
	Interval, Lease, Heartbeat, Flush time.Duration
	Now                               func() time.Time
	Logger                            *slog.Logger
}

// Worker runs due import jobs. One per process; several processes share the table by leases.
type Worker struct {
	options WorkerOptions
	slots   chan struct{}
	wg      sync.WaitGroup
	// The importer; tests replace it (export_test.go) to drive the job's states.
	runImport func(context.Context, *pgxpool.Pool, freshdesk.Options) (*freshdesk.Report, error)
	preview   func(context.Context, freshdesk.Options) (*freshdesk.PreviewResult, error)
	undo      func(context.Context, *pgxpool.Pool, attachments.Storage, freshdesk.UndoOptions) (freshdesk.UndoResult, error)
}

// NewWorker fills the defaults of options.
func NewWorker(options WorkerOptions) *Worker {
	if options.Workers <= 0 {
		options.Workers = defaultWorkers
	}
	for _, d := range []struct {
		value *time.Duration
		def   time.Duration
	}{{&options.Interval, defaultInterval}, {&options.Lease, defaultLease}, {&options.Heartbeat, defaultHeartbeat}, {&options.Flush, defaultFlush}} {
		if *d.value <= 0 {
			*d.value = d.def
		}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.BaseURL == nil {
		options.BaseURL = func(host string) string { return "https://" + host }
	}
	return &Worker{options: options, slots: make(chan struct{}, options.Workers), runImport: freshdesk.Run, preview: freshdesk.Preview, undo: freshdesk.Undo}
}

// Run claims and runs due jobs until ctx ends, then waits for the running ones to stop. A job stopped
// by shutdown keeps its state; its lease expires and a worker runs it again.
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.options.Interval)
	defer ticker.Stop()
	for {
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			w.options.Logger.Error("import jobs", "error", err)
		}
		select {
		case <-ctx.Done():
			w.wg.Wait()
			return
		case <-ticker.C:
		}
	}
}

// Tick expires stale previews and starts the due jobs that fit in the free slots. It does not wait
// for them; Wait does.
func (w *Worker) Tick(ctx context.Context) error {
	expired, err := expirePreviews(ctx, w.options.DB, w.options.Now().UTC())
	w.options.Keys.forget(expired...)
	free := cap(w.slots) - len(w.slots)
	if free == 0 || err != nil {
		return err
	}
	jobs, err := claimDue(ctx, w.options.DB, w.options.Keys.instance, w.options.Keys.ring.IDs(), w.options.Now().UTC(), free, w.options.Lease)
	for _, item := range jobs {
		w.slots <- struct{}{}
		w.wg.Add(1)
		go func() {
			defer func() { <-w.slots; w.wg.Done() }()
			if err := w.process(ctx, item); err != nil && !errors.Is(err, errLeaseLost) && ctx.Err() == nil {
				w.options.Logger.Error("import job", "job", item.id, "error", err)
			}
		}()
	}
	return err
}

// Wait blocks until every job this worker started has stopped.
func (w *Worker) Wait() { w.wg.Wait() }

// errCancelRequested and errLeaseLost end a run's context; context.Cause tells them apart.
var errCancelRequested = errors.New("cancel requested")

// process runs one claimed job to its next resting state.
func (w *Worker) process(parent context.Context, item claimed) error {
	job, err := loadRun(parent, w.options.DB, item)
	if err != nil {
		return err
	}
	now := w.options.Now().UTC()
	if job.attemptsNoProgress >= maxCrashes {
		return w.end(parent, item, StateFailed, "", CodeNoProgress,
			fmt.Sprintf("The import stopped %d times without getting further; it was given up. Start it again, or report the problem.", job.attemptsNoProgress), nil, nil)
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	run := &jobRun{worker: w, item: item, job: job, cancel: cancel}
	stop := run.keepAlive(ctx)
	defer stop()
	if job.Kind == KindUndo {
		return run.undo(ctx)
	}
	key, err := w.options.Keys.open(item.workspaceID, item.id, job.sealedKey, job.sealedKeyID)
	if err != nil {
		message := "The server cannot read the Freshdesk API key any more (it restarted, or its key ring changed). Enter the key again to go on."
		return run.update(ctx, `state='waiting_key', error_code=$4, error_message=$5, sealed_key=NULL, sealed_key_id=NULL, claim_token=NULL, lease_until=NULL, updated_at=$6`,
			CodeKeyUnavailable, message, now)
	}
	options := w.importOptions(job, key, run.onProgress)
	switch job.Phase {
	case PhasePreview:
		return run.preview(ctx, options)
	case PhaseRecent:
		if err := run.recent(ctx, options); err != nil || run.ended || ctx.Err() != nil {
			return err
		}
		fallthrough
	default:
		return run.full(ctx, options)
	}
}

func (w *Worker) importOptions(job runRow, key string, onProgress func(freshdesk.ProgressEvent)) freshdesk.Options {
	options := freshdesk.Options{WorkspaceSlug: job.slug, BaseURL: w.options.BaseURL(job.Domain), APIKey: key, Storage: w.options.Storage,
		Cloud: w.options.Cloud, PlansOnSale: w.options.PlansOnSale, PaidStorageBytes: w.options.PaidStorageBytes, ReserveRateLimit: true,
		IncludePersonalCannedResponses: job.Options.IncludePersonalCannedResponses, HelpCenterPortal: job.Options.HelpCenterPortal, OnProgress: onProgress}
	if w.options.Configure != nil {
		w.options.Configure(&options)
	}
	return options
}

// jobRun is one claimed job in flight.
type jobRun struct {
	worker *Worker
	item   claimed
	job    runRow
	cancel context.CancelCauseFunc
	// ended is set once the job reached a state of its own (it is no longer this run's to write).
	ended bool

	mu         sync.Mutex
	latest     *Progress
	dirty      bool
	progressed bool
	samples    []sample
}

type sample struct {
	at   time.Time
	step string
	done int
}

func (r *jobRun) now() time.Time { return r.worker.options.Now().UTC() }

func (r *jobRun) update(ctx context.Context, sql string, args ...any) error {
	err := update(context.WithoutCancel(ctx), r.worker.options.DB, r.item, sql, args...)
	if errors.Is(err, errLeaseLost) {
		r.cancel(errLeaseLost)
	}
	return err
}

// onProgress keeps the newest event; the flusher writes it. It runs on importer goroutines.
func (r *jobRun) onProgress(event freshdesk.ProgressEvent) {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.latest != nil && event.Phase == r.latest.Step && event.Done > r.latest.Done {
		r.progressed = true
	}
	progress := Progress{Step: event.Phase, Done: event.Done, Total: event.Total, APICalls: event.APICalls, UpdatedAt: now}
	if event.RateLimitUntil != nil {
		progress.RateLimitUntil = event.RateLimitUntil
		// A wait says nothing about the counts: the step keeps the ones it had.
		if r.latest != nil && r.latest.Step == event.Phase {
			progress.Done, progress.Total = r.latest.Done, r.latest.Total
		}
	} else if r.latest != nil && r.latest.RateLimitUntil != nil && r.latest.RateLimitUntil.After(now) && r.latest.Step == event.Phase {
		progress.RateLimitUntil = r.latest.RateLimitUntil
	}
	if event.RateLimitUntil == nil && event.Phase != "" {
		r.samples = append(r.samples, sample{at: now, step: event.Phase, done: event.Done})
		r.samples = trimSamples(r.samples, now, event.Phase)
		progress.ETASeconds = eta(r.samples, event.Done, event.Total)
	}
	r.latest, r.dirty = &progress, true
}

// trimSamples keeps the samples of step from the last etaWindow.
func trimSamples(samples []sample, now time.Time, step string) []sample {
	kept := samples[:0]
	for _, s := range samples {
		if s.step == step && now.Sub(s.at) <= etaWindow {
			kept = append(kept, s)
		}
	}
	return kept
}

// eta is the rest of a step at the pace of the samples; nil before a minute of samples or without a total.
func eta(samples []sample, done, total int) *int {
	if total <= 0 || len(samples) < 2 || done >= total {
		return nil
	}
	first, last := samples[0], samples[len(samples)-1]
	elapsed := last.at.Sub(first.at)
	if elapsed < time.Minute || last.done <= first.done {
		return nil
	}
	seconds := int(elapsed.Seconds() * float64(total-done) / float64(last.done-first.done))
	return &seconds
}

// keepAlive renews the lease and reads a cancel request every heartbeat, and writes progress every
// flush. A renewal that finds another claim stops the run.
func (r *jobRun) keepAlive(ctx context.Context) func() {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		heartbeat := time.NewTicker(r.worker.options.Heartbeat)
		flush := time.NewTicker(r.worker.options.Flush)
		defer heartbeat.Stop()
		defer flush.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-flush.C:
				r.flush(ctx)
			case <-heartbeat.C:
				r.heartbeat(ctx)
			}
		}
	}()
	return func() { close(done); <-finished }
}

func (r *jobRun) heartbeat(ctx context.Context) {
	var cancelRequested bool
	err := r.worker.options.DB.InWorkspace(context.WithoutCancel(ctx), r.item.workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.WithoutCancel(ctx), `UPDATE import_jobs SET lease_until=$4 WHERE workspace_id=$1 AND id=$2 AND claim_token=$3 RETURNING cancel_requested_at IS NOT NULL`,
			r.item.workspaceID, r.item.id, r.item.token, r.now().Add(r.worker.options.Lease)).Scan(&cancelRequested)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		r.cancel(errLeaseLost)
	case err != nil:
		r.worker.options.Logger.Error("import job heartbeat", "job", r.item.id, "error", err)
	case cancelRequested:
		r.cancel(errCancelRequested)
	}
}

// flush writes the newest progress when it changed; progress clears the crash and retry counters.
func (r *jobRun) flush(ctx context.Context) {
	r.mu.Lock()
	latest, dirty, progressed := r.latest, r.dirty, r.progressed
	r.dirty, r.progressed = false, false
	r.mu.Unlock()
	if !dirty {
		return
	}
	encoded, err := json.Marshal(latest)
	if err != nil {
		return
	}
	err = r.update(ctx, `progress=$4, updated_at=$5,
		attempts_without_progress=CASE WHEN $6 THEN 0 ELSE attempts_without_progress END,
		retrying_since=CASE WHEN $6 THEN NULL ELSE retrying_since END`, encoded, r.now(), progressed)
	if err != nil && !errors.Is(err, errLeaseLost) {
		r.worker.options.Logger.Error("import job progress", "job", r.item.id, "error", err)
	}
}

func (r *jobRun) preview(ctx context.Context, options freshdesk.Options) error {
	result, err := r.worker.preview(ctx, options)
	if errors.Is(err, freshdesk.ErrUnavailable) && ctx.Err() == nil {
		// A preview is not retried in the background: the person is waiting for it and can ask again.
		return r.end(ctx, StateFailed, "", CodeUnavailable, err.Error(), nil, nil)
	}
	if err != nil {
		return r.fail(ctx, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	r.ended = true
	return r.update(ctx, `state='previewed', account_source=$4, preview=$5, claim_token=NULL, lease_until=NULL, updated_at=$6`,
		result.Source, encoded, r.now())
}

// recent runs the recent pass and saves the move to the full pass before the full pass starts.
func (r *jobRun) recent(ctx context.Context, options freshdesk.Options) error {
	since := r.job.StartedAt
	if since == nil {
		started := r.now()
		since = &started
	}
	from := since.AddDate(0, 0, -r.job.Options.recentDays()).UTC()
	options.Mode, options.Since = freshdesk.ModeRecent, &from
	report, err := r.worker.runImport(ctx, r.worker.options.Pool, options)
	if err != nil {
		return r.fail(ctx, err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	r.flush(ctx)
	r.job.HasInterimReport = true
	return r.update(ctx, `phase='full', interim_report=$4, updated_at=$5`, encoded, r.now())
}

func (r *jobRun) full(ctx context.Context, options freshdesk.Options) error {
	report, err := r.worker.runImport(ctx, r.worker.options.Pool, options)
	if err != nil {
		var capErr *freshdesk.StorageCapError
		if errors.As(err, &capErr) && r.job.HasInterimReport {
			return r.end(ctx, StateFailed, "", CodeStorageCap, fmt.Sprintf(
				"The recent tickets were imported, but the rest of the Freshdesk account needs %s of attachments and %s is free under the %s cap of the %s plan, so it was not imported. Upgrade the plan (Billing > Manage billing) and start the import again, or undo it.",
				domain.FormatBytes(capErr.Needed), domain.FormatBytes(max(capErr.Limit-capErr.Used, 0)), domain.FormatBytes(capErr.Limit), domain.PlanName(capErr.Plan)), nil, nil)
		}
		return r.fail(ctx, err)
	}
	r.flush(ctx)
	encoded, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if report.Status == freshdesk.StatusFailed && r.job.fullRetries < maxFullRetries && fewerFailures(report, r.job.report) {
		r.ended = true
		return r.update(ctx, `state='retry_wait', report=$4, report_markdown=$5, error_code=$6, error_message=$7, next_run_at=$8, full_retries=full_retries+1,
			claim_token=NULL, lease_until=NULL, updated_at=$9`, encoded, report.Markdown(), CodeRetryingFailures,
			fmt.Sprintf("%d item(s) failed; the import tries them again.", len(report.Failures)), r.now().Add(time.Minute), r.now())
	}
	return r.end(ctx, StateFinished, report.Status, "", "", encoded, ptr(report.Markdown()))
}

// fewerFailures reports whether a FAILED report is worth another run: the first one is, a later one
// when it failed less than the report before it.
func fewerFailures(report *freshdesk.Report, previous []byte) bool {
	if previous == nil {
		return true
	}
	var before freshdesk.Report
	if err := json.Unmarshal(previous, &before); err != nil {
		return false
	}
	return len(report.Failures) < len(before.Failures)
}

func (r *jobRun) undo(ctx context.Context) error {
	if r.job.AccountSource == nil {
		return r.end(ctx, StateFailed, "", CodeImportError, "The import to undo has no Freshdesk account.", nil, nil)
	}
	result, err := r.worker.undo(ctx, r.worker.options.Pool, r.worker.options.Storage, freshdesk.UndoOptions{WorkspaceSlug: r.job.slug, Source: *r.job.AccountSource})
	if err != nil {
		return r.fail(ctx, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return r.end(ctx, StateFinished, "", "", "", encoded, ptr(result.Markdown()))
}

// fail turns a run error into the job's next state: retry what may pass later, end the rest.
func (r *jobRun) fail(ctx context.Context, err error) error {
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errCancelRequested):
		return r.end(ctx, StateCancelled, "", "", "", nil, nil)
	case errors.Is(cause, errLeaseLost):
		return errLeaseLost
	case ctx.Err() != nil:
		// Shutdown: release the lease so a worker runs the job again soon; the state stays.
		return r.update(ctx, `claim_token=NULL, lease_until=NULL, attempts_without_progress=GREATEST(attempts_without_progress-1,0)`)
	}
	var capErr *freshdesk.StorageCapError
	var kbErr *freshdesk.KnowledgeBaseLimitError
	switch {
	case errors.Is(err, freshdesk.ErrKeyRejected):
		return r.end(ctx, StateFailed, "", CodeKeyRejected, "Freshdesk refused the API key (HTTP 401). Check it in Freshdesk (Profile settings > API key) and start a new import.", nil, nil)
	case errors.Is(err, freshdesk.ErrNeedsAdminKey):
		return r.end(ctx, StateFailed, "", CodeNeedsAdminKey, err.Error(), nil, nil)
	case errors.Is(err, freshdesk.ErrPlanNoAPI):
		return r.end(ctx, StateFailed, "", CodePlanNoAPI,
			"This Freshdesk account has no API calls: the Free plan has none, and a trial of a higher plan that ended falls back to it. Start a 14-day trial of a higher Freshdesk plan, then import again. What was imported stays.", nil, nil)
	case errors.As(err, &capErr):
		return r.end(ctx, StateFailed, "", CodeStorageCap, err.Error(), nil, nil)
	case errors.As(err, &kbErr):
		return r.end(ctx, StateFailed, "", CodeKnowledgeBase, err.Error(), nil, nil)
	case errors.Is(err, freshdesk.ErrUnavailable):
		return r.retry(ctx, CodeUnavailable, err)
	case errors.Is(err, freshdesk.ErrImportRunning):
		return r.retry(ctx, CodeRunningElsewhere, err)
	default:
		return r.end(ctx, StateFailed, "", CodeImportError, err.Error(), nil, nil)
	}
}

// retry waits and runs the job again, until it was unavailable for retryWindow without progress.
func (r *jobRun) retry(ctx context.Context, code string, cause error) error {
	now := r.now()
	since := now
	if r.job.retryingSince != nil {
		since = *r.job.retryingSince
	}
	if now.Sub(since) >= retryWindow {
		return r.end(ctx, StateFailed, "", code, fmt.Sprintf("Gave up after %s without progress: %v", retryWindow, cause), nil, nil)
	}
	delay := retryDelay(now.Sub(since))
	if code == CodeRunningElsewhere {
		delay = time.Minute
	}
	r.ended = true
	return r.update(ctx, `state='retry_wait', error_code=$4, error_message=$5, next_run_at=$6, retrying_since=COALESCE(retrying_since,$7),
		claim_token=NULL, lease_until=NULL, updated_at=$7`, code, cause.Error(), now.Add(delay), now)
}

// retryDelay backs off 1, 2, 5, 10, then 30 minutes over a streak of retries.
func retryDelay(streak time.Duration) time.Duration {
	switch {
	case streak < time.Minute:
		return time.Minute
	case streak < 3*time.Minute:
		return 2 * time.Minute
	case streak < 8*time.Minute:
		return 5 * time.Minute
	case streak < 18*time.Minute:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// end moves the job to a terminal state and erases its key in the same update.
func (r *jobRun) end(ctx context.Context, state, status, code, message string, report []byte, markdown *string) error {
	r.ended = true
	r.flush(ctx)
	err := r.update(ctx, endSQL+`, report=COALESCE($9, report), report_markdown=COALESCE($10, report_markdown)`,
		state, nullable(status), nullable(code), nullable(message), r.now(), report, markdown)
	r.worker.options.Keys.forget(r.item.id)
	return err
}

// end ends a job that never started running (the crash guard), outside a jobRun.
func (w *Worker) end(ctx context.Context, item claimed, state, status, code, message string, report []byte, markdown *string) error {
	err := update(ctx, w.options.DB, item, endSQL+`, report=COALESCE($9, report), report_markdown=COALESCE($10, report_markdown)`,
		state, nullable(status), nullable(code), nullable(message), w.options.Now().UTC(), report, markdown)
	w.options.Keys.forget(item.id)
	return err
}

func ptr[T any](value T) *T { return &value }
