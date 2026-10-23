package docgen_test

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver/docgen"
)

var update = flag.Bool("update", false, "rewrite the generated parts of docs/user from the catalog")

const userGuides = "../../../../docs/user"

// The tool table of the MCP guide is generated from the catalog, so a tool, its tier or its
// "Cannot be undone" never drifts from what the server registers. `make docs-mcp` rewrites it.
func TestGeneratedDocsMatchTheCatalog(t *testing.T) {
	guide := filepath.Join(userGuides, "mcp.md")
	onDisk, err := os.ReadFile(guide)
	if err != nil {
		t.Skipf("docs/user is not next to this module: %v", err)
	}
	want, err := docgen.Replace(string(onDisk), "tools", docgen.ToolTable(mcpserver.Catalog()))
	if err != nil {
		t.Fatal(err)
	}
	if *update {
		for _, path := range []string{guide, "../userdocs/mcp.md"} {
			if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	if string(onDisk) != want {
		t.Errorf("docs/user/mcp.md differs from the tool catalog: run `make docs-mcp`")
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
