package helpcenter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"golang.org/x/sync/singleflight"
)

const (
	// hostCacheTTL is how long this process trusts what it remembers about a host name (its workspace,
	// or that it has none) and a workspace's active custom domain. A domain write in this process
	// forgets the names it changes at once; another process sees it within this time.
	hostCacheTTL = 30 * time.Second
	// hostCacheEntries bounds each cache, so random Host headers cannot grow one. Known and unknown
	// names have caches of their own: a flood of unknown names only evicts other unknown names.
	hostCacheEntries = 4096
)

// ttlCache remembers values for a while, up to max keys. When full it drops expired keys first, then
// arbitrary ones (map order), so it never grows past max. Every forget starts a new generation; a
// value read from the store before a forget is not kept after it (set takes the generation it started
// in), so a lookup that raced a domain write never stores what the write changed.
type ttlCache struct {
	mu         sync.Mutex
	generation uint64
	ttl        time.Duration
	max        int
	now        func() time.Time
	entries    map[string]cacheEntry
}

type cacheEntry struct {
	value   string
	expires time.Time
}

func newTTLCache(ttl time.Duration, max int, now func() time.Time) *ttlCache {
	return &ttlCache{ttl: ttl, max: max, now: now, entries: map[string]cacheEntry{}}
}

// get returns the value of key while it is fresh.
func (c *ttlCache) get(key string) (string, bool) {
	value, fresh, _ := c.peek(key)
	return value, fresh
}

// peek returns the value of key, whether it is still fresh, and whether the cache holds it at all
// (fresh or expired).
func (c *ttlCache) peek(key string) (value string, fresh, held bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, held := c.entries[key]
	return entry.value, held && c.now().Before(entry.expires), held
}

func (c *ttlCache) currentGeneration() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

func (c *ttlCache) set(key, value string, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	now := c.now()
	if _, ok := c.entries[key]; !ok && len(c.entries) >= c.max {
		for name, entry := range c.entries {
			if !now.Before(entry.expires) {
				delete(c.entries, name)
			}
		}
		for name := range c.entries {
			if len(c.entries) < c.max {
				break
			}
			delete(c.entries, name)
		}
	}
	c.entries[key] = cacheEntry{value: value, expires: now.Add(c.ttl)}
}

// drop removes key without starting a new generation.
func (c *ttlCache) drop(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// forget removes keys and starts a new generation, so no lookup that began before stores its value.
func (c *ttlCache) forget(keys ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.entries, key)
	}
	c.generation++
}

