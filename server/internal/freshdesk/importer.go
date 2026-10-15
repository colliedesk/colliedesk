// Package freshdesk imports a Freshdesk account into a Colliedesk workspace
// through the public Freshdesk API v2 and ends every run with a reconcile report.
package freshdesk

import (
	"context"
	"errors"
	"log"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ticketRef struct {
	id        int64
	updatedAt time.Time
	archived  bool
}

type run struct {
	opts         Options
	db           *database.DB
	client       *client
	ledger       ledger
	workspaceID  string
	agents       map[int64]fdAgent
	agentTargets map[int64]string
	// deactivatedAgents are the Freshdesk agents matched to a deactivated Colliedesk agent.
	deactivatedAgents map[int64]bool
	companies         map[int64]string
	usedCompanies     map[int64]bool
	contactNames      map[int64]string
	contactIDs        []string
	refs              []ticketRef
	deleted           []string
	trashed           []string
	spam              []string
	absent            int
	unresolved        []string
	archived          int
	warnings          []*Warning
	internalErr       error

	listedContacts     int
	probeAhead         int
	probed             int
	highestExisting    int64
	highestProbed      int64
	unclassified       []string
	unchanged          []string
	archiveUnavailable atomic.Bool
	verification       Verifications
	progress           *progress
}

// Run imports (or, with DryRun, only counts) a Freshdesk account. Item-level
// problems are recorded in the ledger and reported; Run returns an error only
// when the run cannot continue (bad key, unreachable account, cancellation).
func Run(ctx context.Context, pool *pgxpool.Pool, options Options) (*Report, error) {
	r, err := newRun(ctx, database.New(pool), options)
	if err != nil {
		return nil, err
	}
	report := &Report{Domain: r.client.base.Host, Workspace: options.WorkspaceSlug, DryRun: options.DryRun,
		Since: options.Since, StartedAt: time.Now().UTC(), NotImported: notImported}
	if r.ledger.source, err = r.accountSource(ctx); err != nil {
		return nil, err
	}
	if !options.DryRun {
		release, err := acquireRunLock(ctx, r.db, r.workspaceID)
		if err != nil {
			return nil, err
		}
		defer release()
		if err := r.guardFoundingStorage(ctx); err != nil {
			return nil, err
		}
	}
	type step struct {
		name string
		run  func(context.Context) error
	}
	steps := []step{{"loading agents", r.loadAgents}, {"loading companies", r.loadCompanies}, {"importing contacts", r.importContacts},
		{"listing tickets", r.listTickets}, {"checking ticket ids", r.classifyTickets}, {"verifying totals", r.verifyTotals}}
	if !options.DryRun {
		steps = append(steps, step{"importing tickets", r.importTickets})
	}
	for _, step := range steps {
		r.progress.phase(step.name)
		if err := step.run(ctx); err != nil {
			return nil, err
		}
	}
	if r.internalErr != nil {
		return nil, r.internalErr
	}
	report, err = r.reconcile(ctx, report)
	if err != nil {
		return report, err
	}
	if options.DryRun {
		return report, r.checkDryRunStorage(ctx, report)
	}
	// The import is already committed; a failed extension must not hide its report. A rerun retries.
	extendedTo, err := r.extendTrial(ctx)
	switch {
	case err != nil:
		log.Printf("freshdesk import: trial extension failed for workspace %s: %v", options.WorkspaceSlug, err)
		if options.Cloud {
			report.TrialExtensionError = err.Error()
		}
	case options.Cloud:
		report.TrialExtendedTo = extendedTo
	}
	return report, nil
}

func newRun(ctx context.Context, db *database.DB, options Options) (*run, error) {
	base, err := url.Parse(strings.TrimRight(options.BaseURL, "/"))
	if err != nil || base.Host == "" {
		return nil, errors.New("invalid Freshdesk base URL")
	}
	if base.Scheme != "https" {
		return nil, errors.New("Freshdesk base URL must use https")
	}
	if options.APIKey == "" {
		return nil, errors.New("Freshdesk API key is empty")
	}
	if options.Storage == nil && !options.DryRun {
		return nil, errors.New("attachment storage is required")
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.MaxTicketPages <= 0 {
		options.MaxTicketPages = 300
	}
	if options.RateLimitPause <= 0 {
		options.RateLimitPause = time.Minute
	}
	workspaceID, err := db.WorkspaceIDBySlug(ctx, options.WorkspaceSlug)
	if err != nil {
		return nil, err
	}
	progress := newProgress(options.Progress, options.ProgressInterval)
	httpClient := newClient(base, options.APIKey, options)
	httpClient.progress = progress
	return &run{
		opts: options, db: db, workspaceID: workspaceID, progress: progress,
		client: httpClient,
		ledger: ledger{db: db, workspaceID: workspaceID}, probeAhead: probeAhead(options.hooks),
		agents: map[int64]fdAgent{}, agentTargets: map[int64]string{}, deactivatedAgents: map[int64]bool{}, companies: map[int64]string{},
		usedCompanies: map[int64]bool{}, contactNames: map[int64]string{},
	}, nil
}

// begin starts a transaction scoped to the import workspace; the importer never reads or writes
// another workspace.
func (r *run) begin(ctx context.Context) (pgx.Tx, error) {
	return r.db.BeginWorkspace(ctx, r.workspaceID)
}

func probeAhead(hooks testHooks) int {
	if hooks.probeAhead > 0 {
		return hooks.probeAhead
	}
	return defaultProbeAhead
}

func (r *run) endpoint(path string, query url.Values) string {
	return r.client.base.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()}).String()
}

func pageQuery() url.Values {
	return url.Values{"per_page": {"100"}}
}

// warn adds a Freshdesk id to a warning, creating the warning on first use.
func (r *run) warn(code, message, id string) {
	var ids []string
	if id != "" {
		ids = []string{id}
	}
	for _, warning := range r.warnings {
		if warning.Code == code {
			warning.FreshdeskIDs = append(warning.FreshdeskIDs, ids...)
			return
		}
	}
	r.warnings = append(r.warnings, &Warning{Code: code, Message: message, FreshdeskIDs: ids})
}

func (r *run) reportWarnings() []Warning {
	out := make([]Warning, 0, len(r.warnings))
	for _, warning := range r.warnings {
		ids := uniqueSorted(warning.FreshdeskIDs)
		out = append(out, Warning{Code: warning.Code, Message: warning.Message, FreshdeskIDs: ids})
	}
	return out
}

func uniqueSorted(ids []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return numericLess(out[i], out[j]) })
	return out
}

func numericLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// ErrImportRunning means another import or undo holds the workspace lock.
var ErrImportRunning = errors.New("an import or undo is already running in this workspace")

// ErrNoImport means the workspace holds no import from the requested source.
var ErrNoImport = errors.New("no Freshdesk import found in this workspace")
