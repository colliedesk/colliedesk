package channels

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
)

// A type's defaults are shared by every channel of the type in every workspace: merging one channel's
// settings over them never writes into them, so one form's tags are never another's default.
func TestMergeLeavesATypesDefaultsAsTheyWere(t *testing.T) {
	defaults := Settings{DefaultTags: make([]string, 1, 4), Commands: map[string]Command{"bug": {}},
		Scopes: map[string]Scope{"group": {Mode: ModeCommands}}}
	defaults.DefaultTags[0] = "web-form"
	merged, err := defaults.Merge(json.RawMessage(`{"default_tags":["vip"],"commands":{"x":{}},"scopes":{"direct":{"mode":"all"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(merged.DefaultTags, []string{"vip"}) || len(merged.Commands) != 2 || len(merged.Scopes) != 2 {
		t.Fatalf("merged = %+v", merged)
	}
	if defaults.DefaultTags[0] != "web-form" || len(defaults.Commands) != 1 || len(defaults.Scopes) != 1 {
		t.Fatalf("the defaults changed: %+v", defaults)
	}
}

// Channels of a type are read at the same time by many requests (run with -race).
func TestMergeIsSafeForConcurrentReaders(t *testing.T) {
	defaults := Settings{DefaultTags: []string{"chat"}, Commands: map[string]Command{"bug": {}}, Scopes: map[string]Scope{"group": {}}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if _, err := defaults.Merge(json.RawMessage(`{"default_tags":["a","b"],"commands":{"x":{}},"scopes":{"direct":{}}}`)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
