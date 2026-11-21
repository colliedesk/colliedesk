package hcanalytics

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

// Sender is a pageview.Consumer that queues each page view of a workspace with analytics and
// sends it from a small pool, so a page never waits on Google or Plausible.
type Sender struct {
	store   Store
	options Options
	client  *http.Client
	queue   chan *delivery
	dropped atomic.Int64

	mu      sync.Mutex
	targets map[string]Target
	results map[string]Result
	keyDay  string
	key     []byte
}

// delivery is one page view on its way to one target.
type delivery struct {
	target   Target
	event    pageview.Event
	clientID string
	session  string
	tries    int
}

// New returns a Sender; Run starts its senders and Reload loads the targets.
func New(store Store, options Options) *Sender {
	options = options.withDefaults()
	return &Sender{store: store, options: options, client: guardedClient(options.AllowDial, options.Timeout),
		queue: make(chan *delivery, options.QueueSize), targets: map[string]Target{}, results: map[string]Result{}}
}

// PageViewed implements pageview.Consumer: it queues the view for the workspace's target, if any.
func (s *Sender) PageViewed(event pageview.Event) {
	s.mu.Lock()
	target, ok := s.targets[event.WorkspaceID]
	var clientID, session string
	if ok && target.Provider == GA4 {
		clientID, session = s.readerIDs(event)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	if !strings.HasPrefix(event.Address, "https://") && !strings.HasPrefix(event.Address, "http://") {
		event.Address = strings.TrimSuffix(s.options.Origin, "/") + event.Address
	}
	s.enqueue(&delivery{target: target, event: event, clientID: clientID, session: session})
}

// readerIDs returns the GA4 client id, an HMAC of the reader's address and browser under the key
// of the day, and a session id from the same HMAC. The key lives only in memory, so the ids cannot
// be traced back to a reader and change every day. s.mu must be held.
func (s *Sender) readerIDs(event pageview.Event) (string, string) {
	if day := s.options.Now().UTC().Format(time.DateOnly); day != s.keyDay {
		s.keyDay, s.key = day, s.options.NewKey()
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(event.Client + "\n" + event.UserAgent))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum[:16]), strconv.FormatUint(uint64(binary.BigEndian.Uint32(sum[:4])), 10)
}

// enqueue adds a delivery without waiting: a full queue drops its oldest delivery first.
func (s *Sender) enqueue(item *delivery) {
	for {
		select {
		case s.queue <- item:
			return
		default:
		}
		select {
		case <-s.queue:
			s.dropped.Add(1)
		default:
		}
	}
}

// Dropped is how many deliveries a full queue has dropped since the server started.
func (s *Sender) Dropped() int64 { return s.dropped.Load() }

// Run loads the targets, then sends until ctx ends, and every FlushEvery records the results and
// reloads the targets.
func (s *Sender) Run(ctx context.Context) {
	if err := s.Reload(ctx, ""); err != nil {
		slog.Warn("help center analytics: loading targets failed; the next reload tries again", "error", err)
	}
	var workers sync.WaitGroup
	for range s.options.Senders {
		workers.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case item := <-s.queue:
					s.send(ctx, item)
				}
			}
		})
	}
	ticker := time.NewTicker(s.options.FlushEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return
		case <-ticker.C:
			if err := s.Flush(ctx); err != nil {
				slog.Warn("help center analytics: recording send results failed", "error", err)
			}
			if err := s.Reload(ctx, ""); err != nil {
				slog.Warn("help center analytics: reloading targets failed", "error", err)
			}
		}
	}
}

// send delivers one item; a failure is queued again after the next retry delay, and the last
// failure is recorded.
func (s *Sender) send(ctx context.Context, item *delivery) {
	failure := s.deliver(ctx, item)
	if ctx.Err() != nil {
		return
	}
	now := s.options.Now()
	if failure == "" {
		s.record(item.target.WorkspaceID, func(r *Result) { r.SentAt = now })
		return
	}
	if item.tries < len(s.options.RetryDelays) {
		delay := s.options.RetryDelays[item.tries]
		item.tries++
		time.AfterFunc(delay, func() {
			if ctx.Err() == nil {
				s.enqueue(item)
			}
		})
		return
	}
	s.record(item.target.WorkspaceID, func(r *Result) { r.Error, r.ErrorAt = failure, now })
}

func (s *Sender) record(workspaceID string, change func(*Result)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := s.results[workspaceID]
	result.WorkspaceID = workspaceID
	change(&result)
	s.results[workspaceID] = result
}

// Flush writes the results gathered since the last flush.
func (s *Sender) Flush(ctx context.Context) error {
	s.mu.Lock()
	if len(s.results) == 0 {
		s.mu.Unlock()
		return nil
	}
	results := make([]Result, 0, len(s.results))
	for _, result := range s.results {
		results = append(results, result)
	}
	s.results = map[string]Result{}
	s.mu.Unlock()
	return s.store.RecordHelpCenterAnalyticsResults(ctx, results)
}

// Reload reads the targets again: every workspace's for "", one workspace's otherwise (after a
// team changes its settings, so the change applies to the next page view).
func (s *Sender) Reload(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		targets, err := s.store.HelpCenterAnalyticsTargets(ctx)
		if err != nil {
			return err
		}
		next := make(map[string]Target, len(targets))
		for _, target := range targets {
			next[target.WorkspaceID] = target
		}
		s.mu.Lock()
		s.targets = next
		s.mu.Unlock()
		return nil
	}
	target, err := s.store.HelpCenterAnalyticsTarget(ctx, workspaceID)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if target == nil || !target.Enabled {
		delete(s.targets, workspaceID)
	} else {
		s.targets[workspaceID] = *target
	}
	return nil
}

func randomKey() []byte {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}
