package freshdesk

import (
	"errors"
	"regexp"
	"strings"
)

var accountHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.freshdesk\.com$`)

// ErrAccountHost means a domain is not a <account>.freshdesk.com address.
var ErrAccountHost = errors.New("the Freshdesk domain must be your <account>.freshdesk.com address")

// AccountHost normalizes what a person types for their account (an optional https:// and a trailing
// slash, any case) to its host, and refuses anything but <account>.freshdesk.com.
func AccountHost(domain string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(domain), "https://"), "/"))
	if !accountHost.MatchString(host) {
		return "", ErrAccountHost
	}
	return host, nil
}
