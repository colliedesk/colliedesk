package helpcenter

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/kbhtml"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdown renders article text the way the editor previews it (GitHub Flavored Markdown). Raw HTML
// in a text goes through rawHTML, which shows allowed video players and the text of anything else.
// Links and images go through publicLinks first.
var markdown = goldmark.New(
	goldmark.WithExtensions(
		// Alignment as an attribute: a style attribute would be refused by the page's CSP.
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough, extension.Linkify, extension.TaskList, extension.Footnote),
	goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithASTTransformers(util.Prioritized(publicLinks{}, 100))),
	// Below the default renderers' 1000, so these functions replace theirs.
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(rawHTML{}, 100))),
)

// plain parses text without changing it, for excerpts.
var plain = goldmark.New(goldmark.WithExtensions(extension.Strikethrough, extension.Table))

// baseKey carries the help center's base path to publicLinks.
var baseKey = parser.NewContextKey()

// kbImageID matches the path an article uses for its own images, as the API writes it (lowercase):
// the public image route looks for exactly this path in the published text.
var kbImageID = regexp.MustCompile(`^` + regexp.QuoteMeta(domain.KBImagePath) + `([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// renderArticle turns an article's Markdown into HTML for a help center at base.
func renderArticle(body, base string) (template.HTML, error) {
	var out bytes.Buffer
	context := parser.NewContext()
	context.Set(baseKey, base)
	if err := markdown.Convert(capNesting([]byte(body)), &out, parser.WithContext(context)); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil //nolint:gosec // goldmark escapes text; rawHTML keeps only kbhtml's output
}

// rawHTML renders each piece of raw HTML through kbhtml, the allowlist the editor preview shares:
// a YouTube or Vimeo player is rebuilt, any other element is left out with its text kept. What is
// stored stays as written.
type rawHTML struct{}

func (rawHTML) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(ast.KindHTMLBlock, func(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		block := node.(*ast.HTMLBlock)
		var raw bytes.Buffer
		lines := block.Lines()
		for i := 0; i < lines.Len(); i++ {
			segment := lines.At(i)
			raw.Write(segment.Value(source))
		}
		if block.HasClosure() {
			raw.Write(block.ClosureLine.Value(source))
		}
		shown, _ := kbhtml.Fragment(raw.String())
		_, _ = writer.WriteString(shown)
		return ast.WalkContinue, nil
	})
	registerer.Register(ast.KindRawHTML, func(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkSkipChildren, nil
		}
		shown, _ := kbhtml.Fragment(string(node.(*ast.RawHTML).Segments.Value(source)))
		_, _ = writer.WriteString(shown)
		return ast.WalkSkipChildren, nil
	})
}

// publicLinks rewrites the links of a text before it is rendered:
//   - an image of the knowledge base points at the help center's public copy;
//   - any other image becomes a link to it, since the page loads images from its own host only;
//   - a link whose scheme is not http, https, mailto or tel keeps its text and loses the link.
type publicLinks struct{}

func (publicLinks) Transform(document *ast.Document, reader text.Reader, context parser.Context) {
	base, _ := context.Get(baseKey).(string)
	source := reader.Source()
	var replace []ast.Node
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Image:
			if match := kbImageID.FindSubmatch(node.Destination); match != nil {
				node.Destination = []byte(base + "/images/" + string(match[1]))
				node.SetAttributeString("loading", []byte("lazy"))
				return ast.WalkSkipChildren, nil
			}
			replace = append(replace, node)
		case *ast.Link:
			if match := kbImageID.FindSubmatch(node.Destination); match != nil {
				node.Destination = []byte(base + "/images/" + string(match[1]))
			} else if !safeURL(string(node.Destination)) {
				replace = append(replace, node)
			}
		case *ast.AutoLink:
			if node.AutoLinkType != ast.AutoLinkEmail && !safeURL(string(node.URL(source))) {
				replace = append(replace, node)
			}
		}
		return ast.WalkContinue, nil
	})
	for _, node := range replace {
		parent := node.Parent()
		if image, ok := node.(*ast.Image); ok && safeURL(string(image.Destination)) {
			link := ast.NewLink()
			link.Destination, link.Title = image.Destination, image.Title
			moveChildren(link, image)
			parent.ReplaceChild(parent, image, link)
			continue
		}
		if link, ok := node.(*ast.AutoLink); ok {
			// An autolink holds its text as a segment, not as children: keep it as plain text.
			parent.ReplaceChild(parent, link, ast.NewString(link.Label(source)))
			continue
		}
		for child := node.FirstChild(); child != nil; {
			next := child.NextSibling()
			parent.InsertBefore(parent, node, child)
			child = next
		}
		parent.RemoveChild(parent, node)
	}
}

func moveChildren(to, from ast.Node) {
	for child := from.FirstChild(); child != nil; {
		next := child.NextSibling()
		to.AppendChild(to, child)
		child = next
	}
}

// safeURL allows relative links and the schemes http, https, mailto and tel. Browsers ignore
// whitespace and control characters inside a scheme, so character references are decoded and those
// characters dropped before it is read.
func safeURL(value string) bool {
	cleaned := strings.Map(func(char rune) rune {
		if char <= ' ' || char == 0x7f {
			return -1
		}
		return char
	}, html.UnescapeString(value))
	colon := strings.IndexByte(cleaned, ':')
	if colon < 0 || strings.ContainsAny(cleaned[:colon], "/?#") {
		return true
	}
	switch strings.ToLower(cleaned[:colon]) {
	case "http", "https", "mailto", "tel":
		return true
	}
	return false
}

// excerpt is the start of an article's text as plain words, at most limit characters, cut at a word.
func excerpt(body string, limit int) string {
	source := capNesting([]byte(body))
	var words strings.Builder
	_ = ast.Walk(plain.Parser().Parse(text.NewReader(source)), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		// Words of one line join as written ("an **admin**." stays "an admin."); blocks and line
		// breaks part with a space.
		if !entering {
			if node.Type() == ast.TypeBlock {
				words.WriteByte(' ')
			}
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Text:
			words.Write(node.Segment.Value(source))
			if node.SoftLineBreak() || node.HardLineBreak() {
				words.WriteByte(' ')
			}
		case *ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock, *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	collapsed := strings.Join(strings.Fields(words.String()), " ")
	if utf8.RuneCountInString(collapsed) <= limit {
		return collapsed
	}
	all := []rune(collapsed)
	runes, cut := all[:limit], limit
	for cut > limit/2 && !unicode.IsSpace(all[cut]) {
		cut--
	}
	if cut <= limit/2 {
		cut = limit
	}
	return strings.TrimRightFunc(string(runes[:cut]), func(char rune) bool { return unicode.IsSpace(char) || unicode.IsPunct(char) }) + "…"
}

// maxNesting is how many quote and list markers a line may open. The parser takes time quadratic in
// the depth a single line opens (100,000 ">" took 2.7 s), and a public page renders on every visit.
const maxNesting = 16

// capNesting shows the markers of a line past maxNesting as text, by escaping the first of them.
// Fenced code is left alone. It returns source itself when nothing changes.
func capNesting(source []byte) []byte {
	var out []byte
	var fence []byte
	for start := 0; start <= len(source); {
		end := bytes.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source)
		} else {
			end += start
		}
		line := source[start:end]
		at := -1
		if marker := fenceOf(line); fence == nil && marker != nil {
			fence = marker
		} else if fence != nil {
			if marker != nil && marker[0] == fence[0] && len(marker) >= len(fence) {
				fence = nil
			}
		} else {
			at = deepMarker(line)
		}
		if at >= 0 && out == nil {
			out = append(make([]byte, 0, len(source)+64), source[:start]...)
		}
		if out != nil {
			if at >= 0 {
				out = append(append(append(out, line[:at]...), '\\'), line[at:]...)
			} else {
				out = append(out, line...)
			}
			if end < len(source) {
				out = append(out, '\n')
			}
		}
		start = end + 1
	}
	if out == nil {
		return source
	}
	return out
}

// fenceOf returns the ``` or ~~~ run that opens or closes a code fence on line, or nil.
func fenceOf(line []byte) []byte {
	trimmed := bytes.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return nil
	}
	run := 0
	for run < len(trimmed) && trimmed[run] == trimmed[0] {
		run++
	}
	if run < 3 {
		return nil
	}
	return trimmed[:run]
}

// deepMarker returns where on line the marker past maxNesting is, or -1: the ">" of a quote, the "-",
// "*" or "+" of a bullet, or the "." or ")" of a number.
func deepMarker(line []byte) int {
	blank := func(at int) bool { return at == len(line) || line[at] == ' ' || line[at] == '\t' }
	for i, depth := 0, 0; ; depth++ {
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
		if i == len(line) {
			return -1
		}
		marker := -1
		switch char := line[i]; {
		case char == '>':
			marker = i
		case (char == '-' || char == '*' || char == '+') && blank(i+1):
			marker = i
		case char >= '0' && char <= '9':
			digits := i
			for digits < len(line) && digits-i < 9 && line[digits] >= '0' && line[digits] <= '9' {
				digits++
			}
			if digits < len(line) && (line[digits] == '.' || line[digits] == ')') && blank(digits+1) {
				marker = digits
			}
		}
		if marker < 0 {
			return -1
		}
		if depth == maxNesting {
			return marker
		}
		i = marker + 1
	}
}
