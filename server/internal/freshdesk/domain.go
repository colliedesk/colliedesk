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

// ErrOldHelpCenterHost means the old help center's address is not a host name.
var ErrOldHelpCenterHost = errors.New("the old help center address must be a host name, for example support.acme.com")

// helpCenterHost is a DNS name of two labels or more, with an optional port.
var helpCenterHost = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?::[0-9]{1,5})?$`)

// OldHelpCenterHost normalizes what a person types for the old Freshdesk help center's own custom
// domain (an optional http:// or https:// and a trailing slash, any case) to its host, and refuses
// anything else: a path, a user, a space or a name longer than DNS allows.
func OldHelpCenterHost(address string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(address))
	for _, scheme := range []string{"https://", "http://"} {
		host = strings.TrimPrefix(host, scheme)
	}
	host = strings.TrimSuffix(host, "/")
	if len(host) > 253 || !helpCenterHost.MatchString(host) {
		return "", ErrOldHelpCenterHost
	}
	return host, nil
}
