package database

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// privilegedCallSites is the reviewed allowlist of code that runs outside a workspace scope.
// Adding a line here is a security review: say why the path must cross workspaces and keep it to
// the lookup that resolves a workspace. docs/internals/workspace-isolation.md lists the same set.
var privilegedCallSites = []string{
	"internal/auth/cloud.go: PrivilegeCloudIdentity",
	"internal/auth/links.go: PrivilegeLinkToken",
	"internal/auth/service.go: PrivilegeCreateWorkspace",
	"internal/auth/sessions.go: PrivilegeSessionToken",
	"internal/database/migrate.go: PrivilegeMigrate",
	"internal/database/scope.go: PrivilegeWorkspaceBySlug",
	"internal/store/email-channels.go: PrivilegeInboundSecret",
	"internal/store/email-inbound.go: PrivilegeReplyToken",
	"internal/store/email-outbox.go: PrivilegeOutboxClaim",
}

// poolFiles may import pgxpool. Outside the database package they only open a pool or hand it to a
// constructor that wraps it in a DB; queries never run on the raw pool.
var poolFiles = []string{
	"cmd/colliedesk/commands.go",
	"cmd/colliedesk/import.go",
	"cmd/colliedesk/serve.go",
	"cmd/colliedesk/undo.go",
	"internal/auth/service.go",
	"internal/database/migrate.go",
	"internal/database/scope.go",
	"internal/freshdesk/importer.go",
	"internal/freshdesk/undo.go",
	"internal/httpapi/api.go",
	"internal/store/store.go",
}

func TestPrivilegedPathsMatchTheReviewedAllowlist(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var calls, imports []string
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == "node_modules" || name == "testdata" || strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		for _, spec := range file.Imports {
			if value, _ := strconv.Unquote(spec.Path.Value); value == "github.com/jackc/pgx/v5/pgxpool" {
				imports = append(imports, relative)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Privileged" || len(call.Args) < 2 {
				return true
			}
			var reason strings.Builder
			_ = printer.Fprint(&reason, fset, call.Args[1])
			calls = append(calls, relative+": "+strings.TrimPrefix(reason.String(), "database."))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(calls)
	calls = slices.Compact(calls)
	slices.Sort(imports)
	if !slices.Equal(calls, privilegedCallSites) {
		t.Errorf("privileged call sites changed; review them and update privilegedCallSites and docs/internals/workspace-isolation.md\n got: %q\nwant: %q", calls, privilegedCallSites)
	}
	if !slices.Equal(imports, poolFiles) {
		t.Errorf("files importing pgxpool changed; queries must go through database.DB, not the raw pool\n got: %q\nwant: %q", imports, poolFiles)
	}
}
