package helpcenter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// routedNames are the variables serve and channelRoute hold a path below a help center's root in.
var routedNames = []string{"sub", "inner", "pageSub"}

// Another help center is served at /<handle> below the primary's root, so every first path segment
// the site routes must be refused as a handle, or a help center could take that page away. The test
// reads the site's own router source: a new case in serve or a new built-in prefix in channelRoute
// fails here until domain.CheckHelpCenterHandle refuses its first segment.
func TestEveryFirstPathSegmentTheSiteRoutesIsRefusedAsAHandle(t *testing.T) {
	files := parsePackage(t)
	consts := stringConsts(files)
	segments := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "serve" && fn.Name.Name != "channelRoute" {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				for _, path := range routedPaths(node, consts) {
					segment, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
					if segment != "" {
						segments[segment] = true
					}
				}
				return true
			})
		}
	}
	for _, want := range []string{"search", "c", "a", "images", "media", "files", "_", "robots.txt", "sitemap.xml", "llms.txt"} {
		if !segments[want] {
			t.Errorf("the router walk did not find /%s; it no longer reads serve and channelRoute", want)
		}
	}
	for segment := range segments {
		if domain.CheckHelpCenterHandle(segment) == nil {
			t.Errorf("the site routes /%s, but %q is accepted as a help center handle; reserve it in domain.CheckHelpCenterHandle", segment, segment)
		}
	}
	// The pages kept from Freshdesk live below /support (ParseLegacyPath).
	if _, ok := ParseLegacyPath("/support/solutions"); !ok {
		t.Fatal("ParseLegacyPath no longer reads /support/solutions")
	}
	if domain.CheckHelpCenterHandle("support") == nil {
		t.Error(`the Freshdesk paths live below /support, but "support" is accepted as a help center handle`)
	}
}

func parsePackage(t *testing.T) []*ast.File {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	set := token.NewFileSet()
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(set, name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
	}
	return files
}

// stringConsts maps each top-level string constant of the package to its value.
func stringConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					if lit, ok := value.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if text, err := strconv.Unquote(lit.Value); err == nil {
							consts[name.Name] = text
						}
					}
				}
			}
		}
	}
	return consts
}

// routedPaths is what node compares a routed path with: the other side of sub == X, the prefix of
// strings.HasPrefix(sub, X), and the elements of a []string list of prefixes.
func routedPaths(node ast.Node, consts map[string]string) []string {
	var exprs []ast.Expr
	switch node := node.(type) {
	case *ast.BinaryExpr:
		if node.Op == token.EQL {
			switch {
			case isRouted(node.X):
				exprs = append(exprs, node.Y)
			case isRouted(node.Y):
				exprs = append(exprs, node.X)
			}
		}
	case *ast.CallExpr:
		if selector, ok := node.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "HasPrefix" && len(node.Args) == 2 && isRouted(node.Args[0]) {
			exprs = append(exprs, node.Args[1])
		}
	case *ast.CompositeLit:
		if array, ok := node.Type.(*ast.ArrayType); ok {
			if element, ok := array.Elt.(*ast.Ident); ok && element.Name == "string" {
				exprs = append(exprs, node.Elts...)
			}
		}
	}
	var paths []string
	for _, expr := range exprs {
		if path, ok := stringValue(expr, consts); ok {
			paths = append(paths, path)
		}
	}
	return paths
}

func isRouted(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && slices.Contains(routedNames, ident.Name)
}

// stringValue is the start of a string expression: a literal, a package constant, or the left side
// of a concatenation (stylePath+name starts with stylePath).
func stringValue(expr ast.Expr, consts map[string]string) (string, bool) {
	switch expr := expr.(type) {
	case *ast.BasicLit:
		if expr.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(expr.Value)
		return text, err == nil
	case *ast.Ident:
		text, ok := consts[expr.Name]
		return text, ok
	case *ast.BinaryExpr:
		if expr.Op == token.ADD {
			return stringValue(expr.X, consts)
		}
	}
	return "", false
}
