package database

import (
	"fmt"
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
	"internal/auth/signup.go: PrivilegeCreateWorkspace",
	"internal/auth/signup.go: PrivilegeSignupSweep",
	"internal/database/migrate.go: PrivilegeMigrate",
	"internal/database/scope.go: PrivilegeRequestLogin",
	"internal/database/scope.go: PrivilegeWorkspaceBySlug",
	"internal/importjobs/people.go: PrivilegeImportJobClaim",
	"internal/importjobs/store.go: PrivilegeImportJobClaim",
	"internal/retention/service.go: PrivilegeRetention",
	"internal/store/ai-retrieval.go: PrivilegeAIRetrievalQueue",
	"internal/store/agent-requests.go: PrivilegeAgentRequestPrune",
	"internal/store/billing-reconcile.go: PrivilegeBillingReconcile",
	"internal/store/email-channels.go: PrivilegeInboundSecret",
	"internal/store/email-inbound.go: PrivilegeReplyToken",
	"internal/store/email-outbox.go: PrivilegeOutboxClaim",
	"internal/store/email-provider-domains.go: PrivilegeProviderDomainSweep",
	"internal/store/email-relay-bounce.go: PrivilegeVerifiedSender",
	"internal/store/kb-images.go: PrivilegeKBPurge",
	"internal/store/kb-media-uploads.go: PrivilegeKBUploadDisk",
	"internal/store/mail-image-cache.go: PrivilegeMailImageSweep",
	"internal/store/mailbox-connections.go: PrivilegeMailboxGrantShared",
	"internal/store/saved-reply-history.go: PrivilegeSavedReplyPurge",
	"internal/store/server-secrets.go: PrivilegeServerSecret",
	"internal/telemetry/usage.go: PrivilegeTelemetry",
}

// poolFiles may import pgxpool. Outside the database package they only open a pool or hand it to a
// constructor that wraps it in a DB; queries never run on the raw pool.
var poolFiles = []string{
	"cmd/collie/billing.go",
	"cmd/collie/commands.go",
	"cmd/collie/email.go",
	"cmd/collie/import.go",
	"cmd/collie/import_helpscout.go",
	"cmd/collie/plan.go",
	"cmd/collie/retention.go",
	"cmd/collie/serve.go",
	"cmd/collie/trial.go",
	"cmd/collie/undo.go",
	"internal/auth/service.go",
	"internal/database/migrate.go",
	"internal/database/scope.go",
	"internal/freshdesk/importer.go",
	"internal/freshdesk/undo.go",
	"internal/helpscout/importer.go",
	"internal/httpapi/api.go",
	"internal/importjobs/worker.go",
	"internal/store/store.go",
}

func TestPrivilegedPathsMatchTheReviewedAllowlist(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var calls, imports, raw []string
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
		if !strings.HasPrefix(relative, "internal/database/") {
			raw = append(raw, rawPoolCalls(fset, relative, file)...)
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
	if len(raw) != 0 {
		t.Errorf("queries on a raw *pgxpool.Pool outside internal/database skip row-level security scoping; use database.DB:\n%s", strings.Join(raw, "\n"))
	}
}

// poolMethods run SQL or hand out a connection; Ping and Close do neither.
var poolMethods = map[string]bool{"Query": true, "QueryRow": true, "Exec": true, "Begin": true, "BeginTx": true,
	"Acquire": true, "AcquireFunc": true, "SendBatch": true, "CopyFrom": true}

// rawPoolCalls reports SQL calls on values the file declares as *pgxpool.Pool (parameters, fields,
// variables, results of pgxpool.New). It is syntactic, which is enough for this codebase's style.
func rawPoolCalls(fset *token.FileSet, name string, file *ast.File) []string {
	isPool := func(expr ast.Expr) bool {
		star, ok := expr.(*ast.StarExpr)
		if !ok {
			return false
		}
		selector, ok := star.X.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := selector.X.(*ast.Ident)
		return ok && pkg.Name == "pgxpool" && selector.Sel.Name == "Pool"
	}
	names := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Field:
			if isPool(node.Type) {
				for _, ident := range node.Names {
					names[ident.Name] = true
				}
			}
		case *ast.ValueSpec:
			if node.Type != nil && isPool(node.Type) {
				for _, ident := range node.Names {
					names[ident.Name] = true
				}
			}
		case *ast.AssignStmt:
			if len(node.Rhs) == 1 && len(node.Lhs) >= 1 {
				if call, ok := node.Rhs[0].(*ast.CallExpr); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
						if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "pgxpool" && strings.HasPrefix(selector.Sel.Name, "New") {
							if ident, ok := node.Lhs[0].(*ast.Ident); ok {
								names[ident.Name] = true
							}
						}
					}
				}
			}
		}
		return true
	})
	var found []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !poolMethods[selector.Sel.Name] {
			return true
		}
		receiver := ""
		switch x := selector.X.(type) {
		case *ast.Ident:
			receiver = x.Name
		case *ast.SelectorExpr:
			receiver = x.Sel.Name
		}
		if names[receiver] {
			var text strings.Builder
			_ = printer.Fprint(&text, fset, selector)
			found = append(found, fmt.Sprintf("%s:%d: %s", name, fset.Position(call.Pos()).Line, text.String()))
		}
		return true
	})
	return found
}

func TestRawPoolCallGuardFindsQueriesOnAPool(t *testing.T) {
	const source = `package sample

import "github.com/jackc/pgx/v5/pgxpool"

type service struct{ pool *pgxpool.Pool }

func (s *service) list(ctx context.Context) { s.pool.Query(ctx, "SELECT 1") }

func open(ctx context.Context, pool *pgxpool.Pool) error {
	other, _ := pgxpool.New(ctx, "")
	other.Begin(ctx)
	_ = pool.Ping(ctx)
	return pool.QueryRow(ctx, "SELECT 1").Scan()
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := rawPoolCalls(fset, "sample.go", file)
	want := []string{"sample.go:7: s.pool.Query", "sample.go:11: other.Begin", "sample.go:13: pool.QueryRow"}
	if !slices.Equal(got, want) {
		t.Fatalf("rawPoolCalls = %q, want %q", got, want)
	}
}
