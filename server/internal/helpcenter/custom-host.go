package helpcenter

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"golang.org/x/sync/singleflight"
)

const (
	// hostCacheTTL is how long this process remembers which workspace a custom host belongs to (or that
	// it belongs to none), and a workspace's active custom domain. A domain write in this process clears
	// both at once; another process sees it within this time.
	hostCacheTTL = 30 * time.Second
	// hostCacheEntries bounds each cache, unknown host names included, so random Host headers cannot
	// grow it.
	hostCacheEntries = 4096
)

// ttlCache remembers values for a while, up to max keys. When full it drops expired keys first, then
// arbitrary ones (map order), so it never grows past max. Every clear starts a new generation; a value
// read from the store before a clear is not kept after it (set takes the generation it started in).
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

func (c *ttlCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expires) {
		return "", false
	}
	return entry.value, true
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

func (c *ttlCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	c.generation++
}

func (c *ttlCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// customHosts finds the workspace of a custom host name and the custom domain of a workspace, through
// the store and a short cache. lookups merges concurrent misses of the same key into one query.
type customHosts struct {
	byHost      *ttlCache
	byWorkspace *ttlCache
	lookups     singleflight.Group
}

func newCustomHosts(now func() time.Time) *customHosts {
	return &customHosts{byHost: newTTLCache(hostCacheTTL, hostCacheEntries, now), byWorkspace: newTTLCache(hostCacheTTL, hostCacheEntries, now)}
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
// has the host as its CNAME target, from the cache or the store. A remembered workspace is only a
// candidate: the page's own query confirms the domain is still active for it (serve).
func (s *Site) workspaceOfHost(ctx context.Context, host string) (string, bool, error) {
	if workspaceID, ok := s.hosts.byHost.get(host); ok {
		return workspaceID, workspaceID != "", nil
	}
	find := s.config.Store.HelpCenterHost
	if claim, ok := claimOfHost(host, s.config.Domain); ok {
		find = func(ctx context.Context, _ string) (string, bool, error) {
			return s.config.Store.HelpCenterClaim(ctx, claim)
		}
	}
	generation := s.hosts.byHost.currentGeneration()
	found, err, _ := s.hosts.lookups.Do(fmt.Sprintf("host %d %s", generation, host), func() (any, error) {
		workspaceID, _, err := find(context.WithoutCancel(ctx), host)
		if err != nil {
			return "", err
		}
		s.hosts.byHost.set(host, workspaceID, generation)
		return workspaceID, nil
	})
	if err != nil {
		return "", false, err
	}
	workspaceID := found.(string)
	return workspaceID, workspaceID != "", nil
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

// ForgetDomains clears what this process remembers about custom domains; every domain write calls it,
// so this process serves a new, changed or removed domain with its next request.
func (s *Site) ForgetDomains() {
	s.hosts.byHost.clear()
	s.hosts.byWorkspace.clear()
}
