package helpcenter

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"golang.org/x/net/idna"
)

// VerifyRecordPrefix names the TXT record that proves a workspace owns a custom domain:
// _help-center-verify.<host name> holds the workspace's token.
const VerifyRecordPrefix = "_help-center-verify."

// maxHostLength is the longest name DNS carries.
const maxHostLength = 253

// CustomHost cleans a host name an admin sets as the help center's custom domain and refuses one
// that cannot be: an IP address, a single label, a name of more than 253 characters, APP_ORIGIN's
// host, and HELP_CENTER_DOMAIN with every name below it (those already belong to the server). A
// Unicode name is returned in its ASCII (punycode) form, which is what DNS, browsers and the reverse
// proxy use. The error's text is for the person who typed the name.
func CustomHost(name, origin, domain string) (string, error) {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" {
		return "", errors.New("enter a host name such as help.example.com")
	}
	if net.ParseIP(strings.Trim(name, "[]")) != nil {
		return "", errors.New("an IP address cannot be a custom domain; enter a host name such as help.example.com")
	}
	if strings.ContainsAny(name, "/:@?#[]") {
		return "", errors.New("enter the host name only, such as help.example.com, without https:// or a path")
	}
	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil || !email.ValidHostname(ascii) {
		return "", errors.New("this is not a valid host name; use letters, digits and hyphens, such as help.example.com")
	}
	ascii = strings.ToLower(ascii)
	if len(ascii) > maxHostLength {
		return "", errors.New("a host name has at most 253 characters")
	}
	if last := ascii[strings.LastIndex(ascii, ".")+1:]; strings.Trim(last, "0123456789") == "" {
		return "", errors.New("this is not a valid host name; it must end in a domain such as .com")
	}
	if originHost := hostOf(origin); ascii == originHost {
		return "", errors.New("this is the address of the app; choose another host name for the help center")
	}
	if domain != "" && (ascii == domain || strings.HasSuffix(ascii, "."+domain)) {
		return "", errors.New("names below " + domain + " already belong to this server; choose a host name of your own domain")
	}
	return ascii, nil
}

// hostOf is the lower-case host name of an origin, without its port.
func hostOf(origin string) string {
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// SlugHost is the host name a custom domain's CNAME points at to prove it belongs to the workspace:
// <slug>.<HELP_CENTER_DOMAIN>, or "" when the server has no help center domain or the slug cannot be
// a host name. Every workspace shares the app's own host, so that one proves nothing.
func SlugHost(domain, slug string) string {
	if domain == "" || email.ValidateHostedSlug(slug) != nil {
		return ""
	}
	return slug + "." + domain
}

// Resolvers look up DNS. CNAME returns the canonical name of a host, the end of its CNAME chain (the
// host itself when it has none), as net.Resolver.LookupCNAME does.
type Resolvers struct {
	CNAME func(ctx context.Context, host string) (string, error)
	TXT   func(ctx context.Context, name string) ([]string, error)
}

// ErrDomainNotProven matches every error of CheckDomain.
var ErrDomainNotProven = errors.New("the DNS records do not prove that the workspace owns this host name")

// notProven is CheckDomain's answer when ownership is not proven; its text says what DNS showed and
// what to set.
type notProven struct{ message string }

func (e *notProven) Error() string        { return e.message }
func (e *notProven) Is(target error) bool { return target == ErrDomainNotProven }

// CheckDomain reports whether DNS proves that a workspace owns host: the end of host's CNAME chain is
// the workspace's own slug host, or the TXT record _help-center-verify.<host> holds its token. An A
// or AAAA record proves nothing, since every custom domain resolves to the same proxy; without a slug
// host (no HELP_CENTER_DOMAIN) only the token does. Both lookups run at once and stop with ctx.
func CheckDomain(ctx context.Context, resolvers Resolvers, host, slugHost, token string) error {
	type answer struct {
		canonical string
		values    []string
		err       error
	}
	cname, txt := make(chan answer, 1), make(chan answer, 1)
	if slugHost != "" && resolvers.CNAME != nil {
		go func() {
			canonical, err := resolvers.CNAME(ctx, host)
			cname <- answer{canonical: strings.ToLower(strings.TrimSuffix(canonical, ".")), err: err}
		}()
	} else {
		cname <- answer{err: errors.New("not asked")}
	}
	if resolvers.TXT != nil {
		go func() {
			values, err := resolvers.TXT(ctx, VerifyRecordPrefix+host)
			txt <- answer{values: values, err: err}
		}()
	} else {
		txt <- answer{err: errors.New("not asked")}
	}
	byName, byToken := <-cname, <-txt
	if byName.err == nil && byName.canonical == slugHost {
		return nil
	}
	if byToken.err == nil {
		for _, value := range byToken.values {
			if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(value)), []byte(token)) == 1 {
				return nil
			}
		}
	}
	verify := VerifyRecordPrefix + host + " does not hold this workspace's TXT token"
	var message string
	switch {
	case slugHost == "":
		message = verify + " yet."
	case byName.err == nil && byName.canonical != "" && byName.canonical != host:
		message = host + " points at " + byName.canonical + ", not " + slugHost + ", and " + verify + "."
	default:
		message = host + " has no CNAME record pointing at " + slugHost + ", and " + verify + "."
	}
	if ctx.Err() != nil {
		message += " DNS did not answer in time."
	}
	return &notProven{message: message + " DNS changes can take a few minutes to reach every server."}
}

