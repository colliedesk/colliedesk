package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Access says whether the API key can read one part of the account.
type Access string

const (
	AccessReadable Access = "readable"
	// AccessNone: Freshdesk answered 404, the account has none (no knowledge base, for example).
	AccessNone Access = "none"
	// AccessRefused: Freshdesk answered 403 to this key.
	AccessRefused Access = "refused"
	// AccessUnknown: another error; the import will meet it again and report it.
	AccessUnknown Access = "unknown"
)

// ErrNeedsAdminKey means Freshdesk answered 403 where the import needs an admin's API key. Freshdesk
// also answers 403 when the account lacks a feature, so the plan is named too.
var ErrNeedsAdminKey = errors.New("Freshdesk refused this API key: it must belong to an admin, on a plan that includes the API")

// PreviewAgent is a Freshdesk agent, for the team to invite before the import.
type PreviewAgent struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Active bool   `json:"active"`
}

// PreviewResult is what a few cheap reads tell about an account before anything is written.
// Freshdesk has no count endpoint for tickets: TicketsOutsideArchive is the search total, which leaves
// archived tickets out, and HighestListedTicketID is the newest listed ticket; archived tickets can
// have higher ids. Neither is an upper bound.
type PreviewResult struct {
	Domain    string         `json:"domain"`
	AccountID int64          `json:"account_id"`
	Source    string         `json:"source"`
	Agents    []PreviewAgent `json:"agents"`
	Companies int            `json:"companies"`
	// Contacts is the search total (active plus inactive); nil when search is unavailable.
	Contacts              *int  `json:"contacts"`
	TicketsOutsideArchive *int  `json:"tickets_outside_archive"`
	HighestListedTicketID int64 `json:"highest_listed_ticket_id"`
	// Archive refused means the import report can be UNVERIFIED at best.
	Archive         Access `json:"archive"`
	CannedResponses Access `json:"canned_responses"`
	KnowledgeBase   Access `json:"knowledge_base"`
	// RateLimitPerMinute is the X-Ratelimit-Total Freshdesk sent; 0 when it sent none.
	RateLimitPerMinute int       `json:"rate_limit_per_minute"`
	APICalls           int       `json:"api_calls"`
	CheckedAt          time.Time `json:"checked_at"`
}

// Preview reads the account with a handful of requests (a few dozen on an account with many agents
// or companies): it confirms the key and names what it can read. It needs no database and writes
// nothing; WorkspaceSlug, Storage and Mode are ignored. A wrong key answers ErrKeyRejected, a key
// without admin rights ErrNeedsAdminKey.
func Preview(ctx context.Context, options Options) (*PreviewResult, error) {
	base, err := accountBase(options)
	if err != nil {
		return nil, err
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.RateLimitPause <= 0 {
		options.RateLimitPause = time.Minute
	}
	counter := newProgress(nil, 0, func(ProgressEvent) {})
	r := &run{opts: options, client: newClient(base, options.APIKey, options), progress: counter}
	r.client.progress = counter
	result := &PreviewResult{Domain: base.Host, Agents: []PreviewAgent{}}
	if result.Source, err = r.accountSource(ctx); err != nil {
		return nil, adminError(err)
	}
	result.AccountID, _ = strconv.ParseInt(result.Source[len("freshdesk:account:"):], 10, 64)
	if err := listAll(ctx, r.client, r.endpoint("/api/v2/agents", pageQuery()), func(agent fdAgent) error {
		result.Agents = append(result.Agents, PreviewAgent{ID: agent.ID, Name: agent.Contact.Name, Email: agent.Contact.Email, Active: agent.Contact.Active})
		return nil
	}); err != nil {
		return nil, adminError(fmt.Errorf("list agents: %w", err))
	}
	if err := listAll(ctx, r.client, r.endpoint("/api/v2/companies", pageQuery()), func(fdCompany) error {
		result.Companies++
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	active, errActive := r.searchTotal(ctx, "/api/v2/search/contacts", `"active:true"`)
	inactive, errInactive := r.searchTotal(ctx, "/api/v2/search/contacts", `"active:false"`)
	if errActive == nil && errInactive == nil {
		total := active + inactive
		result.Contacts = &total
	}
	if total, err := r.searchTotal(ctx, "/api/v2/search/tickets", allTicketsQuery); err == nil {
		result.TicketsOutsideArchive = &total
	}
	// Without updated_since the list returns only tickets created in the last 30 days.
	query := url.Values{"per_page": {"1"}, "updated_since": {listEpoch.Format(time.RFC3339)}, "order_by": {"created_at"}, "order_type": {"desc"}}
	var newest []fdTicket
	if _, err := r.client.getJSON(ctx, r.endpoint("/api/v2/tickets", query), &newest); err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	probe := int64(1)
	if len(newest) > 0 {
		result.HighestListedTicketID, probe = newest[0].ID, newest[0].ID
	}
	var ignored any
	result.Archive = r.access(ctx, r.endpoint("/api/v2/tickets/archived/"+strconv.FormatInt(probe, 10), nil), &ignored, true)
	result.CannedResponses = r.access(ctx, r.endpoint("/api/v2/canned_response_folders", pageQuery()), &[]fdCannedFolder{}, false)
	result.KnowledgeBase = r.access(ctx, r.solutionsPath("categories"), &[]fdSolutionCategory{}, false)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.RateLimitPerMinute = r.client.rateLimitPerMinute()
	result.APICalls = int(counter.calls.Load())
	result.CheckedAt = time.Now().UTC()
	return result, nil
}

// access reads one page of target. On the archive a 404 means the id is not archived, which proves
// the endpoint answers.
func (r *run) access(ctx context.Context, target string, out any, notFoundReadable bool) Access {
	_, err := r.client.getJSON(ctx, target, out)
	switch {
	case err == nil:
		return AccessReadable
	case errors.Is(err, errNotFound) && notFoundReadable:
		return AccessReadable
	case errors.Is(err, errNotFound):
		return AccessNone
	case errors.Is(err, errForbidden):
		return AccessRefused
	default:
		return AccessUnknown
	}
}

func adminError(err error) error {
	if errors.Is(err, errForbidden) {
		return fmt.Errorf("%w: %w", ErrNeedsAdminKey, err)
	}
	return err
}
