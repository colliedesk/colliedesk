package httpapi

import (
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
type webFormLimiter struct {
	mu      sync.Mutex
	limit   int
	maxKeys int
	hits    map[string][]time.Time
}

func newWebFormLimiter(limit, maxKeys int) *webFormLimiter {
	return &webFormLimiter{limit: limit, maxKeys: maxKeys, hits: map[string][]time.Time{}}
}

// allow records one attempt for key at now. When refused it reports how long until a slot frees.
func (l *webFormLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	known := l.hits[key]
	recent := make([]time.Time, 0, len(known)+1)
	for _, hit := range known {
		if now.Sub(hit) < webFormWindow {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= l.limit {
		l.hits[key] = recent
		return false, webFormWindow - now.Sub(recent[0])
	}
	if _, present := l.hits[key]; !present && len(l.hits) >= l.maxKeys {
		l.makeRoom(now)
	}
	l.hits[key] = append(recent, now)
	return true, 0
}

// makeRoom drops keys whose attempts have all left the window; when every key is live it drops the
// one that was used longest ago.
func (l *webFormLimiter) makeRoom(now time.Time) {
	var oldest string
	var oldestSeen time.Time
	for key, hits := range l.hits {
		last := time.Time{}
		if len(hits) > 0 {
			last = hits[len(hits)-1]
		}
		if now.Sub(last) >= webFormWindow {
			delete(l.hits, key)
			continue
		}
		if oldest == "" || last.Before(oldestSeen) {
			oldest, oldestSeen = key, last
		}
	}
	if len(l.hits) >= l.maxKeys && oldest != "" {
		delete(l.hits, oldest)
	}
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

func isTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
