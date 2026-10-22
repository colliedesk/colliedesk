package helpcenter

import (
	"fmt"
	"testing"
	"time"
)

// The host cache forgets after its time, never holds more than its bound however many names arrive,
// and never keeps a value read before a clear.
func TestHostCacheExpiresStaysBoundedAndForgetsOnClear(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	cache := newTTLCache(30*time.Second, 3, func() time.Time { return now })
	cache.set("a.example", "workspace-a", cache.currentGeneration())
	if got, ok := cache.get("a.example"); !ok || got != "workspace-a" {
		t.Fatalf("get = %q, %v", got, ok)
	}
	now = now.Add(30 * time.Second)
	if _, ok := cache.get("a.example"); ok {
		t.Fatal("an entry outlived its 30 seconds")
	}
	for index := range 100 {
		cache.set(fmt.Sprintf("random-%d.example", index), "", cache.currentGeneration())
		if cache.len() > 3 {
			t.Fatalf("the cache grew to %d entries", cache.len())
		}
	}
	stale := cache.currentGeneration()
	cache.clear()
	cache.set("b.example", "workspace-b", stale)
	if _, ok := cache.get("b.example"); ok || cache.len() != 0 {
		t.Fatal("a value read before the clear was kept after it")
	}
}

func TestCustomHostCandidateSkipsTheServersOwnNames(t *testing.T) {
	site := New(Config{Origin: "https://desk.example.com", Domain: "help.example.net"})
	for host, want := range map[string]string{
		"help.acme.example": "help.acme.example", "HELP.Acme.Example.:443": "help.acme.example",
		"desk.example.com": "", "desk.example.com:443": "", "acme.help.example.net": "", "help.example.net": "", "a.b.help.example.net": "",
		"localhost": "", "localhost:8080": "", "127.0.0.1:8080": "", "[::1]:8080": "", "10.0.0": "", "": "", "bad_host.example": "",
	} {
		if got, _ := site.customHostCandidate(host); got != want {
			t.Errorf("customHostCandidate(%q) = %q, want %q", host, got, want)
		}
	}
}