// DomainRecord is a DNS record an admin sets for a custom domain. Purpose is route_and_verify (it
// sends visitors to the help center and proves ownership), route or verify.
type DomainRecord struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Purpose string `json:"purpose"`
}

// Records lists the DNS records for a custom domain: one that sends the host name to the server and
// the TXT record with the token. On a server with a help center domain the first is a CNAME to the
// workspace's slug host, which also proves ownership; otherwise it points at the app's host (a CNAME,
// or an A or AAAA record when APP_ORIGIN is an IP address) and only routes.
func Records(host, token, origin, domain, slug string) []DomainRecord {
	verify := DomainRecord{Type: "TXT", Name: VerifyRecordPrefix + host, Value: token, Purpose: "verify"}
	if slugHost := SlugHost(domain, slug); slugHost != "" {
		return []DomainRecord{{Type: "CNAME", Name: host, Value: slugHost, Purpose: "route_and_verify"}, verify}
	}
	route := DomainRecord{Type: "CNAME", Name: host, Value: hostOf(origin), Purpose: "route"}
	if ip := net.ParseIP(route.Value); ip != nil {
		route.Type, route.Value = "AAAA", ip.String()
		if ip.To4() != nil {
			route.Type = "A"
		}
	}
	return []DomainRecord{route, verify}
}

// LiveChecks bounds the DNS checks the reverse proxy's certificate question may start: a few at a
// time, and one per host name per back-off, so asking about a name over and over never makes the
// server flood DNS. It remembers at most max host names; past that it forgets the ones whose back-off
// is over, and refuses while every one is recent.
type LiveChecks struct {
	slots   chan struct{}
	backoff time.Duration
	max     int
	now     func() time.Time
	mu      sync.Mutex
	last    map[string]time.Time
}

// NewLiveChecks bounds live checks to concurrency at once and one per host per backoff.
func NewLiveChecks(concurrency int, backoff time.Duration, max int, now func() time.Time) *LiveChecks {
	return &LiveChecks{slots: make(chan struct{}, concurrency), backoff: backoff, max: max, now: now, last: map[string]time.Time{}}
}

// Begin reports whether a live check of host may run now; when it may, done frees its slot.
func (c *LiveChecks) Begin(host string) (done func(), ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	at, seen := c.last[host]
	if seen && now.Sub(at) < c.backoff {
		return nil, false
	}
	if !seen && len(c.last) >= c.max {
		for name, at := range c.last {
			if now.Sub(at) >= c.backoff {
				delete(c.last, name)
			}
		}
		if len(c.last) >= c.max {
			return nil, false
		}
	}
	select {
	case c.slots <- struct{}{}:
	default:
		return nil, false
	}
	c.last[host] = now
	return func() { <-c.slots }, true
}
