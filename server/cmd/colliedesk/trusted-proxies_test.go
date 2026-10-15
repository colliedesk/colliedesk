package main

import (
	"net/netip"
	"testing"
)

// lookup stands in for os.LookupEnv; set false means the variable is not in the environment at all.
func lookup(t *testing.T, value string, set bool) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key != "TRUSTED_PROXY_CIDRS" {
			t.Fatalf("read unexpected variable %q", key)
		}
		return value, set
	}
}

func prefixes(list []netip.Prefix) []string {
	var out []string
	for _, prefix := range list {
		out = append(out, prefix.String())
	}
	return out
}

func TestTrustedProxiesFromEnv(t *testing.T) {
	got, err := trustedProxiesFromEnv(lookup(t, " 10.0.0.0/8, 192.0.2.7 ,fd00::/8,", true))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.0/8", "192.0.2.7/32", "fd00::/8"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}
	if _, err := trustedProxiesFromEnv(lookup(t, "10.0.0.0/8,not-an-ip", true)); err == nil {
		t.Fatal("accepted an entry that is neither an address nor a CIDR range")
	}
}

func TestUnsetTrustedProxiesDefaultsToTheLocalProxy(t *testing.T) {
	got, err := trustedProxiesFromEnv(lookup(t, "", false))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"127.0.0.0/8", "::1/128", "172.16.0.0/12"}
	if len(got) != len(want) {
		t.Fatalf("unset = %v, want %v", prefixes(got), want)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("entry %d = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestExplicitlyEmptyTrustedProxiesTrustNobody(t *testing.T) {
	got, err := trustedProxiesFromEnv(lookup(t, "", true))
	if err != nil || len(got) != 0 {
		t.Fatalf("explicitly empty = %v, %v; want no proxies", prefixes(got), err)
	}
}
