package netguard

import (
	"net"
	"testing"
)

func TestBlockedCoversReservedAndTranslatedAddresses(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0",
		"198.18.0.1", "198.19.255.254", "192.0.0.8", "240.0.0.1", "255.255.255.255", "224.0.0.1",
		"::1", "::", "fe80::1", "fc00::1", "fd00:ec2::254", "ff02::1", "::ffff:10.0.0.1", "::ffff:127.0.0.1",
		"::127.0.0.1", "::a9fe:a9fe", "::10.0.0.1", "fec0::1", "100::1", "100:0:0:0:ffff::1",
		"64:ff9b::a9fe:a9fe", "64:ff9b:1::1", "2002:a9fe:a9fe::1", "2001:0:4136:e378::1", "2001:db8::1",
	}
	for _, address := range blocked {
		if !Blocked(net.ParseIP(address)) {
			t.Errorf("%s is not blocked", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "52.95.110.1", "2606:4700::1111", "2a00:1450:4001::1"} {
		if Blocked(net.ParseIP(address)) {
			t.Errorf("public address %s is blocked", address)
		}
	}
}

func TestControlRefusesNameThatResolvedToPrivateAddress(t *testing.T) {
	if err := Control(nil)("tcp", "10.0.0.5:443", nil); err == nil {
		t.Fatal("a dial to a private address was allowed")
	}
	if err := Control(nil)("tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("a public address was refused: %v", err)
	}
	if err := Control(map[string]bool{"127.0.0.1:9": true})("tcp", "127.0.0.1:9", nil); err != nil {
		t.Fatalf("an explicitly allowed address was refused: %v", err)
	}
}
