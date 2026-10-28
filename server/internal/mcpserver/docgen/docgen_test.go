package docgen_test

import (
	"encoding/json"
	"flag"
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

var update = flag.Bool("update", false, "rewrite the generated parts of docs/user from the catalog")

const userGuides = "../../../../docs/user"

const connectSnippets = "../../../../web/src/features/settings/connect-snippets.generated.ts"

// generated is every file `make docs-mcp` writes, by path, as the catalog and the asset list make it.
func generated(t *testing.T) map[string]string {
	t.Helper()
	guide := filepath.Join(userGuides, "mcp.md")
	page, err := os.ReadFile(guide)
	if err != nil {
		t.Skipf("docs/user is not next to this module: %v", err)
	}
	data, err := docgen.NewData(mcpserver.Catalog(), "../../../../.github/scripts/client-assets.txt")
	if err != nil {
		t.Fatal(err)
	}
	mcpGuide := string(page)
	for name, body := range map[string]string{
		"tools": docgen.ToolTable(mcpserver.Catalog()), "versions": docgen.Versions(data), "hosts": docgen.HostLinks(),
	} {
		if mcpGuide, err = docgen.Replace(mcpGuide, name, body); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{guide: mcpGuide, connectSnippets: docgen.TypeScript(data)}
	guides, err := docgen.Guides(data)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range guides {
		files[filepath.Join(userGuides, name)] = body
	}
	return files
}

// The tool table and the host guides are generated from the catalog, so a tool, its tier, a
// command or a file name never drifts from what the server and the release ship. The web app's
// Connect panel reads the same commands. `make docs-mcp` rewrites them.
func TestGeneratedDocsMatchTheCatalog(t *testing.T) {
	for path, want := range generated(t) {
		if *update {
			if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		onDisk, err := os.ReadFile(path)
		if err != nil || string(onDisk) != want {
			t.Errorf("%s differs from what the catalog makes: run `make docs-mcp`", filepath.Base(path))
		}
	}
}

// What a person copies from a guide must work: every JSON block parses, every release file named
// is one the release attaches, and no snippet holds a token.
func TestHostGuidesHoldValidJSONAndRealFileNames(t *testing.T) {
	assets := map[string]bool{}
	raw, err := os.ReadFile("../../../../.github/scripts/client-assets.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if fields := strings.Fields(line); len(fields) == 3 && !strings.HasPrefix(fields[0], "#") {
			assets[fields[2]] = true
		}
	}
	block := regexp.MustCompile("(?s)```json\n(.*?)```")
	release := regexp.MustCompile(`releases/latest/download/((?:[A-Za-z0-9_.-]|\$\([a-z -]+\)|\$env:[A-Z_]+)+)`)
	for path, body := range generated(t) {
		if strings.Contains(body, mcpbridge.TokenPrefix) && !strings.Contains(path, "connect-snippets") {
			for _, line := range strings.Split(body, "\n") {
				if strings.Contains(line, mcpbridge.TokenPrefix) && !strings.Contains(line, "`"+mcpbridge.TokenPrefix+"`") {
					t.Errorf("%s holds what looks like a token: %q", filepath.Base(path), line)
				}
			}
		}
		for _, match := range block.FindAllStringSubmatch(body, -1) {
			if !json.Valid([]byte(match[1])) {
				t.Errorf("%s has a JSON block that does not parse:\n%s", filepath.Base(path), match[1])
			}
		}
		for _, match := range release.FindAllStringSubmatch(body, -1) {
			name := strings.TrimRight(match[1], ".")
			switch {
			case assets[name]:
			case strings.Contains(name, "$(uname -s)_$(uname -m)"):
				for _, system := range []string{"Darwin_arm64", "Darwin_x86_64", "Linux_x86_64", "Linux_aarch64"} {
					if !assets[strings.ReplaceAll(name, "$(uname -s)_$(uname -m)", system)] {
						t.Errorf("%s installs %s, which the release does not attach for %s", filepath.Base(path), name, system)
					}
				}
			case strings.Contains(name, "$env:PROCESSOR_ARCHITECTURE"):
				for _, machine := range []string{"AMD64", "ARM64"} {
					if !assets[strings.ReplaceAll(name, "$env:PROCESSOR_ARCHITECTURE", machine)] {
						t.Errorf("%s installs %s, which the release does not attach for %s", filepath.Base(path), name, machine)
					}
				}
			default:
				t.Errorf("%s links %s, which the release does not attach (client-assets.txt)", filepath.Base(path), name)
			}
		}
	}
}

// A guide that names a tool the server does not have sends a reader (or a model reading
// docs_search) after something that does not exist.
func TestUserGuidesNameOnlyRealTools(t *testing.T) {
	tools := map[string]bool{}
	var names []string
	for _, spec := range mcpserver.Catalog() {
		tools[spec.Name] = true
		names = append(names, spec.Name)
	}
	guides, err := filepath.Glob(filepath.Join(userGuides, "*.md"))
	if err != nil || len(guides) == 0 {
		t.Skipf("docs/user is not next to this module: %v", err)
	}
	name := regexp.MustCompile(`colliedesk_[a-z_]*[a-z](_\*)?`) // colliedesk_saved_view_* names a family
	for _, path := range guides {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, found := range name.FindAllString(string(text), -1) {
			if prefix, family := strings.CutSuffix(found, "*"); family {
				if !slices.ContainsFunc(names, func(tool string) bool { return strings.HasPrefix(tool, prefix) }) {
					t.Errorf("%s names %s, and no tool of the catalog starts with %s", filepath.Base(path), found, prefix)
				}
			} else if !tools[found] {
				t.Errorf("%s names %s, which is not a tool of the catalog", filepath.Base(path), found)
			}
		}
	}
}

func TestReplaceNeedsBothMarkers(t *testing.T) {
	if _, err := docgen.Replace("no markers here", "tools", "x"); err == nil {
		t.Error("a page without the markers must be an error, not a silent no-op")
	}
	got, err := docgen.Replace("a\n<!-- generated:tools -->\nold\n<!-- /generated -->\nb\n", "tools", "new\n")
	if err != nil || got != "a\n<!-- generated:tools -->\nnew\n<!-- /generated -->\nb\n" {
		t.Errorf("Replace = %q, %v", got, err)
	}
}
