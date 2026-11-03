package vnaddr_test

import (
	"net/netip"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/vnaddr"
)

// The expected answers come from APNIC's delegation file, read by hand: 14.160.0.0/11 and
// 2402:800::/32 are delegated to Vietnam; 8.8.8.8 and 2001:4860::/32 are Google's, in the US.
func TestContainsKnowsVietnameseAddresses(t *testing.T) {
	cases := []struct {
		address string
		want    bool
	}{
		{"14.160.0.1", true},
		{"14.191.255.254", true},
		{"14.192.0.1", false},
		{"8.8.8.8", false},
		{"2402:800:6311::1", true},
		{"2001:4860:4860::8888", false},
		{"::ffff:14.160.0.1", true},
	}
	for _, c := range cases {
		if got := vnaddr.Contains(netip.MustParseAddr(c.address)); got != c.want {
			t.Errorf("Contains(%s) = %v, want %v", c.address, got, c.want)
		}
	}
}

// An address that says nothing about where the person is (this machine, a private network, none at
// all) is unknown, never "outside Vietnam".
func TestLocatedOnlyForPublicAddresses(t *testing.T) {
	for _, address := range []netip.Addr{{}, netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("10.1.2.3"),
		netip.MustParseAddr("172.18.0.1"), netip.MustParseAddr("192.168.1.10"), netip.MustParseAddr("fd00::1"), netip.MustParseAddr("::1")} {
		if vnaddr.Located(address) {
			t.Errorf("Located(%v) = true, want false", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "14.160.0.1", "2001:4860:4860::8888"} {
		if !vnaddr.Located(netip.MustParseAddr(address)) {
			t.Errorf("Located(%s) = false, want true", address)
		}
	}
}
