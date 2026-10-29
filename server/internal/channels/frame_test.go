package channels_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// shippedTypes are the types this server registers; the frame must not name any of them.
var shippedTypes = []string{"email", "web_form", "help_chat"}

// frameFiles are the shared code every type goes through. A type's name in them means the frame
// treats that type apart, which is what adding a type must never need (docs/internals/channels.md).
var frameFiles = []string{
	"internal/channels/*.go",
	"internal/channels/pipeline/*.go",
	"internal/channels/outbox/*.go",
	"internal/store/channels.go",
	"internal/store/channel-*.go",
	"internal/httpapi/channel-handlers.go",
}

func TestTheFrameNamesNoChannelType(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	checked := 0
	for _, pattern := range frameFiles {
		paths, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			source, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(fset, path, source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			checked++
			tags := map[*ast.BasicLit]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				if field, ok := node.(*ast.Field); ok && field.Tag != nil {
					tags[field.Tag] = true
				}
				return true
			})
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING || tags[literal] {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					return true
				}
				for _, name := range shippedTypes {
					if value == name || strings.Contains(value, "'"+name+"'") || strings.Contains(value, "'"+name+":") {
						t.Errorf("%s names the %s channel type; read it from the type's Definition instead", fset.Position(literal.Pos()), name)
					}
				}
				return true
			})
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d frame files", checked)
	}
}
