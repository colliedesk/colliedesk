package main

import "testing"

func TestTrustedProxiesFromEnv(t *testing.T) {
	env := func(value string) func(string) string {
		return func(key string) string {
			if key != "TRUSTED_PROXY_CIDRS" {
				t.Fatalf("read unexpected variable %q", key)
			}
			return value
		}
	}
	got, err := trustedProxiesFromEnv(env(" 10.0.0.0/8, 192.0.2.7 ,fd00::/8,"))
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
	if none, err := trustedProxiesFromEnv(env("")); err != nil || len(none) != 0 {
		t.Fatalf("empty = %v, %v; want no proxies", none, err)
	}
	if _, err := trustedProxiesFromEnv(env("10.0.0.0/8,not-an-ip")); err == nil {
		t.Fatal("accepted an entry that is neither an address nor a CIDR range")
	}
}
