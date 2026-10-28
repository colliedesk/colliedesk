package mcpserver

import (
	"strings"
	"testing"
)

// Every tool has one sentence for the user guide, and toolDocs keeps none for a tool that is gone.
func TestEveryToolHasADocSentence(t *testing.T) {
	names := map[string]bool{}
	for _, spec := range Catalog() {
		names[spec.Name] = true
		switch {
		case strings.TrimSpace(spec.Doc) == "":
			t.Errorf("%s has no Doc: add one to toolDocs", spec.Name)
		case strings.ContainsAny(spec.Doc, "|\n"):
			t.Errorf("%s: Doc breaks the table row: %q", spec.Name, spec.Doc)
		case strings.Contains(spec.Doc, "Cannot be undone"), strings.HasSuffix(spec.Doc, "."):
			t.Errorf("%s: Doc ends without a period and leaves \"Cannot be undone\" to Destructive: %q", spec.Name, spec.Doc)
		}
	}
	for name := range toolDocs {
		if !names[name] {
			t.Errorf("toolDocs has %s, which is not in the catalog", name)
		}
	}
}
