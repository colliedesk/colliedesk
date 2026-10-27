package autofixworker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/autofix"
	"github.com/colliedesk/colliedesk/server/internal/cursor"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// Cursor is everything Autofix does with Cursor's API besides the worker's loop: the settings check and
// the vendor side of a person's cancel. The worker and the API share one per process, so they share its
// call budget.
type Cursor struct {
	Store *store.Store
	// Base is Cursor's API (cursor.DefaultBase when empty; cursortest in tests).
	Base       string
	HTTPClient *http.Client
	Now        func() time.Time
	budget     budget
	// listed is when each workspace last listed its repositories at Cursor, which allows that once a
	// minute.
	listMu sync.Mutex
	listed map[string]time.Time
}

// ForgetListing drops the workspace's last listing: a new or removed key makes it stale.
func (c *Cursor) ForgetListing(workspaceID string) {
	c.listMu.Lock()
	defer c.listMu.Unlock()
	delete(c.listed, workspaceID)
}

// mayList reports whether the workspace may list its repositories at Cursor now, and if so counts it.
func (c *Cursor) mayList(workspaceID string) bool {
	c.listMu.Lock()
	defer c.listMu.Unlock()
	now := c.now()
	if at, ok := c.listed[workspaceID]; ok && now.Sub(at) < time.Minute {
		return false
	}
	if c.listed == nil {
		c.listed = map[string]time.Time{}
	}
	c.listed[workspaceID] = now
	return true
}

var cursorActor = autofix.Actor{Kind: autofix.ActorCursor, Name: "Cursor"}

func (c *Cursor) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Cursor) client(key string) cursor.Client {
	return cursor.Client{Base: c.Base, Key: key, HTTPClient: c.HTTPClient}
}

// budget spreads each workspace's calls: Cursor allows a key 20 requests a minute by default, and
// Colliedesk keeps to half of that so a person's check or cancel always has room. It is per process.
type budget struct {
	mu    sync.Mutex
	calls map[string][]time.Time
}

const (
	callsPerMinute = 10
	budgetWindow   = time.Minute
)

// take spends one call of the workspace, or says how long until one is free.
func (b *budget) take(workspaceID string, now time.Time) (bool, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls == nil {
		b.calls = map[string][]time.Time{}
	}
	recent := slices.DeleteFunc(b.calls[workspaceID], func(at time.Time) bool { return !at.After(now.Add(-budgetWindow)) })
	if len(recent) >= callsPerMinute {
		b.calls[workspaceID] = recent
		return false, recent[0].Add(budgetWindow).Sub(now)
	}
	b.calls[workspaceID] = append(recent, now)
	return true, 0
}

// takeNow spends a call for a person waiting on it (check, cancel): it goes through even over the
// worker's budget, and counts against it.
func (b *budget) takeNow(workspaceID string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.calls == nil {
		b.calls = map[string][]time.Time{}
	}
	b.calls[workspaceID] = append(b.calls[workspaceID], now)
}

// repoURL is a connected repository's address as Cursor takes it.
func repoURL(fullName string) string { return "https://github.com/" + fullName }

// sameRepo compares a repository address Cursor gave with a connected repository's name: GitHub names
// are not case sensitive, and Cursor may leave out the scheme or add .git.
func sameRepo(address, fullName string) bool {
	address = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(address), "/"), ".git")
	address = strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	address = strings.TrimPrefix(address, "www.")
	return strings.EqualFold(address, "github.com/"+fullName)
}