func (c *ttlCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// customHosts finds the workspace of a host name (an active custom domain or a claim's CNAME target)
// and the custom domain of a workspace, through the store and short caches: known names with their
// workspace, unknown names, and workspaces with their domain. The certificate question (CertifyHost)
// keeps its own pair, claimed and unclaimed: a name some workspace claims, active or pending, and one
// nobody claims; a name unknown to the pages may still have a pending claim, which the question checks.
// lookups merges concurrent misses of the same key into one query.
type customHosts struct {
	known, unknown     *ttlCache
	claimed, unclaimed *ttlCache
	byWorkspace        *ttlCache
	lookups            singleflight.Group
}

func newCustomHosts(now func() time.Time) *customHosts {
	return &customHosts{known: newTTLCache(hostCacheTTL, hostCacheEntries, now), unknown: newTTLCache(hostCacheTTL, hostCacheEntries, now),
		claimed: newTTLCache(hostCacheTTL, hostCacheEntries, now), unclaimed: newTTLCache(hostCacheTTL, hostCacheEntries, now),
		byWorkspace: newTTLCache(hostCacheTTL, hostCacheEntries, now)}
}

// hostGeneration is the generations of a pair of host caches, found and missing names, when a lookup
// starts.
type hostGeneration struct{ known, unknown uint64 }

func (h *customHosts) generation() hostGeneration { return pairGeneration(h.known, h.unknown) }

func pairGeneration(known, unknown *ttlCache) hostGeneration {
	return hostGeneration{known: known.currentGeneration(), unknown: unknown.currentGeneration()}
}

// remember stores what a lookup that started at generation found: the workspace of host, or "" for a
// name that belongs to none.
func (h *customHosts) remember(host, workspaceID string, generation hostGeneration) {
	rememberIn(h.known, h.unknown, host, workspaceID, generation)
}

// rememberIn stores value for host in known, or "" in unknown when value is empty; a name that moved
// leaves the other cache.
func rememberIn(known, unknown *ttlCache, host, value string, generation hostGeneration) {
	if value == "" {
		known.drop(host)
		unknown.set(host, "", generation.unknown)
		return
	}
	unknown.drop(host)
	known.set(host, value, generation.known)
}

// customHostCandidate is the host name of a request that may be a custom domain, lower case without
// port or trailing dot: never APP_ORIGIN's host or a name at or below HELP_CENTER_DOMAIN, and only a
// valid name of at least two labels that is no IP address. Every other request goes on unchanged
// without a lookup.
func (s *Site) customHostCandidate(host string) (string, bool) {
	host = requestHost(host)
	if host == "" || host == s.originHost || len(host) > maxHostLength || net.ParseIP(host) != nil || !email.ValidHostname(host) {
		return "", false
	}
	if domain := s.config.Domain; domain != "" && (host == domain || strings.HasSuffix(host, "."+domain)) {
		return "", false
	}
	if last := host[strings.LastIndex(host, ".")+1:]; strings.Trim(last, "0123456789") == "" {
		return "", false
	}
	return host, true
}

// workspaceOfHost returns the workspace whose help center serves on the custom host, or whose claim
// has the host as its CNAME target, from the caches or the store. A remembered workspace is only a
// candidate: the page's own query confirms the domain is still active for it (serve). A name no cache
// knows costs a privileged query, so the client must be allowed one more (Config.AllowLookup) first;
// otherwise wait is how long until it may. A name that was known before, its cache entry expired, is
// looked up without that charge.
func (s *Site) workspaceOfHost(request *http.Request, host string) (workspaceID string, found bool, wait time.Duration, err error) {
	workspaceID, fresh, known := s.hosts.known.peek(host)
	if fresh {
		return workspaceID, true, 0, nil
	}
	if _, unknown := s.hosts.unknown.get(host); unknown {
		return "", false, 0, nil
	}
	if !known && s.config.AllowLookup != nil {
		if allowed, wait := s.config.AllowLookup(request); !allowed {
			return "", false, max(wait, time.Second), nil
		}
	}
	find := s.config.Store.HelpCenterHost
	if claim, ok := claimOfHost(host, s.config.Domain); ok {
		find = func(ctx context.Context, _ string) (string, bool, error) {
			return s.config.Store.HelpCenterClaim(ctx, claim)
		}
	}
	generation := s.hosts.generation()
	result, err, _ := s.hosts.lookups.Do(fmt.Sprintf("host %d %d %s", generation.known, generation.unknown, host), func() (any, error) {
		workspaceID, _, err := find(context.WithoutCancel(request.Context()), host)
		if err != nil {
			return "", err
		}
		s.hosts.remember(host, workspaceID, generation)
		return workspaceID, nil
	})
	if err != nil {
		return "", false, 0, err
	}
	workspaceID = result.(string)
	return workspaceID, workspaceID != "", 0, nil
}

// Address is the public address of a workspace's help center as the API hands it out: SiteURL with
// the workspace's active custom domain, read through the cache.
func (s *Site) Address(ctx context.Context, workspaceID, slug string) (string, error) {
	custom, err := s.CustomHost(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return SiteURL(s.config.Origin, s.config.Domain, slug, custom), nil
}

// CustomHost is the workspace's active custom domain, "" when it has none, read through the cache.
func (s *Site) CustomHost(ctx context.Context, workspaceID string) (string, error) {
	if host, ok := s.hosts.byWorkspace.get(workspaceID); ok {
		return host, nil
	}
	generation := s.hosts.byWorkspace.currentGeneration()
	found, err, _ := s.hosts.lookups.Do(fmt.Sprintf("workspace %d %s", generation, workspaceID), func() (any, error) {
		current, ok, err := s.config.Store.GetHelpCenterDomain(context.WithoutCancel(ctx), workspaceID)
		if err != nil {
			return "", err
		}
		host := ""
		if ok && current.Status == domain.HelpCenterDomainActive {
			host = current.Hostname
		}
		s.hosts.byWorkspace.set(workspaceID, host, generation)
		return host, nil
	})
	if err != nil {
		return "", err
	}
	return found.(string), nil
}

// ForgetDomain makes this process look up again what a domain write changed: the workspace's address,
// and the host name and CNAME target of a claim it set, replaced, removed or whose status changed
// (hostname or claim may be ""). Every other name stays cached, and a check that changes nothing
// forgets nothing.
func (s *Site) ForgetDomain(workspaceID, hostname, claim string) {
	var hosts []string
	if hostname != "" {
		hosts = append(hosts, hostname)
	}
	if target := ClaimHost(s.config.Domain, claim); target != "" {
		hosts = append(hosts, target)
	}
	s.hosts.known.forget(hosts...)
	s.hosts.unknown.forget(hosts...)
	s.hosts.claimed.forget(hosts...)
	s.hosts.unclaimed.forget(hosts...)
	s.hosts.byWorkspace.forget(workspaceID)
}
