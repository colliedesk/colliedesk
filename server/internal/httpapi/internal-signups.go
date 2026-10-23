package httpapi

import (
	"fmt"
	"net/netip"
	"strings"
)

// InternalSignups recognises the team's own cloud sign-ups, so their trial_started event says
// internal=true and the funnel and the new-trial alert can leave tests and teammates out. A sign-up
// is the team's when its address is listed (INTERNAL_SIGNUP_EMAILS) or it comes from one of the
// team's networks (INTERNAL_SIGNUP_NETWORKS, the agent hosts, which test with any address).
type InternalSignups struct {
	domains   map[string]bool
	addresses map[string]bool
	networks  []netip.Prefix
}

// ParseInternalSignups reads the comma-separated "@domain" and "name@domain" email entries (a domain
// entry is that domain exactly; both compare case-blind) and the comma-separated network CIDRs or
// addresses. A bad entry is an error, so a typo cannot silently count the team as customers.
func ParseInternalSignups(emails, networks string) (InternalSignups, error) {
	internal := InternalSignups{domains: map[string]bool{}, addresses: map[string]bool{}}
	for _, entry := range strings.Split(emails, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		local, domain, found := strings.Cut(entry, "@")
		if !found || domain == "" {
			return InternalSignups{}, fmt.Errorf("INTERNAL_SIGNUP_EMAILS entry %q is not @domain or an email address", entry)
		}
		if local == "" {
			internal.domains[domain] = true
		} else {
			internal.addresses[mailbox(entry)] = true
		}
	}
	prefixes, err := parsePrefixes("INTERNAL_SIGNUP_NETWORKS", networks)
	if err != nil {
		return InternalSignups{}, err
	}
	internal.networks = prefixes
	return internal, nil
}

// Match reports whether a sign-up by email from clientIP (as clientIP returns it) is the team's.
func (s InternalSignups) Match(email, clientIP string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	_, domain, _ := strings.Cut(email, "@")
	if s.addresses[mailbox(email)] || s.domains[domain] {
		return true
	}
	address, err := netip.ParseAddr(clientIP)
	return err == nil && isTrusted(address, s.networks)
}

// mailbox is the one spelling of every address that reaches the same inbox: the "+tag" dropped, and
// for Gmail the dots too and googlemail.com read as gmail.com.
func mailbox(email string) string {
	local, domain, _ := strings.Cut(email, "@")
	local, _, _ = strings.Cut(local, "+")
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}
	if domain == "gmail.com" {
		local = strings.ReplaceAll(local, ".", "")
	}
	return local + "@" + domain
}
