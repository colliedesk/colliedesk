package registry_test

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// Every registered type is whole: its row in channel_types, a test that runs the shared contract
// against it, and a package of its own under internal/channels. A type added here without them fails.
func TestEveryRegisteredTypeIsWhole(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	runsContract := regexp.MustCompile(`channeltest\.Run\(`)
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	contract := string(source)
	shipped := contractValues(t, contract, "ChannelType", "Shipped types")
	types := registry.New(registry.Config{})
	for _, definition := range types.Definitions() {
		kind := string(definition.Type)
		var connectable bool
		if err := pool.QueryRow(ctx, `SELECT connectable FROM channel_types WHERE type=$1`, kind).Scan(&connectable); err != nil {
			t.Errorf("%s: no channel_types row (add it in the type's migration): %v", kind, err)
			continue
		}
		if !connectable {
			t.Errorf("%s: channel_types says it is not connectable", kind)
		}
		// The adapter's own package: its directory is named apart from every other package of the
		// server (internal/email is the email parser), since go test -c names a binary by it.
		adapter, _ := types.Adapter(definition.Type)
		dir := filepath.Join("..", path.Base(packagePath(adapter)))
		tests, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
		found := false
		for _, path := range tests {
			test, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			found = found || runsContract.Match(test)
		}
		if !found {
			t.Errorf("%s: no test in %s runs channeltest.Run (the shared contract)", kind, dir)
		}
		if definition.Name == "" || len(definition.Capabilities) == 0 {
			t.Errorf("%s: a definition needs a name and its capabilities", kind)
		}
		if _, err := os.Stat(filepath.Join("..", "..", "..", "..", "docs", "user", definition.Guide)); definition.Guide == "" || err != nil {
			t.Errorf("%s: no user guide page docs/user/%s (Definition.Guide)", kind, definition.Guide)
		}
		schema := pascal(kind) + "ChannelConfig"
		if !strings.Contains(contract, "- {$ref: '#/components/schemas/"+schema+"'}") || !strings.Contains(contract, "\n    "+schema+":") {
			t.Errorf("%s: contracts/openapi.yaml has no %s schema in the oneOf of ChannelConfig", kind, schema)
		}
		if !slices.Contains(shipped, kind) {
			t.Errorf("%s: the shipped types of ChannelType in contracts/openapi.yaml do not list it", kind)
		}
	}
}

// How a conversation started is a channel type or one of the origins that come through no channel. The
// contract documents exactly those (ConversationChannel "Values: …"): clients name and filter by them, and a
// type documented there that no type runs is a filter for nothing. The schema takes any type's name, so a
// type a client does not know yet never breaks its list.
func TestTheConversationChannelOfTheContractIsTheRegistryAndTheOrigins(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	listed := contractValues(t, string(source), "ConversationChannel", "Values")
	var want []string
	for _, kind := range registry.New(registry.Config{}).Types() {
		want = append(want, string(kind))
	}
	want = append(want, domain.ConversationOrigins...)
	slices.Sort(listed)
	slices.Sort(want)
	if !slices.Equal(listed, want) {
		t.Fatalf("ConversationChannel values %v, want the registered types and the origins %v", listed, want)
	}
}

// contractValues reads the values a schema of contracts/openapi.yaml documents in its description as
// "<label>: a, b, c." The schema itself takes any type's name (a test build runs a chat stand-in), so the
// list is what clients name and filter by, kept equal to the registry here.
func contractValues(t *testing.T, contract, schema, label string) []string {
	t.Helper()
	block := regexp.MustCompile(`\n    ` + schema + `:\n((?:      .*\n|        .*\n)*)`).FindStringSubmatch(contract)
	if block == nil {
		t.Fatalf("contracts/openapi.yaml has no %s schema", schema)
	}
	match := regexp.MustCompile(label + `: ([a-z_, ]+)\.`).FindStringSubmatch(strings.Join(strings.Fields(block[1]), " "))
	if match == nil {
		t.Fatalf("the description of %s does not say %q", schema, label+": …")
	}
	var values []string
	for _, value := range strings.Split(match[1], ",") {
		values = append(values, strings.TrimSpace(value))
	}
	return values
}

// pascal writes a type as its schema names it: web_form is WebForm.
func pascal(kind string) string {
	var b strings.Builder
	for _, part := range strings.Split(kind, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// packagePath is the import path of the package that defines the value's type.
func packagePath(value any) string {
	kind := reflect.TypeOf(value)
	for kind != nil && kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	if kind == nil {
		return ""
	}
	return kind.PkgPath()
}