// Check checks the workspace's Cursor key: Cursor knows it, and it reaches the connected repositories
// when Cursor can say (a service account key lists none). The result is kept on the connector.
func (c *Cursor) Check(ctx context.Context, workspaceID string) (autofix.ConnectorSetting, error) {
	key, err := c.Store.OpenCursorKey(ctx, workspaceID)
	if err != nil {
		return autofix.ConnectorSetting{}, err
	}
	before, err := c.Store.GetFixConnector(ctx, workspaceID, autofix.CursorAPI)
	if err != nil {
		return autofix.ConnectorSetting{}, err
	}
	client := c.client(key)
	c.budget.takeNow(workspaceID, c.now())
	if err := client.Me(ctx); err != nil {
		if cursor.StatusOf(err) == http.StatusUnauthorized || cursor.StatusOf(err) == http.StatusForbidden {
			return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckKeyInvalid,
				"Cursor does not accept this key. Create a new one (a service account key is advised) and set it again.")
		}
		return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckProviderUnavailable,
			"Cursor did not answer. Nothing changed; check again in a minute.")
	}
	repos, err := c.Store.ConnectedRepoNames(ctx, workspaceID)
	if err != nil {
		return autofix.ConnectorSetting{}, err
	}
	if len(repos) == 0 {
		return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckOK, "Cursor accepts the key. Connect a repository to send fixes to it.")
	}
	// Cursor allows listing repositories once a minute: within it, the last listing's result stands.
	if !c.mayList(workspaceID) {
		return before, nil
	}
	c.budget.takeNow(workspaceID, c.now())
	reachable, err := client.Repositories(ctx)
	switch {
	case cursor.StatusOf(err) == http.StatusTooManyRequests:
		return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckOK,
			"Cursor accepts the key. Its repositories were not listed: Cursor allows that once a minute.")
	case err != nil:
		return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckProviderUnavailable,
			"Cursor accepts the key but did not list its repositories. Check again in a minute.")
	case len(reachable) == 0:
		return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckOK,
			"Cursor accepts the key. It lists no repositories (a service account key never does), so the first fix shows whether Cursor reaches yours.")
	}
	var missing []string
	for _, name := range repos {
		if !slices.ContainsFunc(reachable, func(address string) bool { return sameRepo(address, name) }) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckNoRepoAccess,
			"Cursor does not reach "+strings.Join(missing, ", ")+". Give Cursor's GitHub integration access to it, then check again.")
	}
	return c.Store.RecordConnectorCheck(ctx, workspaceID, autofix.CursorAPI, autofix.CheckOK, "Cursor accepts the key and reaches every connected repository.")
}

// ErrStopOnCursor means a cancelled fix's Cursor run could not be stopped (no key, Cursor did not
// answer): the person stops it on Cursor (vendor_url).
var ErrStopOnCursor = errors.New("the Cursor run could not be stopped from Colliedesk; stop it on Cursor")

// Cancel is the vendor side of a person's cancel, after store.ActOnFix committed: it cancels the fix's
// Cursor run, if it has one. A run already ended is fine. A fix not sent yet has nothing to stop: the
// worker sees it cancelled and stops whatever it created.
func (c *Cursor) Cancel(ctx context.Context, workspaceID, fixID string) error {
	work, err := c.Store.GetFixWork(ctx, workspaceID, fixID)
	if err != nil {
		return err
	}
	if work.Connector != autofix.CursorAPI || work.VendorRef == "" || work.VendorRunID == "" {
		return nil
	}
	if err := c.Store.StopFixChecks(ctx, workspaceID, fixID); err != nil {
		return err
	}
	key, err := c.Store.OpenCursorKey(ctx, workspaceID)
	if errors.Is(err, autofix.ErrCursorKeyMissing) {
		return ErrStopOnCursor
	}
	if err != nil {
		return err
	}
	c.budget.takeNow(workspaceID, c.now())
	err = c.client(key).CancelRun(ctx, work.VendorRef, work.VendorRunID)
	var cursorErr *cursor.Error
	if errors.As(err, &cursorErr) && cursorErr.Status == http.StatusConflict {
		// run_not_cancellable: it already ended.
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStopOnCursor, err)
	}
	return nil
}

// Stop is Cancel for a fix that already ended in Colliedesk (a person's cancel, a repository
// disconnected): when the run cannot be stopped, the fix's timeline says so with where to stop it
// (cursor_cancel_failed). It returns Cancel's error, for the log.
func (c *Cursor) Stop(ctx context.Context, workspaceID, fixID string) error {
	err := c.Cancel(ctx, workspaceID, fixID)
	if err == nil || errors.Is(err, autofix.ErrFixNotFound) {
		return err
	}
	data := map[string]any{"summary": "Cursor did not stop the run: stop it there."}
	if work, readErr := c.Store.GetFixWork(ctx, workspaceID, fixID); readErr == nil && work.VendorURL != nil {
		data["vendor_url"] = *work.VendorURL
	}
	if noteErr := c.Store.NoteFixEvent(ctx, workspaceID, fixID, "cursor_cancel_failed", data, autofix.Actor{Kind: autofix.ActorSystem, Name: "Colliedesk"}); noteErr != nil {
		slog.Error("autofix: note a Cursor run left going", "error", noteErr, "workspace", workspaceID, "fix", fixID)
	}
	return err
}
