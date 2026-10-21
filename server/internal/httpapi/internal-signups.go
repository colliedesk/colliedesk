package httpapi

import (
	"fmt"
	"strings"
)

// InternalSignups recognises the team's own cloud sign-ups (INTERNAL_SIGNUP_EMAILS), so their
// trial_started event says internal=true and the funnel can leave tests and teammates out. An entry
// is a whole domain ("@example.com", that domain exactly) or one address; both compare case-blind.
type InternalSignups struct {
	domains   map[string]bool
	addresses map[string]bool
}

// ParseInternalSignups reads a comma-separated list of "@domain" and "name@domain" entries.
func ParseInternalSignups(value string) (InternalSignups, error) {
	internal := InternalSignups{domains: map[string]bool{}, addresses: map[string]bool{}}
	for _, entry := range strings.Split(value, ",") {
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
			internal.addresses[entry] = true
		}
	}
	return internal, nil
}

// Match reports whether email belongs to the team.
func (s InternalSignups) Match(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	_, domain, _ := strings.Cut(email, "@")
	return s.addresses[email] || s.domains[domain]
}
