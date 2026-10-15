package mcpserver

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
