package helpcenter

import (
	"fmt"
	"testing"
	"time"
)

// The host cache forgets after its time, never holds more than its bound however many names arrive,
// and never keeps a value read before a forget.
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
	cache.set("kept.example", "workspace-k", cache.currentGeneration())
	stale := cache.currentGeneration()
	cache.forget("random-99.example")
	cache.set("b.example", "workspace-b", stale)
	if _, ok := cache.get("b.example"); ok {
		t.Fatal("a value read before the forget was kept after it")
	}
	if got, ok := cache.get("kept.example"); !ok || got != "workspace-k" {
		t.Fatal("forgetting one name dropped another")
	}
}

// However many unknown names arrive, they evict only other unknown names: a known host keeps its
// workspace, and stays held (for a lookup without the client's charge) after its time.
func TestUnknownHostsNeverEvictKnownOnes(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	hosts := newCustomHosts(func() time.Time { return now })
	hosts.remember("help.real.example", "workspace-a", hosts.generation())
	for index := range 3 * hostCacheEntries {
		hosts.remember(fmt.Sprintf("random-%d.example", index), "", hosts.generation())
	}
	if hosts.unknown.len() > hostCacheEntries {
		t.Fatalf("%d unknown names remembered, bound %d", hosts.unknown.len(), hostCacheEntries)
	}
	if got, fresh, _ := hosts.known.peek("help.real.example"); !fresh || got != "workspace-a" {
		t.Fatalf("the known host = %q, fresh %v", got, fresh)
	}
	now = now.Add(time.Minute)
	if _, fresh, held := hosts.known.peek("help.real.example"); fresh || !held {
		t.Fatalf("after its time the known host is fresh %v, held %v", fresh, held)
	}
	// A known name that turns out unknown leaves the known cache.
	hosts.remember("help.real.example", "", hosts.generation())
	if _, _, held := hosts.known.peek("help.real.example"); held {
		t.Fatal("a removed host is still held as known")
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
