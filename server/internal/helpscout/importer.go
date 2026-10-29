// Package helpscout imports one Help Scout inbox into a Colliedesk workspace through the public
// Help Scout Inbox API 2.0 and ends every run with a reconcile report. It follows the Freshdesk
// importer: the same ledger table, idempotency, workspace lock, reconcile statuses and undo.
package helpscout

import (
	"context"
	"errors"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/importkit"
	"github.com/colliedesk/colliedesk/server/internal/results"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type run struct {
	opts         Options
	db           *database.DB
	client       *client
	ledger       ledger
	workspaceID  string
	mailbox      hsMailbox
	users        map[int64]hsUser
	agentTargets map[int64]string
	deactivated  map[int64]bool
	refs         []hsConversation
	spam         []string
	deleted      []string
	drafts       []string
	listed       int
	verification Verifications
	warnings     []*Warning
	internalErr  error
	progress     *progress
}

// Run imports (or, with DryRun, only counts) one Help Scout inbox. Item-level problems are recorded
// in the ledger and reported; Run returns an error only when the run cannot continue (bad
// credentials, unreachable API, no inbox chosen, storage cap, cancellation).
func Run(ctx context.Context, pool *pgxpool.Pool, options Options) (*Report, error) {
	r, err := newRun(ctx, database.New(pool), options)
	if err != nil {
		return nil, err
	}
	report := &Report{Workspace: options.WorkspaceSlug, DryRun: options.DryRun, Since: options.Since, StartedAt: time.Now().UTC(), NotImported: notImported}
	r.progress.phase("choosing the inbox")
	if err := r.selectMailbox(ctx); err != nil {
		return nil, err
	}
	report.Source, report.Mailbox = r.ledger.source, Mailbox{ID: r.mailbox.ID, Name: r.mailbox.Name}
	if !options.DryRun {
		release, err := importkit.AcquireRunLock(ctx, r.db, r.workspaceID)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	type step struct {
		name string
		run  func(context.Context) error
	}
	steps := []step{{"loading users", r.loadUsers}, {"listing conversations", r.listConversations}}
	if !options.DryRun {
		steps = append(steps, step{"checking the storage cap", r.guardStorage}, step{"importing conversations", r.importConversations})
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
	if err := r.reconcile(ctx, report); err != nil {
		return nil, err
	}
	if options.DryRun {
		return report, r.checkDryRunStorage(ctx, report)
	}
	// The results page measures its baseline from the first import to finish. Help Scout has no
	// resolve times to read, so that baseline has first response, volume and handle time only.
	if err := r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) error {
		return results.MarkImportFinished(ctx, tx, r.workspaceID, r.ledger.source, report.Status, "", r.now())
	}); err != nil {
		log.Printf("helpscout import: results baseline not marked for workspace %s: %v", options.WorkspaceSlug, err)
	}
	// The import is already committed; a failed extension must not hide its report. A rerun retries.
	extendedTo, err := importkit.ExtendTrial(ctx, r.db, r.workspaceID, r.now())
	switch {
	case err != nil:
		log.Printf("helpscout import: trial extension failed for workspace %s: %v", options.WorkspaceSlug, err)
		if options.Cloud {
			report.TrialExtensionError = err.Error()
		}
	case options.Cloud:
		report.TrialExtendedTo = extendedTo
	}
	return report, nil
}

func newRun(ctx context.Context, db *database.DB, options Options) (*run, error) {
	if options.BaseURL == "" {
		options.BaseURL = DefaultBaseURL
	}
	base, err := url.Parse(strings.TrimRight(options.BaseURL, "/"))
	if err != nil || base.Host == "" {
		return nil, errors.New("invalid Help Scout API URL")
	}
	if base.Scheme != "https" {
		return nil, errors.New("Help Scout API URL must use https")
	}
	if options.AppID == "" || options.AppSecret == "" {
		return nil, errors.New("Help Scout App ID and App Secret are required")
	}
	if options.Storage == nil && !options.DryRun {
		return nil, errors.New("attachment storage is required")
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	workspaceID, err := db.WorkspaceIDBySlug(ctx, options.WorkspaceSlug)
	if err != nil {
		return nil, err
	}
	r := &run{opts: options, db: db, workspaceID: workspaceID, progress: newProgress(options.Progress),
		ledger: ledger{db: db, workspaceID: workspaceID}, users: map[int64]hsUser{}, agentTargets: map[int64]string{}, deactivated: map[int64]bool{}}
	r.client = newClient(base, options, r.allowedImageURL)
	r.client.progress = r.progress
	return r, nil
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

func (r *run) now() time.Time {
	if r.opts.Now != nil {
		return r.opts.Now()
	}
	return time.Now()
}

// warn adds a Help Scout id to a warning, creating the warning on first use.
func (r *run) warn(code, message, id string) {
	for _, warning := range r.warnings {
		if warning.Code == code {
			if id != "" {
				warning.HelpScoutIDs = append(warning.HelpScoutIDs, id)
			}
			return
		}
	}
	warning := &Warning{Code: code, Message: message, HelpScoutIDs: []string{}}
	if id != "" {
		warning.HelpScoutIDs = append(warning.HelpScoutIDs, id)
	}
	r.warnings = append(r.warnings, warning)
}

// itemFailed records a per-item failure. Cancellation is not a failure of the item.
func (r *run) itemFailed(ctx context.Context, entity, sourceID, parentID string, cause error) {
	if ctx.Err() != nil {
		return
	}
	err := r.ledger.mark(ctx, nil, ledgerMark{entity: entity, sourceID: sourceID, parentID: parentID, status: ledgerFailed, errText: cause.Error()})
	if err != nil && r.internalErr == nil {
		r.internalErr = err
	}
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
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

// notImported lists Help Scout data this importer does not carry over, so the report never implies
// more than it delivers.
var notImported = []string{
	"Other inboxes than the one chosen (run the import once per inbox)",
	"Customers with no conversation in the chosen inbox, and customer phone numbers, addresses, social profiles and organizations",
	"Custom fields, folders, workflows, saved replies, ratings, snoozes and scheduled replies",
	"Line items (Help Scout's record of a status, assignee or inbox change) and draft threads that were never sent (listed by id)",
	"Conversations in Spam, deleted conversations and draft conversations (listed by id)",
	"Attachments Help Scout marked as a virus (listed by id)",
	"Images in thread HTML that are not hosted by Help Scout stay links; the original HTML is kept",
	"Team assignees (a conversation assigned to a Help Scout team is imported unassigned)",
	"A re-run takes subject, status and assignee from Help Scout again and only adds tags; tags removed in Help Scout stay",
}
