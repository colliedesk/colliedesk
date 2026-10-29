package ai_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// meteredCallSites are the only places that may call a provider's Complete or Check: the adapters
// themselves (and their fake) and the metered layer. Every other call would leave no usage row.
var meteredCallSites = map[string]bool{
	"internal/ai/meter.go": true,
	// migrationcheck.Check is a package function, not a provider.
	"cmd/migration-check/main.go": true,
}

func TestProvidersAreCalledOnlyThroughMetered(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if strings.HasPrefix(relative, "internal/aiprovider/") || meteredCallSites[relative] {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && ((selector.Sel.Name == "Complete" || selector.Sel.Name == "Check") && len(call.Args) == 2 ||
				selector.Sel.Name == "Stream" && len(call.Args) == 3) {
				t.Errorf("%s calls .%s: call a provider through ai.Service.Metered, so the call leaves a usage row", relative, selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
