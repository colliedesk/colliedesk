package email

import (
	"strings"
)

// trustedStamp returns the topmost Authentication-Results header whose authserv-id is the trusted
// receiving server's, or "" when there is none or no server is trusted. The trusted server stamps its
// header above the ones it received, so a header the sender wrote is never the topmost match.
func trustedStamp(headers []string, trustedAuthServID string) string {
	if trustedAuthServID == "" {
		return ""
	}
	for _, header := range headers {
		authServ, _, _ := strings.Cut(header, ";")
		fields := strings.Fields(authServ)
		if len(fields) > 0 && strings.EqualFold(fields[0], trustedAuthServID) {
			return header
		}
	}
	return ""
}

// senderAligned tells whether stamp vouches for fromDomain: DMARC passed with header.from that
// domain, or a DKIM signature whose domain (header.d, or the domain of header.i) is exactly that
// domain passed, and no DMARC check failed. SPF is never enough: it checks the envelope sender, not
// From.
func senderAligned(stamp, fromDomain string) bool {
	fromDomain = strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	if stamp == "" || fromDomain == "" {
		return false
	}
	results := strings.Split(withoutComments(stamp), ";")
	aligned := false
	for _, result := range results[1:] {
		fields := strings.Fields(strings.ToLower(result))
		if len(fields) == 0 {
			continue
		}
		method, verdict, _ := strings.Cut(fields[0], "=")
		properties := map[string]string{}
		for _, field := range fields[1:] {
			if key, value, ok := strings.Cut(field, "="); ok {
				properties[key] = strings.TrimSuffix(strings.Trim(value, `"`), ".")
			}
		}
		switch {
		case method == "dmarc" && verdict == "fail":
			return false
		case method == "dmarc" && verdict == "pass":
			aligned = aligned || properties["header.from"] == fromDomain
		case method == "dkim" && verdict == "pass":
			_, identity, _ := strings.Cut(properties["header.i"], "@")
			aligned = aligned || properties["header.d"] == fromDomain || identity == fromDomain
		}
	}
	return aligned
}

// withoutComments drops RFC 5322 comments, (nested ones too), which may hold any text.
func withoutComments(value string) string {
	var out strings.Builder
	depth := 0
	for _, r := range value {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0:
			out.WriteRune(r)
		}
	}
	return out.String()
}
