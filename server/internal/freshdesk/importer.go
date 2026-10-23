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
	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ticketRef struct {
	id        int64
	updatedAt time.Time
	archived  bool
	// stats are the ticket stats the list returned; nil for an archived ticket.
	stats *fdStats
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
	pendingStats       []pendingStats
	archiveUnavailable atomic.Bool
	verification       Verifications
	progress           *progress

	cannedFolders      int
	cannedIDs          []string
	cannedEmpty        []string
	cannedPersonal     []string
	cannedListError    string
	cannedUnavailable  bool
	cannedWithFiles    []string
	cannedAttachments  int
	cannedUnreadable   []string
	cannedPlaceholders map[string][]string

	kb        *knowledgeBase
	kbState   *kbState
	kbRefused []string
}

// Run imports (or, with DryRun, only counts) a Freshdesk account. Item-level
// problems are recorded in the ledger and reported; Run returns an error only
// when the run cannot continue (bad key, unreachable account, cancellation).
func Run(ctx context.Context, pool *pgxpool.Pool, options Options) (*Report, error) {
	r, err := newRun(ctx, database.New(pool), options)
	if err != nil {
		return nil, err
	}
	report := &Report{Domain: r.client.base.Host, Workspace: options.WorkspaceSlug, DryRun: options.DryRun, Interim: options.Mode == ModeRecent,
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
	}
	if options.Mode == ModeRecent {
		if err := r.importRecent(ctx); err != nil {
			return nil, err
		}
		return r.finish(ctx, report)
	}
	// The knowledge base is listed in full before anything is written, so its limits and its images'
	// bytes are checked for the whole import first. --since applies to tickets only.
	r.progress.phase("listing knowledge base")
	if err := r.listSolutions(ctx); err != nil {
		return nil, err
	}
	if err := r.client.fatalErr(); err != nil {
		return nil, err
	}
	if err := r.kb.checkPortal(options.HelpCenterPortal); err != nil {
		return nil, err
	}
	if r.kbRefused, err = r.kbLimitProblems(ctx); err != nil {
		return nil, err
	}
	if !options.DryRun {
		if len(r.kbRefused) > 0 {
			return nil, &KnowledgeBaseLimitError{Problems: r.kbRefused}
		}
		if err := r.guardStorage(ctx); err != nil {
			return nil, err
		}
	}
	type step struct {
		name string
		run  func(context.Context) error
	}
	steps := []step{{"loading agents", r.loadAgents}, {"loading companies", r.loadCompanies}, {"importing contacts", r.importContacts}, {"importing saved replies", r.importSavedReplies},
		{"listing tickets", r.listTickets}, {"checking ticket ids", r.classifyTickets}, {"verifying totals", r.verifyTotals}}
	if !options.DryRun {
		steps = append(steps, step{"importing knowledge base", r.importKnowledgeBase}, step{"importing tickets", r.importTickets})
	}
	for _, step := range steps {
		r.progress.phase(step.name)
		if err := step.run(ctx); err != nil {
			return nil, err
		}
		// A step records item errors and goes on; a refused key or a plan without API calls ends the run.
		if err := r.client.fatalErr(); err != nil {
			return nil, err
		}
	}
	return r.finish(ctx, report)
}

// importRecent is the run of ModeRecent: the people tickets point at, the listed tickets newest
// first, and on a capped plan a size check of that slice before its first write.
func (r *run) importRecent(ctx context.Context) error {
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{{"loading agents", r.loadAgents}, {"loading companies", r.loadCompanies}, {"listing tickets", r.listTickets}} {
		r.progress.phase(step.name)
		if err := step.run(ctx); err != nil {
			return err
		}
		if err := r.client.fatalErr(); err != nil {
			return err
		}
	}
	// The list is read oldest-updated first (its 300-page window depends on it); import the other way.
	sort.SliceStable(r.refs, func(i, j int) bool { return r.refs[i].updatedAt.After(r.refs[j].updatedAt) })
	if err := r.guardSliceStorage(ctx); err != nil {
		return err
	}
	r.progress.phase("importing tickets")
	return r.importTickets(ctx)
}

// finish reconciles and, after a real run, extends a running trial.
func (r *run) finish(ctx context.Context, report *Report) (*Report, error) {
	options := r.opts
	if r.internalErr != nil {
		return nil, r.internalErr
	}
	report, err := r.reconcile(ctx, report)
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

// accountBase checks the account URL and the key of options.
func accountBase(options Options) (*url.URL, error) {
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
	return base, nil
}

func newRun(ctx context.Context, db *database.DB, options Options) (*run, error) {
	base, err := accountBase(options)
	if err != nil {
		return nil, err
	}
	if options.Mode == ModeRecent && (options.Since == nil || options.DryRun) {
		return nil, errors.New("a recent-tickets run needs a since date and is never a dry run")
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
	progress := newProgress(options.Progress, options.ProgressInterval, options.OnProgress)
	httpClient := newClient(base, options.APIKey, options)
	httpClient.progress = progress
	return &run{
		opts: options, db: db, workspaceID: workspaceID, progress: progress,
		client: httpClient,
		ledger: ledger{db: db, workspaceID: workspaceID}, probeAhead: probeAhead(options.hooks),
		agents: map[int64]fdAgent{}, agentTargets: map[int64]string{}, deactivatedAgents: map[int64]bool{}, companies: map[int64]string{},
		usedCompanies: map[int64]bool{}, contactNames: map[int64]string{}, cannedPlaceholders: map[string][]string{},
	}, nil
}

// begin starts a transaction scoped to the import workspace; the importer never reads or writes
// another workspace. Its status changes are recorded as the import's, not Colliedesk work.
func (r *run) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := r.db.BeginWorkspace(ctx, r.workspaceID)
	if err != nil {
		return nil, err
	}
	if err := store.MarkImportWrites(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
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

// ErrKeyRejected means Freshdesk answered 401 to the API key.
var ErrKeyRejected = errors.New("Freshdesk rejected the API key")

// ErrPlanNoAPI means Freshdesk reported an API limit of 0 calls: the Freshdesk Free plan has no API
// (support article 225439), and an account whose trial of a higher plan ended falls back to it.
// Waiting does not help; the account needs a plan with API calls.
var ErrPlanNoAPI = errors.New("this Freshdesk plan has no API calls (the Free plan has none); start a trial of a higher Freshdesk plan, then import")

// ErrUnavailable means Freshdesk kept answering 5xx or 429, or could not be reached, after the
// client's retries. It is the only run error worth trying again later as is.
var ErrUnavailable = errors.New("Freshdesk is unavailable")

// ErrImportRunning means another import or undo holds the workspace lock.
var ErrImportRunning = importkit.ErrImportRunning

// ErrNoImport means the workspace holds no import from the requested source.
var ErrNoImport = errors.New("no Freshdesk or Help Scout import found in this workspace")
