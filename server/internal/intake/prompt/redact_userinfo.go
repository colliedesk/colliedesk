package prompt

import (
	"net/netip"
	"regexp"
	"strings"
)

var (
	// The user part of a link or a connection string: scheme://user:password@host, the user possibly
	// empty. Any password counts, up to the last "@" before the host (a password may hold one).
	schemeUserinfoPattern = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.\-]*://[^\s:/@]*:)([^\s/]+)@([^\s/@]+)`)
	// user:password@host without a scheme, as an ssh or a database login is written.
	bareUserinfoPattern = regexp.MustCompile(`(^|[\s(\[<"'])(\p{L}[\w.\-]*:)([^\s:/]+)@([^\s/@]+)`)
)

// hideUserinfo hides the password of a user part: always after a scheme; without one only when the user
// starts with a letter and the password is not all digits or the host is a domain or an address, so
// "10:30@office" stays.
func hideUserinfo(line string) string {
	line = schemeUserinfoPattern.ReplaceAllString(line, "${1}"+hidden+"@${3}")
	return bareUserinfoPattern.ReplaceAllStringFunc(line, func(match string) string {
		m := bareUserinfoPattern.FindStringSubmatch(match)
		host := strings.TrimRight(m[4], ".,;:!?)]>\"'")
		if allDigits(m[3]) && !strings.Contains(host, ".") && !isAddress(host) {
			return match
		}
		return m[1] + m[2] + hidden + "@" + m[4]
	})
}

func isAddress(host string) bool {
	_, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil
}
