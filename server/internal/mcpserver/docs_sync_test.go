package mcpserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The embedded guides are a copy of docs/user. A guide edited in one place only would answer
// docs_search with stale text.
func TestEmbeddedUserGuidesMatchTheDocsDirectory(t *testing.T) {
	const source = "../../../docs/user"
	if _, err := os.Stat(source); err != nil {
		t.Skipf("docs/user is not next to this module: %v", err)
	}
	onDisk, err := filepath.Glob(source + "/*.md")
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := fs.Glob(userDocs, "userdocs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	var diskNames, embeddedNames []string
	for _, path := range onDisk {
		diskNames = append(diskNames, filepath.Base(path))
	}
	for _, path := range embedded {
		embeddedNames = append(embeddedNames, filepath.Base(path))
	}
	if !slices.Equal(diskNames, embeddedNames) {
		t.Fatalf("docs/user has %v, the server embeds %v: run `make sync-user-docs`", diskNames, embeddedNames)
	}
	for _, path := range onDisk {
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := fs.ReadFile(userDocs, "userdocs/"+filepath.Base(path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs from its embedded copy: run `make sync-user-docs`", filepath.Base(path))
		}
	}
}

// docs_search quotes the guides; the markers around a generated table are not text to quote.
func TestGuideSectionsLeaveOutGeneratedMarkers(t *testing.T) {
	sections := splitGuide("mcp", "# MCP\n\n## Tools\n\n<!-- generated:tools -->\n| a | b |\n<!-- /generated -->\n")
	for _, section := range sections {
		if strings.Contains(section.text, "<!--") || strings.Contains(section.text, "generated") {
			t.Errorf("section %q keeps a marker: %q", section.heading, section.text)
		}
	}
	if len(sections) != 1 || !strings.Contains(sections[0].text, "| a | b |") {
		t.Errorf("sections = %+v, want the table under Tools", sections)
	}
}
