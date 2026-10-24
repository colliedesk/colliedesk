package registry_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
)

// Every registered type is whole: its row in channel_types, a test that runs the shared contract
// against it, and a package of its own under internal/channels. A type added here without them fails.
func TestEveryRegisteredTypeIsWhole(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	contract := regexp.MustCompile(`channeltest\.Run\(`)
	for _, definition := range registry.New(registry.Config{}).Definitions() {
		kind := string(definition.Type)
		var connectable, singleton bool
		if err := pool.QueryRow(ctx, `SELECT connectable,singleton FROM channel_types WHERE type=$1`, kind).Scan(&connectable, &singleton); err != nil {
			t.Errorf("%s: no channel_types row (add it in the type's migration): %v", kind, err)
			continue
		}
		if !connectable || singleton != definition.Singleton {
			t.Errorf("%s: channel_types says connectable=%v singleton=%v, the definition singleton=%v", kind, connectable, singleton, definition.Singleton)
		}
		dir := filepath.Join("..", strings.ReplaceAll(kind, "_", ""))
		tests, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
		found := false
		for _, path := range tests {
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			found = found || contract.Match(source)
		}
		if !found {
			t.Errorf("%s: no test in %s runs channeltest.Run (the shared contract)", kind, dir)
		}
		if definition.Name == "" || len(definition.Capabilities) == 0 {
			t.Errorf("%s: a definition needs a name and its capabilities", kind)
		}
	}
}
