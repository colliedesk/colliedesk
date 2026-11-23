package helpcenter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// readerStoreMethods are the store reads of the public site that run as the help center reader:
// row-level security keeps each statement to the rows one help center shows, and the reader has
// no grant on the inbox, contacts or channels. docs/internals/workspace-isolation.md lists them.
var readerStoreMethods = []string{
	"HelpCenterArticleIn", "HelpCenterArticlesIn", "HelpCenterBrandImage", "HelpCenterCollectionsIn",
	"HelpCenterFile", "HelpCenterImage", "HelpCenterIndex", "HelpCenterLegacyTarget", "HelpCenterRelatedArticlesIn",
	"HelpCenterTranslationIndex", "HelpCenterVideo", "KBArticleVotesVersion", "SearchHelpCenterIn",
	"VoteKBArticle",
}

// appRoleStoreMethods are the reviewed store calls of the public site that stay on the app role,
// each with the reason it must see more than one help center's rows. Adding one is a security
// review; docs/internals/workspace-isolation.md lists the same set.
var appRoleStoreMethods = map[string]string{
	"GetHelpCenterDomain":     "the custom domain record decides which help center a host serves; the reader has no grant on help_center_domains",
	"HelpCenterClaim":         "answers the domain ownership check before any help center is resolved",
	"HelpCenterHost":          "resolves a host name to its workspace and help center, before there is a help center to read as",
	"HelpCenterLegacyItems":   "staff only: the session route that builds the old helpdesk redirect snippet lists every imported article and collection of the workspace, with no help center filter",
	"HelpCenterPreview":       "staff only: the theme preview shows the primary help center's public articles (onPrimary, publicKBArticle), but checks the candidate logo and favicon against the whole media library, trash included, which the reader cannot read",
	"LogKBSearch":             "the per-minute cap counts the whole workspace's searches, and a search written before the workspace has a help center makes the primary one; the reader has no grant on kb_searches, so stored search words stay out of its reach",
	"OpenKBSearch":            "records an open on a search row the reader cannot read; the signed link names the workspace and the row",
	"OpenKBArticleFeedback":   "after the vote is written as the reader, opens a conversation from it; the reader has no grant on conversations, contacts or messages, and the setting lives on the primary row",
	"OpenHelpCenter":          "resolves the help center row; content_version and last_article_number live on the primary row",
	"OpenHelpCenterAt":        "resolves the help center row; content_version and last_article_number live on the primary row",
	"OpenHelpCenterHandle":    "resolves a handle to its help center among the workspace's help centers",
	"OpenHelpCenterWorkspace": "resolves the help center row; content_version and last_article_number live on the primary row",
	"WorkspaceForMailSlug":    "resolves a workspace from its slug before any help center is chosen",
}

// TestThePublicSiteReadsTheKnowledgeBaseAsTheHelpCenterReader: every store method the public site
// calls is either a reader method, whose body runs through helpCenterScoped and never through the
// app role's scope, or a reviewed app-role method.
func TestThePublicSiteReadsTheKnowledgeBaseAsTheHelpCenterReader(t *testing.T) {
	called := storeCalls(t, ".")
	for _, name := range called {
		if !slices.Contains(readerStoreMethods, name) && appRoleStoreMethods[name] == "" {
			t.Errorf("the public site calls Store.%s: add it to readerStoreMethods (run it through helpCenterScoped) "+
				"or to appRoleStoreMethods with the reason it must cross help centers", name)
		}
	}
	bodies := storeBodies(t, filepath.Join("..", "store"))
	for _, name := range readerStoreMethods {
		body, ok := bodies[name]
		if !ok {
			t.Errorf("store method %s not found", name)
			continue
		}
		if !strings.Contains(body, "helpCenterScoped(") {
			t.Errorf("Store.%s does not run as the help center reader (no helpCenterScoped)", name)
		}
		for _, appScope := range []string{"scoped(ctx", "InWorkspace(", "BeginWorkspace("} {
			if strings.Contains(body, appScope) {
				t.Errorf("Store.%s also reads as the app role (%s)", name, appScope)
			}
		}
	}
}

// storeCalls lists the Store methods the non-test files in dir call through a .Store field.
func storeCalls(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	for _, file := range parseDir(t, dir) {
		ast.Inspect(file.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if inner, ok := call.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "Store" && !slices.Contains(names, call.Sel.Name) {
				names = append(names, call.Sel.Name)
			}
			return true
		})
	}
	return names
}

// storeBodies maps each method of *Store in dir to the source text of its body.
func storeBodies(t *testing.T, dir string) map[string]string {
	t.Helper()
	bodies := map[string]string{}
	for _, file := range parseDir(t, dir) {
		for _, decl := range file.ast.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			bodies[fn.Name.Name] = string(file.src[fn.Body.Pos()-file.base : fn.Body.End()-file.base])
		}
	}
	return bodies
}

type parsedFile struct {
	ast  *ast.File
	src  []byte
	base token.Pos
}

func parseDir(t *testing.T, dir string) []parsedFile {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []parsedFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, parsedFile{ast: f, src: src, base: token.Pos(fset.File(f.Pos()).Base())})
	}
	return files
}
