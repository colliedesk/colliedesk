package helpcenter

import (
	"bytes"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	goldhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// highlightLimit is the longest code block coloured; a longer one shows as plain text. Lexing takes
// about 5 ms per KB for Go or JavaScript, so a block at the limit renders in about 40 ms, and an
// article with many keeps renderWait as its bound.
const highlightLimit = 8_000

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
	if !highlight(w, string(language), code.String()) {
		goldhtml.DefaultWriter.RawWrite(w, code.Bytes())
	}
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

// highlight writes code as token spans, or nothing and false when it is not coloured: no language,
// one chroma does not know, or code over highlightLimit.
func highlight(w util.BufWriter, language, code string) bool {
	if language == "" || len(code) > highlightLimit {
		return false
	}
	lexer := lexers.Get(language)
	if lexer == nil {
		return false
	}
	tokens, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return false
	}
	for _, token := range tokens.Tokens() {
		class := tokenClass(token.Type)
		if class != "" {
			_, _ = w.WriteString(`<span class="` + class + `">`)
		}
		goldhtml.DefaultWriter.RawWrite(w, []byte(token.Value))
		if class != "" {
			_, _ = w.WriteString("</span>")
		}
	}
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
