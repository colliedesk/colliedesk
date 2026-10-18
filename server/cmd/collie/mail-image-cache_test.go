package main

import "testing"

func TestMailImageCacheBytesFromEnv(t *testing.T) {
	env := func(value string) func(string) string {
		return func(name string) string {
			if name == "MAIL_IMAGE_CACHE_BYTES" {
				return value
			}
			return ""
		}
	}
	for value, want := range map[string]int64{"": 1 << 30, "536870912": 512 << 20, " 2048 ": 2048} {
		if got, err := mailImageCacheBytesFromEnv(env(value)); err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", value, got, err, want)
		}
	}
	for _, bad := range []string{"0", "-5", "1GB", "abc", "1.5"} {
		if _, err := mailImageCacheBytesFromEnv(env(bad)); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
