// Package docgen writes the parts of the user guide that come from the tool catalog. The output is
// checked in; a test regenerates it in memory and fails when the files drift (make docs-mcp).
package docgen

import (
	"fmt"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

// ToolTable is the Markdown table of every workspace tool (Path), in catalog order.
func ToolTable(specs []mcpserver.Spec) string {
	return table(WorkspaceTools(specs))
}

// OperatorToolTable is the table of the operator tools (OperatorPath).
func OperatorToolTable(specs []mcpserver.Spec) string {
	var operator []mcpserver.Spec
	for _, spec := range specs {
		if spec.Tier == auth.ScopeOperator {
			operator = append(operator, spec)
		}
	}
	return table(operator)
}

// WorkspaceTools leaves out the operator tools, which a workspace token never sees.
func WorkspaceTools(specs []mcpserver.Spec) []mcpserver.Spec {
	var workspace []mcpserver.Spec
	for _, spec := range specs {
		if spec.Tier != auth.ScopeOperator {
			workspace = append(workspace, spec)
		}
	}
	return workspace
}

func table(specs []mcpserver.Spec) string {
	var b strings.Builder
	b.WriteString("| Tool | What it does | Tier |\n|---|---|---|\n")
	for _, spec := range specs {
		doc := spec.Doc
		if spec.Destructive {
			doc += ". Cannot be undone"
		}
		tier := "`" + spec.Tier + "`"
		if spec.Tier == "any" {
			tier = "any"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", spec.Name, doc, tier)
	}
	return b.String()
}

// Replace puts body between the lines <!-- generated:name --> and <!-- /generated --> of page.
func Replace(page, name, body string) (string, error) {
	start, end := "<!-- generated:"+name+" -->\n", "<!-- /generated -->"
	before, rest, found := strings.Cut(page, start)
	if !found {
		return "", fmt.Errorf("the page has no %q line", strings.TrimSpace(start))
	}
	_, after, found := strings.Cut(rest, end)
	if !found {
		return "", fmt.Errorf("the page has no %q line after %q", end, strings.TrimSpace(start))
	}
	return before + start + body + end + after, nil
}
