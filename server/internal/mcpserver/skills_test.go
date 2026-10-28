package mcpserver_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpbridge"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

const plugin = "../../../plugins/colliedesk"

// The skills in plugins/colliedesk follow the Agent Skills format (agentskills.io), so Codex,
// Cursor, VS Code and claude.ai load them as they are, and they name only tools the server has,
// at the tier the server gives them.
func TestSkillsNameRealToolsAtTheirTier(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(plugin, "skills", "*"))
	if err != nil || len(dirs) == 0 {
		t.Skipf("plugins/colliedesk is not next to this module: %v", err)
	}
	tiers := map[string]string{}
	for _, spec := range mcpserver.Catalog() {
		tiers[spec.Name] = spec.Tier
	}
	name := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	tool := regexp.MustCompile(`colliedesk_[a-z_]*[a-z]`)
	row := regexp.MustCompile("(?m)^\\| `(colliedesk_[a-z_]+)` \\| ([a-z]+) \\|$")
	allowed := []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil {
			t.Errorf("%s: %v", dir, err)
			continue
		}
		skill := filepath.Base(dir)
		front, body, ok := strings.Cut(strings.TrimPrefix(string(raw), "---\n"), "\n---\n")
		if !ok || !strings.HasPrefix(string(raw), "---\n") {
			t.Errorf("%s: no frontmatter", skill)
			continue
		}
		fields := map[string]string{}
		for _, line := range strings.Split(front, "\n") {
			key, value, _ := strings.Cut(line, ":")
			if !slices.Contains(allowed, key) {
				t.Errorf("%s: frontmatter field %q is not in the Agent Skills spec; claude.ai refuses the upload", skill, key)
			}
			fields[key] = strings.TrimSpace(value)
		}
		if fields["name"] != skill || len(skill) > 64 || !name.MatchString(skill) {
			t.Errorf("%s: name %q must equal the folder, lower case and single hyphens, at most 64 characters", skill, fields["name"])
		}
		if description := fields["description"]; description == "" || len(description) > 1024 {
			t.Errorf("%s: description of %d characters, want 1 to 1024", skill, len(description))
		}
		if lines := strings.Count(string(raw), "\n"); lines > 500 {
			t.Errorf("%s: %d lines; the spec asks for at most 500", skill, lines)
		}

		table := map[string]string{}
		for _, match := range row.FindAllStringSubmatch(body, -1) {
			table[match[1]] = match[2]
		}
		if len(table) == 0 {
			t.Errorf("%s has no Tools table of `tool` | tier rows", skill)
		}
		for _, used := range tool.FindAllString(body, -1) {
			tier, real := tiers[used]
			switch {
			case !real:
				t.Errorf("%s names %s, which is not a tool of the catalog", skill, used)
			case table[used] == "":
				t.Errorf("%s uses %s but its Tools table does not list it", skill, used)
			case table[used] != tier:
				t.Errorf("%s lists %s at tier %s; the catalog says %s", skill, used, table[used], tier)
			}
		}
		// The plugin follows main, a person's server may be older: every skill checks first.
		if !strings.Contains(body, "Call `colliedesk_whoami`") || !strings.Contains(body, "tools_you_can_use") {
			t.Errorf("%s must start from colliedesk_whoami and its tools_you_can_use", skill)
		}
	}
}

// The plugin asks for the token as a sensitive option (kept in the system's credential store) and
// only references it; no file of the plugin holds one.
func TestPluginKeepsTheTokenOutOfItsFiles(t *testing.T) {
	read := func(path string, into any) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("plugins/colliedesk is not next to this module: %v", err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return string(raw)
	}
	var manifest struct {
		Name       string `json:"name"`
		Version    string `json:"version"`
		UserConfig map[string]struct {
			Sensitive bool `json:"sensitive"`
			Required  bool `json:"required"`
		} `json:"userConfig"`
	}
	read(filepath.Join(plugin, ".claude-plugin", "plugin.json"), &manifest)
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	read("../../../.claude-plugin/marketplace.json", &marketplace)
	var servers struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	raw := read(filepath.Join(plugin, ".mcp.json"), &servers)

	if manifest.Name != "colliedesk" || manifest.Version != "" {
		t.Errorf("plugin %q, version %q: no version, so an install follows main", manifest.Name, manifest.Version)
	}
	if token := manifest.UserConfig["token"]; !token.Sensitive || !token.Required {
		t.Errorf("the token option must be required and sensitive: %+v", token)
	}
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != manifest.Name || marketplace.Plugins[0].Source != "./plugins/colliedesk" {
		t.Errorf("marketplace %+v does not offer the plugin", marketplace)
	}
	server, ok := servers.MCPServers["colliedesk"]
	if !ok || server.Type != "http" || server.URL != "${user_config.url}" || server.Headers["Authorization"] != "Bearer ${user_config.token}" {
		t.Errorf(".mcp.json: %+v", servers)
	}
	if strings.Contains(raw, mcpbridge.TokenPrefix) {
		t.Error(".mcp.json holds a token")
	}
}
