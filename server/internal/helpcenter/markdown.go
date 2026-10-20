package helpcenter

import (
	"bytes"
	"html"
	"html/template"
	"regexp"
	"slices"
	"strconv"
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
	goldmark.WithParserOptions(parser.WithAutoHeadingID(), parser.WithASTTransformers(util.Prioritized(publicLinks{}, 100), util.Prioritized(hiddenText{}, 200))),
	// Below the default renderers' 1000, so these functions replace theirs.
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(rawHTML{}, 100))),
)

// plain parses text for excerpts, leaving out what the page hides (hiddenText).
var plain = goldmark.New(goldmark.WithExtensions(extension.Strikethrough, extension.Table),
	goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(hiddenText{}, 200))))

// baseKey carries the help center's base path to publicLinks.
var baseKey = parser.NewContextKey()

// kbImageID matches the path an article uses for its own images, as the API writes it (lowercase):
// the public image route looks for exactly this path in the published text.
var kbImageID = regexp.MustCompile(`^` + regexp.QuoteMeta(domain.KBImagePath) + `([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// renderArticle turns an article's Markdown into HTML for a help center at base.
func renderArticle(body, base string) (template.HTML, error) {
	var out bytes.Buffer
	context := parser.NewContext(parser.WithIDs(&headingIDs{used: map[string]bool{}, next: map[string]int{}}))
	context.Set(baseKey, base)
	if err := markdown.Convert(tame([]byte(body)), &out, parser.WithContext(context)); err != nil {
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

// hiddenText removes what inline raw HTML hides, as the editor preview does: the words between an
// inline <script>, <style>, <iframe>… and its closing tag (or the end of the paragraph), and then a
// paragraph left with nothing to show.
type hiddenText struct{}

func (hiddenText) Transform(document *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var empty []ast.Node
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || !hasInline(node) {
			return ast.WalkContinue, nil
		}
		hidden := ""
		shown := false
		for child := node.FirstChild(); child != nil; {
			next := child.NextSibling()
			raw, isRaw := child.(*ast.RawHTML)
			var value string
			if isRaw {
				value = string(raw.Segments.Value(source))
			}
			switch {
			case hidden != "":
				if isRaw && kbhtml.Closes(value, hidden) {
					hidden = ""
				}
				node.RemoveChild(node, child)
			case isRaw:
				hidden = kbhtml.HidesWhatFollows(value)
				if out, _ := kbhtml.Fragment(value); out != "" {
					shown = true
				}
			default:
				if text, ok := child.(*ast.Text); !ok || strings.TrimSpace(string(text.Segment.Value(source))) != "" {
					shown = true
				}
			}
			child = next
		}
		if !shown && node.Kind() == ast.KindParagraph {
			empty = append(empty, node)
		}
		return ast.WalkContinue, nil
	})
	for _, node := range empty {
		node.Parent().RemoveChild(node.Parent(), node)
	}
}

// hasInline reports whether a node holds inline content (a paragraph, a heading, a table cell, a
// link, an emphasis…).
func hasInline(node ast.Node) bool {
	child := node.FirstChild()
	return child != nil && child.Type() == ast.TypeInline
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
	source := tame([]byte(body))
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

// The parser takes time quadratic in two things a single line can hold: the depth of quotes and lists
// it opens (100,000 ">" took 2.7 s) and the links it starts without closing (25,000 "[a](" took
// 3.6 s). A public page renders on every visit, so tame caps both.
const (
	// maxNesting is how many quote and list markers a line may open.
	maxNesting = 16
	// maxOpenLinks is how many link starts ("](") a line may hold whose "(" is not closed before the
	// next one.
	maxOpenLinks = 16
)

// tame shows what a line holds past maxNesting and maxOpenLinks as text, by escaping the first
// marker past the cap and the "]" of every open link start past it. Fenced code is left alone. It returns
// source itself when nothing changes.
func tame(source []byte) []byte {
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
		var escapes []int
		if marker := fenceOf(line); fence == nil && marker != nil {
			fence = marker
		} else if fence != nil {
			if marker != nil && marker[0] == fence[0] && len(marker) >= len(fence) {
				fence = nil
			}
		} else {
			escapes = lineEscapes(line)
		}
		if escapes != nil && out == nil {
			out = append(make([]byte, 0, len(source)+64), source[:start]...)
		}
		if out != nil {
			from := 0
			for _, at := range escapes {
				out = append(append(out, line[from:at]...), '\\')
				from = at
			}
			out = append(out, line[from:]...)
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

// lineEscapes returns where on line a backslash goes, in order, or nil.
func lineEscapes(line []byte) []int {
	var escapes []int
	deep := deepMarker(line)
	if deep >= 0 {
		escapes = append(escapes, deep)
	}
	// A link start whose parentheses do not close before the next one cannot end there, yet the parser
	// looks for its end up to the end of the line each time.
	if bytes.Count(line, []byte("](")) > maxOpenLinks {
		var starts []int
		for at := 0; at+1 < len(line); at++ {
			if line[at] == ']' && line[at+1] == '(' {
				starts = append(starts, at)
			}
		}
		seen := 0
		for index, at := range starts {
			end := len(line)
			if index+1 < len(starts) {
				end = starts[index+1]
			}
			if closes(line[at+2 : end]) {
				continue
			}
			if seen++; seen > maxOpenLinks && at != deep {
				escapes = append(escapes, at)
			}
		}
		slices.Sort(escapes)
	}
	return escapes
}

// closes reports whether the "(" just before destination is closed in it, counting nested
// parentheses and skipping escaped ones, as a link destination is read.
func closes(destination []byte) bool {
	depth := 1
	for at := 0; at < len(destination); at++ {
		switch destination[at] {
		case '\\':
			at++
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return true
			}
		}
	}
	return false
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

// headingIDs names headings as goldmark does ("Reset password" is reset-password, the next one
// reset-password-1, …) so anchors keep working, but remembers the next free suffix of each name:
// goldmark tries every suffix from 1 again, which took 24 s for 25,000 headings of one name.
type headingIDs struct {
	used map[string]bool
	next map[string]int
}

func (ids *headingIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	value = util.TrimRightSpace(util.TrimLeftSpace(value))
	var name []byte
	for i := 0; i < len(value); {
		char := value[i]
		size := util.UTF8Len(char)
		i += int(size)
		switch {
		case size != 1:
		case util.IsAlphaNumeric(char):
			if 'A' <= char && char <= 'Z' {
				char += 'a' - 'A'
			}
			name = append(name, char)
		case util.IsSpace(char) || char == '-' || char == '_':
			name = append(name, '-')
		}
	}
	if len(name) == 0 {
		name = []byte("id")
		if kind == ast.KindHeading {
			name = []byte("heading")
		}
	}
	base := string(name)
	if !ids.used[base] {
		ids.used[base] = true
		return name
	}
	for suffix := max(ids.next[base], 1); ; suffix++ {
		if id := base + "-" + strconv.Itoa(suffix); !ids.used[id] {
			ids.used[id], ids.next[base] = true, suffix+1
			return []byte(id)
		}
	}
}

func (ids *headingIDs) Put(value []byte) {
	ids.used[string(value)] = true
}
