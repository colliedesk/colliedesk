package mcpserver_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// An agent sets up any channel type in one call only when the tools say what each type takes: the config
// of colliedesk_channels_create and _update names every registered type, and
// colliedesk_channels_credentials_set names every secret a type keeps.
func TestTheChannelToolsDescribeEveryRegisteredType(t *testing.T) {
	specs := map[string]mcpserver.Spec{}
	for _, spec := range mcpserver.Catalog() {
		specs[spec.Name] = spec
	}
	configOf := func(name string) string {
		var input struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal([]byte(specs[name].Input), &input); err != nil {
			t.Fatalf("%s input: %v", name, err)
		}
		return input.Properties["config"].Description
	}
	reg := registry.New(registry.Config{})
	for _, kind := range reg.Types() {
		adapter, _ := reg.Adapter(kind)
		for _, tool := range []string{"colliedesk_channels_create", "colliedesk_channels_update"} {
			if !strings.Contains(configOf(tool), string(kind)+" {") {
				t.Errorf("the config of %s does not say what %s takes: add `%s {fields}` to channelConfig in server/internal/mcpserver/catalog-channels.go", tool, kind, kind)
			}
		}
		for _, credential := range adapter.Config().Credentials() {
			description := specs["colliedesk_channels_credentials_set"].Description
			if !strings.Contains(description, string(kind)+":") || !strings.Contains(description, credential.Name) {
				t.Errorf("colliedesk_channels_credentials_set does not name %s's secret %s", kind, credential.Name)
			}
		}
	}
}
