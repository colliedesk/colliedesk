package freshdesk

import (
	"net"
	"testing"
)

func TestDialGuardBlocksReservedAndTranslatedAddresses(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0",
		"198.18.0.1", "198.19.255.254", "192.0.0.8", "240.0.0.1", "255.255.255.255", "224.0.0.1",
		"::1", "fe80::1", "fc00::1", "::ffff:10.0.0.1",
		"64:ff9b::a9fe:a9fe", "64:ff9b:1::1", "2002:a9fe:a9fe::1", "2001:0:4136:e378::1", "2001:db8::1",
	}
	for _, address := range blocked {
		if !blockedIP(net.ParseIP(address)) {
			t.Errorf("%s is not blocked", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "52.95.110.1", "2606:4700::1111", "2a00:1450:4001::1"} {
		if blockedIP(net.ParseIP(address)) {
			t.Errorf("public address %s is blocked", address)
		}
	}
}
