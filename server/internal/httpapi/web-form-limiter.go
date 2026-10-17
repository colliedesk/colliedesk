package httpapi

import (
	"container/list"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	// webFormMaxPerWorkspace is how many submissions one client address may send to one workspace's form per window.
	webFormMaxPerWorkspace = 5
	// webFormMaxPerAddress is how many requests one client address may send to any public form per
	// window, junk slugs and preflights included. It bounds what one address can ask the database.
	webFormMaxPerAddress = 30
	webFormWindow        = time.Minute
	// webFormMaxKeys bounds each limiter's memory. A full limiter evicts its oldest key to admit a new
	// one, so a flood of keys can never make the limiter refuse everyone.
	webFormMaxKeys = 20000
)

// webFormLimiter allows limit attempts per key in a sliding webFormWindow. It lives in this process
// only: a restart clears it, and several server processes each keep their own count. The
// per-workspace ceiling on new conversations in the store bounds what rotating addresses can do.
// Keys are kept in a least-recently-used list, so admitting a new key at capacity is O(1): the
// limiter's lock is shared by every public form request, and a scan would let one client with many
// addresses slow everyone down.
type webFormLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	order   *list.List // front is the most recently used key
	index   map[string]*list.Element
}

type limiterEntry struct {
	key  string
	hits []time.Time
}

func newWebFormLimiter(limit, maxKeys int) *webFormLimiter {
	return newWindowLimiter(limit, maxKeys, webFormWindow)
}

// newWindowLimiter allows limit attempts per key in a sliding window of the given length.
func newWindowLimiter(limit, maxKeys int, window time.Duration) *webFormLimiter {
	return &webFormLimiter{limit: limit, window: window, maxKeys: maxKeys, order: list.New(), index: map[string]*list.Element{}}
}

// allow records one attempt for key at now. When refused it reports how long until a slot frees.
func (l *webFormLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	element, present := l.index[key]
	if !present {
		if len(l.index) >= l.maxKeys {
			oldest := l.order.Back()
			delete(l.index, oldest.Value.(*limiterEntry).key)
			l.order.Remove(oldest)
		}
		element = l.order.PushFront(&limiterEntry{key: key})
		l.index[key] = element
	} else {
		l.order.MoveToFront(element)
	}
	entry := element.Value.(*limiterEntry)
	recent := entry.hits[:0]
	for _, hit := range entry.hits {
		if now.Sub(hit) < l.window {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= l.limit {
		entry.hits = recent
		return false, l.window - now.Sub(recent[0])
	}
	entry.hits = append(recent, now)
	return true, 0
}

// clientAddress is the address a request counts under. It is the TCP peer unless the peer is a
// trusted proxy, in which case it is the rightmost X-Forwarded-For entry that is not itself a
// trusted proxy: entries to its left are written by the client and are never believed. IPv6
// addresses count by their /64, the smallest block one subscriber usually holds.
func clientAddress(request *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	client := peer.Unmap()
	if isTrusted(client, trusted) {
		entries := strings.Split(strings.Join(request.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(entries) - 1; i >= 0; i-- {
			candidate, ok := forwardedAddress(entries[i])
			if !ok {
				break
			}
			client = candidate.Unmap()
			if !isTrusted(client, trusted) {
				break
			}
		}
	}
	if client.Is6() {
		if block, err := client.Prefix(64); err == nil {
			return block.String()
		}
	}
	return client.String()
}

// forwardedAddress reads one X-Forwarded-For entry: a bare address, or an address with a port
// ("198.51.100.7:4711", "[2001:db8::1]:443") as some load balancers send it.
func forwardedAddress(entry string) (netip.Addr, bool) {
	entry = strings.TrimSpace(entry)
	if withPort, err := netip.ParseAddrPort(entry); err == nil {
		return withPort.Addr(), true
	}
	address, err := netip.ParseAddr(strings.Trim(entry, "[]"))
	return address, err == nil
}
