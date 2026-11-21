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
	failing map[string]failing
	// generation counts single-workspace reloads; changed holds the generation of each one that a
	// full reload in progress may not have seen.
	generation uint64
	changed    map[string]uint64
	keyDay     string
	key        []byte
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
		queue: make(chan *delivery, options.QueueSize), targets: map[string]Target{}, results: map[string]Result{},
		failing: map[string]failing{}, changed: map[string]uint64{}}
}

// PageViewed implements pageview.Consumer: it queues the view for the workspace's target, if any
// and not paused.
func (s *Sender) PageViewed(event pageview.Event) {
	s.mu.Lock()
	target, ok := s.targets[event.WorkspaceID]
	ok = ok && !s.pausedLocked(event.WorkspaceID)
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

// readerIDs returns the GA4 client id, an HMAC of the workspace, the reader's address and browser
// under the key of the day, and a session id from the same HMAC. The key lives only in memory, so
// the ids cannot be traced back to a reader, change every day and differ between workspaces.
// s.mu must be held.
func (s *Sender) readerIDs(event pageview.Event) (string, string) {
	if day := s.options.Now().UTC().Format(time.DateOnly); day != s.keyDay {
		s.keyDay, s.key = day, s.options.NewKey()
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(event.WorkspaceID + "\n" + event.Client + "\n" + event.UserAgent))
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

// enqueueRetry queues a retry only when there is room: a newer page view is worth more than a
// retry of an older one.
func (s *Sender) enqueueRetry(item *delivery) {
	select {
	case s.queue <- item:
	default:
		s.dropped.Add(1)
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
					if s.stillCurrent(item) {
						s.send(ctx, item)
					}
				}
			}
		})
	}
	ticker := time.NewTicker(s.options.FlushEvery)
	defer ticker.Stop()
	var dropped int64
	for {
		select {
		case <-ctx.Done():
			workers.Wait()
			return
		case <-ticker.C:
			if err := s.Flush(ctx); err != nil {
				slog.Warn("help center analytics: recording send results failed; the next flush tries again", "error", err)
			}
			if now := s.Dropped(); now != dropped {
				slog.Warn("help center analytics: the queue was full and dropped page views", "dropped", now-dropped)
				dropped = now
			}
			if err := s.Reload(ctx, ""); err != nil {
				slog.Warn("help center analytics: reloading targets failed", "error", err)
			}
		}
	}
}

// send delivers one item; a failure is queued again after the next retry delay while its target is
// still current, and the last failure is recorded.
func (s *Sender) send(ctx context.Context, item *delivery) {
	failure := s.deliver(ctx, item)
	if ctx.Err() != nil {
		return
	}
	retry := failure != "" && item.tries < len(s.options.RetryDelays)
	s.sent(item, failure, !retry)
	if !retry {
		return
	}
	delay := s.options.RetryDelays[item.tries]
	item.tries++
	time.AfterFunc(delay, func() {
		if ctx.Err() == nil && s.stillCurrent(item) {
			s.enqueueRetry(item)
		}
	})
}

// Flush writes the results gathered since the last flush. Results a failed write could not record
// wait for the next flush, unless newer ones replaced them.
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
	err := s.store.RecordHelpCenterAnalyticsResults(ctx, results)
	if err != nil {
		s.mu.Lock()
		for _, result := range results {
			s.results[result.WorkspaceID] = mergeOlder(s.results[result.WorkspaceID], result)
		}
		s.mu.Unlock()
	}
	return err
}

// mergeOlder fills what newer lacks from older, a result gathered before it.
func mergeOlder(newer, older Result) Result {
	if newer.WorkspaceID == "" {
		return older
	}
	if newer.Version != older.Version {
		return newer
	}
	if newer.SentAt.IsZero() {
		newer.SentAt = older.SentAt
	}
	if newer.ErrorAt.IsZero() {
		newer.Error, newer.ErrorAt = older.Error, older.ErrorAt
	}
	return newer
}

func randomKey() []byte {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	return key
}
