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
	"github.com/colliedesk/colliedesk/server/internal/mcpserver/docgen"
)

const plugin = "../../../plugins/colliedesk"

// The skills in plugins/colliedesk follow the Agent Skills format (agentskills.io), so Codex,
// Cursor, VS Code and claude.ai load them as they are. A skill says what it is for and links the
// colliedesk.com guides; it never copies a feature's details, tools or permissions, which change with
// Colliedesk. The agent learns them at run time from colliedesk_whoami, the tool descriptions and
// the linked pages (colliedesk_docs_search returns the same guides from the person's server).
func TestSkillsDescribeTheirPurposeAndLinkTheGuides(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(plugin, "skills", "*"))
	if err != nil || len(dirs) == 0 {
		t.Skipf("plugins/colliedesk is not next to this module: %v", err)
	}
	name := regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	tool := regexp.MustCompile(`colliedesk_[a-z_]*[a-z]`)
	heading := regexp.MustCompile(`(?m)^#{1,6} .*$`)
	link := regexp.MustCompile(`https?://[^)\s>]+`)
	guide := regexp.MustCompile(`^https://colliedesk\.com/docs/([a-z0-9-]+)\.md$`)
	index, err := os.ReadFile("../../../docs/user/README.md")
	if err != nil {
		t.Fatal(err)
	}
	sections := []string{"## What it is for", "## When to use", "## Boundaries", "## Before you start", "## Reference"}
	mayName := []string{"colliedesk_whoami", "colliedesk_docs_search"}
	for _, spec := range mcpserver.Catalog() {
		if slices.Contains(mayName, spec.Name) && len(spec.Needs) > 0 {
			t.Errorf("%s needs %v; a skill tells every agent to call it first, so it must work with any token", spec.Name, spec.Needs)
		}
	}
	for _, want := range mayName {
		if !slices.ContainsFunc(mcpserver.Catalog(), func(spec mcpserver.Spec) bool { return spec.Name == want }) {
			t.Errorf("%s is not a tool of the catalog", want)
		}
	}
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
		if lines := strings.Count(string(raw), "\n"); lines > 60 {
			t.Errorf("%s: %d lines; a skill says what it is for and links the guides, at most 60 lines", skill, lines)
		}

		want := append([]string{"# "}, sections...)
		got := heading.FindAllString(body, -1)
		if len(got) != len(want) || !strings.HasPrefix(got[0], "# ") || !slices.Equal(got[1:], sections) {
			t.Errorf("%s: headings %q; want a title, then exactly %q in that order", skill, got, sections)
		}
		seen := map[string]bool{}
		for _, used := range tool.FindAllString(body+fields["description"], -1) {
			if !seen[used] && !slices.Contains(mayName, used) {
				seen[used] = true
				t.Errorf("%s names %s: a skill names no tool but %v; tools, arguments and permissions come from colliedesk_whoami and the tool descriptions, so a change in Colliedesk never leaves a skill wrong", skill, used, mayName)
			}
		}
		_, boundaries, _ := strings.Cut(body, "## Boundaries")
		boundaries, _, _ = strings.Cut(boundaries, "## Before you start")
		if !regexp.MustCompile(`(?m)^1\. \S`).MatchString(boundaries) {
			t.Errorf("%s: its Boundaries section lists nothing; it says what the skill must never do", skill)
		}
		if !strings.Contains(body, "colliedesk_whoami") || !strings.Contains(body, "tools_you_can_use") || !strings.Contains(body, "workspace is locked") {
			t.Errorf("%s must start from colliedesk_whoami and its tools_you_can_use: a person's server may be older than the skill", skill)
		}
		_, reference, _ := strings.Cut(body, "## Reference")
		links := link.FindAllString(body, -1)
		if !link.MatchString(reference) {
			t.Errorf("%s: its Reference section links no guide", skill)
		}
		for _, url := range links {
			page := guide.FindStringSubmatch(url)
			if page == nil {
				t.Errorf("%s links %s; link only the Markdown twin of a user guide, https://colliedesk.com/docs/<page>.md, without an anchor (colliedesk_docs_search serves those guides too)", skill, url)
				continue
			}
			if _, err := os.Stat(filepath.Join("../../../docs/user", page[1]+".md")); err != nil {
				t.Errorf("%s links %s, but docs/user/%s.md does not exist: the page was renamed or removed", skill, url, page[1])
			} else if !strings.Contains(string(index), "]("+page[1]+".md)") {
				t.Errorf("%s links %s, but docs/user/README.md does not list it, so colliedesk.com does not publish it", skill, url)
			}
		}
	}
}

// The plugins connect with OAuth to the fixed public address of Colliedesk Cloud: no token, no
// header and no option in any of their files, so Claude Code (and Cursor) runs the sign-in when /mcp
// answers 401, and the plugin directory reads one public URL. A self-hosted server is added next to
// the plugin with its own address (docs/user/connect-claude-code.md). The plugin has a version, so an
// install changes only when it is raised (TestPluginVersionIsRaisedWithItsFiles in CI).
func TestPluginConnectsWithOAuthToAFixedAddress(t *testing.T) {
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
		Name       string          `json:"name"`
		Version    string          `json:"version"`
		Skills     json.RawMessage `json:"skills"`
		UserConfig json.RawMessage `json:"userConfig"`
	}
	read(filepath.Join(plugin, ".claude-plugin", "plugin.json"), &manifest)
	var cursor struct {
		Variables json.RawMessage `json:"variables"`
	}
	read(filepath.Join(plugin, ".cursor-plugin", "plugin.json"), &cursor)
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	read("../../../.claude-plugin/marketplace.json", &marketplace)
	type servers struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}

	if manifest.Name != "colliedesk" || !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(manifest.Version) {
		t.Errorf("plugin %q, version %q: want colliedesk with a version such as 1.0.0", manifest.Name, manifest.Version)
	}
	if manifest.UserConfig != nil || cursor.Variables != nil {
		t.Errorf("the plugins ask for nothing (userConfig %s, Cursor variables %s): the address is fixed and OAuth signs in", manifest.UserConfig, cursor.Variables)
	}
	if manifest.Skills != nil {
		t.Errorf("plugin.json skills %s: the plugin loads only skills/, the Agent Skills every host shares", manifest.Skills)
	}
	if len(marketplace.Plugins) != 1 || marketplace.Plugins[0].Name != manifest.Name || marketplace.Plugins[0].Source != "./plugins/colliedesk" {
		t.Errorf("marketplace %+v does not offer the plugin", marketplace)
	}
	for _, file := range []string{".mcp.json", "cursor-mcp.json"} {
		var config servers
		raw := read(filepath.Join(plugin, file), &config)
		server, ok := config.MCPServers[docgen.ServerName]
		if len(config.MCPServers) != 1 || !ok || server.URL != docgen.PluginMCPURL || server.Headers != nil {
			t.Errorf("%s: %+v; want only %s at %s with no headers", file, config, docgen.ServerName, docgen.PluginMCPURL)
		}
		if file == ".mcp.json" && server.Type != "http" {
			t.Errorf(".mcp.json: type %q, want http", server.Type)
		}
		if strings.Contains(raw, mcpbridge.TokenPrefix) || strings.Contains(raw, "${") {
			t.Errorf("%s refers to a token or a variable", file)
		}
	}
	// Everything the plugin holds is Markdown, JSON or its icon: it runs no program on the person's computer.
	err := filepath.WalkDir(plugin, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".md" && ext != ".json" && ext != ".png" {
			t.Errorf("%s: the plugin holds only Markdown, JSON and its icon", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
