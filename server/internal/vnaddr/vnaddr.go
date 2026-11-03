// Package vnaddr tells whether an IP address is Vietnamese, from the address blocks APNIC delegates
// to Vietnam (ranges.txt, written by `go run ./cmd/vn-ranges`). It needs no service and no database,
// so a self-hosted server answers the same as the cloud.
package vnaddr

import (
	_ "embed"
	"net/netip"
	"strings"
)

//go:embed ranges.txt
var rangesText string

var ranges = parse(rangesText)

func parse(text string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		prefixes = append(prefixes, netip.MustParsePrefix(line))
	}
	return prefixes
}

// Located reports whether the address says where its person is: a public address. This machine, a
// private network or no address at all says nothing, and a caller treats it as unknown.
func Located(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() && address.IsGlobalUnicast() && !address.IsPrivate()
}

// Contains reports whether the address is in a block delegated to Vietnam.
func Contains(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range ranges {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
