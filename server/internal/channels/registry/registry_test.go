package registry_test

import (
	"context"
	"os"
	"path/filepath"
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
	channelTypeEnum := ""
	if start := strings.Index(contract, "\n    ChannelType:\n"); start >= 0 {
		channelTypeEnum = contract[start : start+200]
	}
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
		if !strings.Contains(channelTypeEnum, kind) {
			t.Errorf("%s: the ChannelType enum of contracts/openapi.yaml does not list it", kind)
		}
	}
}

// How a conversation started is a channel type or one of the origins that come through no channel. The
// contract lists exactly those (ConversationChannel): the web app parses every conversation against it,
// so a type missing there breaks the inbox, and one listed there that no type runs is a filter for nothing.
func TestTheConversationChannelOfTheContractIsTheRegistryAndTheOrigins(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	enum := regexp.MustCompile(`\n    ConversationChannel:\n      type: string\n      enum: \[([^\]]*)\]`).FindStringSubmatch(string(source))
	if enum == nil {
		t.Fatal("contracts/openapi.yaml has no ConversationChannel enum")
	}
	var listed []string
	for _, value := range strings.Split(enum[1], ",") {
		listed = append(listed, strings.TrimSpace(value))
	}
	var want []string
	for _, kind := range registry.New(registry.Config{}).Types() {
		want = append(want, string(kind))
	}
	want = append(want, domain.ConversationOrigins...)
	slices.Sort(listed)
	slices.Sort(want)
	if !slices.Equal(listed, want) {
		t.Fatalf("ConversationChannel enum %v, want the registered types and the origins %v", listed, want)
	}
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
