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
	webFormMaxPerWindow = 5
	webFormWindow       = time.Minute
	// webFormMaxKeys bounds the limiter's memory. When it is full of live keys the limiter refuses
	// new ones, so a flood of addresses cannot grow the server without bound.
	webFormMaxKeys = 20000
)

// webFormLimiter allows webFormMaxPerWindow submissions per client address and workspace in a
// sliding webFormWindow. It lives in this process only: a restart clears it, and several server
// processes each keep their own count. The per-workspace ceiling on new conversations in the
// store bounds what rotating addresses can do.
type webFormLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newWebFormLimiter() *webFormLimiter {
	return &webFormLimiter{hits: map[string][]time.Time{}}
}

// allow records one attempt for key at now. When refused it reports how long until a slot frees.
func (l *webFormLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.hits[key][:0:0]
	for _, hit := range l.hits[key] {
		if now.Sub(hit) < webFormWindow {
			recent = append(recent, hit)
		}
	}
	if len(recent) >= webFormMaxPerWindow {
		l.hits[key] = recent
		return false, webFormWindow - now.Sub(recent[0])
	}
	if _, known := l.hits[key]; !known && len(l.hits) >= webFormMaxKeys {
		l.sweep(now)
		if len(l.hits) >= webFormMaxKeys {
			return false, webFormWindow
		}
	}
	l.hits[key] = append(recent, now)
	return true, 0
}

func (l *webFormLimiter) sweep(now time.Time) {
	for key, hits := range l.hits {
		if len(hits) == 0 || now.Sub(hits[len(hits)-1]) >= webFormWindow {
			delete(l.hits, key)
		}
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
			candidate, err := netip.ParseAddr(strings.TrimSpace(entries[i]))
			if err != nil {
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

func isTrusted(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
