package helpcenter

import (
	"bufio"
	"bytes"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// highlightLimit is the most code an article colours, in characters: blocks are coloured in order
// while their total fits, and one that would pass it shows as plain text. Lexing takes up to about
// 3 ms per KB for the common languages, and colouring multiplies a block's HTML by up to 18, so the
// limit bounds both the render (about 30 ms, which highlightTime also caps) and the page (about
// 180 KB more).
const highlightLimit = 10_000

// highlightTime is the most time an article spends lexing its code blocks.
// Some lexers are quadratic on crafted code (8,000 newlines in C or Java, colons in YAML take
// seconds), so the block lexing when it runs out and every later block show as plain text. A var so
// a test can spend it.
var highlightTime = 30 * time.Millisecond

// codeHTML writes a fenced code block as goldmark does, with the code of a language chroma knows
// split into spans of token classes (chroma's short names: kd, s, c, …) that site.css colours. Only
// classes: an inline style would be refused by the page's CSP (style-src 'self').
type codeHTML struct{}

func (codeHTML) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, renderFencedCode)
}

func renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block := node.(*ast.FencedCodeBlock)
	language := block.Language(source)
	_, _ = w.WriteString("<pre><code")
	if language != nil {
		_, _ = w.WriteString(` class="language-`)
		goldhtml.DefaultWriter.Write(w, language)
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	var code bytes.Buffer
	lines := block.Lines()
	for i := range lines.Len() {
		line := lines.At(i)
		code.Write(line.Value(source))
	}
	colored := false
	if page, ok := w.(*pageWriter); ok {
		colored = page.code.highlight(w, string(language), code.String())
	}
	if !colored {
		goldhtml.DefaultWriter.RawWrite(w, code.Bytes())
	}
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

// codeBudget is what an article has used for colouring code: characters and lexing time.
type codeBudget struct {
	used  int
	lexed time.Duration
	spent bool
}

// highlight writes code as token spans, or nothing and false when it is not coloured: a language
// not in codeLexers, code that passes the article's limit, or lexing that runs out of its time.
func (budget *codeBudget) highlight(w util.BufWriter, language, code string) bool {
	lexer := codeLexers()[strings.ToLower(language)]
	if lexer == nil || budget.spent || budget.used+len(code) > highlightLimit {
		return false
	}
	budget.used += len(code)
	// Tokenise compiles a lexer's rules on its first use in the process, so only the tokens are timed.
	tokens, err := lexer.Tokenise(nil, code)
	if err != nil {
		return false
	}
	// The raw tokens, not chroma.Coalesce's: one Coalesce call can lex many tokens, so the time is
	// checked per token. Adjacent tokens of one class are merged into a span here instead.
	var spans bytes.Buffer
	out := bufio.NewWriter(&spans)
	class, run := "", strings.Builder{}
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if class != "" {
			_, _ = out.WriteString(`<span class="` + class + `">`)
		}
		goldhtml.DefaultWriter.RawWrite(out, []byte(run.String()))
		if class != "" {
			_, _ = out.WriteString("</span>")
		}
		run.Reset()
	}
	for {
		if budget.lexed >= highlightTime {
			budget.spent = true
			return false
		}
		started := time.Now()
		token := tokens()
		budget.lexed += time.Since(started)
		if token == chroma.EOF {
			break
		}
		if token.Value == "" {
			continue
		}
		if next := tokenClass(token.Type); next != class {
			flush()
			class = next
		}
		run.WriteString(token.Value)
	}
	flush()
	_ = out.Flush()
	_, _ = w.Write(spans.Bytes())
	return true
}

// tokenClass is chroma's class for a token type, or its nearest parent's; plain text has none.
func tokenClass(kind chroma.TokenType) string {
	for ; kind != 0; kind = kind.Parent() {
		if class, ok := chroma.StandardTypes[kind]; ok {
			return class
		}
	}
	return ""
}
