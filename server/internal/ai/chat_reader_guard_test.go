package ai

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// helpChatFiles are the files of the help chat's answers: the end user's chat, its prompt and its
// limits. chat-admin.go is the team's side (sessions, usage) and reads no article.
var helpChatFiles = []string{"chat.go", "chat-context.go", "chat-filter.go", "chat-limits.go"}

// helpChatKBReads are the store reads of articles and translations. The help chat runs each one
// inside inHelpChat, as the reader of the chat's help center: row security keeps every article of
// another help center out. docs/internals/workspace-isolation.md lists them.
var helpChatKBReads = []string{
	"AIRetrieve", "ChatKBArticleByIDIn", "ChatKBArticleIDOf", "ChatKBArticles", "ChatKBArticlesByIDIn", "ChatKBArticlesIn",
}

// helpChatAppRoleStoreCalls are the other store calls of the help chat files, each with the reason it
// runs on the app role.
var helpChatAppRoleStoreCalls = map[string]string{
	"PrimaryHelpCenterIDIn": "names the primary help center (made off when the workspace has none) before there is a help center to read as; it reads help_centers only",
}

// helpChatAppRoleFuncs are the help chat functions that open a transaction on the app role, each with
// what it reads or writes there. None reads an article (helpChatKBReads); adding one is a security
// review, and docs/internals/workspace-isolation.md lists the same set.
var helpChatAppRoleFuncs = map[string]string{
	"ChatHandedOff":      "writes the hand-off on the chat session (ai_chat_sessions); the reader has no grant on chats",
	"ChatPrewarm":        "reads when the cache of a set of articles was last warmed (ai_chat_kb_snapshots, by fingerprint and model: no article text)",
	"ChatPrompt":         "reads the token count of a set of articles (ai_chat_kb_snapshots, keyed by the fingerprint of the articles and their versions: no article text)",
	"ChatSessionOf":      "reads the browser's own chat (ai_chat_sessions, ai_chat_turns), found by the hash of its token",
	"ChatTurnOf":         "reads one turn of the browser's own chat and the sources stored with it",
	"CreateChatTransfer": "writes the one-time code that moves the browser's chat to the help center's page (ai_chat_transfers)",
	"EndChat":            "ends the browser's own chat (ai_chat_sessions)",
	"RedeemChatTransfer": "uses a one-time code and moves the chat to the page's token (ai_chat_transfers, ai_chat_sessions)",
	"SetChatFeedback":    "writes the end user's rating of a turn of their own chat (ai_chat_turns)",
	"finishChatTurn":     "writes the answer, its usage and its sources (ai_chat_turns, ai_chat_sources, ai_chat_sessions)",
	"measureChatKB":      "writes the token count of a set of articles (ai_chat_kb_snapshots); the articles themselves are read in inHelpChat",
	"noteChatCache":      "writes when the cache of a set of articles was last read or written (ai_chat_kb_snapshots)",
	"primaryHelpCenter":  "names the primary help center for a chat that names none (store.PrimaryHelpCenterIDIn)",
	"startChatTurn":      "opens or continues the browser's chat and writes the question (ai_chat_sessions, ai_chat_turns) and counts it against the chat's limits; the article the end user reads is looked up in inHelpChat before it",
}

// helpChatRouteCalls are the calls of the help chat's routes (httpapi/help-chat-handlers.go, with
// helpChatAdditions) that do not reach the help chat files above, each with its reason.
var helpChatRouteCalls = map[string]string{
	"kit.Channel":           "reads the help chat channel that serves the help center (channels), which the reader has no grant on; it reads no article",
	"kit.Receive":           "the hand-off opens or joins a conversation through the channel's intake, which the reader must never reach",
	"store.ChannelConfigOf": "reads the channel's settings (its instructions for the AI); it reads no article",
}

// TestHelpChatReadsTheKnowledgeBaseAsTheHelpCenterReader: every read of an article or translation in
// the help chat runs as the reader of the chat's help center (inHelpChat), never on the app role; every
// other store call and every app-role transaction there is reviewed; and every call the help chat's
// routes make is one of those functions or is reviewed.
func TestHelpChatReadsTheKnowledgeBaseAsTheHelpCenterReader(t *testing.T) {
	declared := map[string]bool{}
	for _, file := range parseChatFiles(t, ".", helpChatFiles) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			declared[fn.Name.Name] = true
			visitChatCalls(fn.Body, false, func(call *ast.CallExpr, reader bool) {
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "store" {
					switch {
					case slices.Contains(helpChatKBReads, sel.Sel.Name) && !reader:
						t.Errorf("%s reads the knowledge base (store.%s) on the app role: run it in s.inHelpChat", fn.Name.Name, sel.Sel.Name)
					case !slices.Contains(helpChatKBReads, sel.Sel.Name) && helpChatAppRoleStoreCalls[sel.Sel.Name] == "":
						t.Errorf("%s calls store.%s: add it to helpChatKBReads (run it in s.inHelpChat) or to helpChatAppRoleStoreCalls with its reason", fn.Name.Name, sel.Sel.Name)
					}
					return
				}
				db, ok := sel.X.(*ast.SelectorExpr)
				if !ok || db.Sel.Name != "db" {
					return
				}
				switch sel.Sel.Name {
				case "InHelpChat", "BeginHelpChatTx":
					if fn.Name.Name != "inHelpChat" {
						t.Errorf("%s opens a help chat transaction itself: use s.inHelpChat", fn.Name.Name)
					}
				default:
					if helpChatAppRoleFuncs[fn.Name.Name] == "" {
						t.Errorf("%s runs on the app role (s.db.%s): add it to helpChatAppRoleFuncs with what it reads or writes", fn.Name.Name, sel.Sel.Name)
					}
				}
			})
		}
	}
	for name := range helpChatAppRoleFuncs {
		if !declared[name] {
			t.Errorf("helpChatAppRoleFuncs lists %s, which the help chat files no longer declare", name)
		}
	}
	routes := parseChatFiles(t, filepath.Join("..", "httpapi"), []string{"help-chat-handlers.go"})
	ast.Inspect(routes[0], func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		var owner string
		switch x := sel.X.(type) {
		case *ast.Ident:
			owner = x.Name
		case *ast.SelectorExpr:
			owner = x.Sel.Name
		}
		name := owner + "." + sel.Sel.Name
		switch {
		case owner == "ai" && declared[sel.Sel.Name]:
		case owner == "ai" || owner == "store" || owner == "kit" || owner == "db":
			if helpChatRouteCalls[name] == "" {
				t.Errorf("help-chat-handlers.go calls %s: add it to helpChatRouteCalls with its reason, or read the knowledge base in the help chat files", name)
			}
		}
		return true
	})
}

// visitChatCalls calls f on every call below n; reader is true inside the function passed to
// s.inHelpChat.
func visitChatCalls(n ast.Node, reader bool, f func(*ast.CallExpr, bool)) {
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		f(call, reader)
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "inHelpChat" {
			for _, arg := range call.Args {
				visitChatCalls(arg, true, f)
			}
			return false
		}
		return true
	})
}

func parseChatFiles(t *testing.T, dir string, names []string) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}
