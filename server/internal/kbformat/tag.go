// Package kbformat reads the formatting HTML a knowledge base article may hold (kbhtml's
// allowlist.json, "formatting"): <u>, <mark>, a coloured <span>, a paragraph or heading wrapped to be
// aligned or indented, a table with merged or coloured cells, and an image in a <figure> that sizes,
// aligns, frames or captions it. Normalize rewrites each such tag of a text to its one canonical
// spelling, and Extension renders them for the help center with its own classes.
// docs/internals/knowledge-base.md shows the forms.
package kbformat

import (
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/kbhtml"
	nethtml "golang.org/x/net/html"
)

var rules = kbhtml.FormattingRules()

// attributeOrder is the order a canonical tag writes its attributes in.
var attributeOrder = []string{"colspan", "rowspan", "data-width", "data-align", "data-indent", "data-color", "data-bg", "data-border"}

// bare are the attributes whose presence is their value: a canonical tag writes them with none.
var bare = []string{"data-border"}

// Tag is one formatting tag: its element, whether it closes it, and the attributes it keeps, in
// attributeOrder.
type Tag struct {
	Name  string
	Close bool
	Attrs []nethtml.Attribute
}

// Attr is the value of the attribute key, "" when the tag has none.
func (t Tag) Attr(key string) string {
	for _, attr := range t.Attrs {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// Has reports whether the tag keeps the attribute key, with a value or a bare one.
func (t Tag) Has(key string) bool {
	return slices.ContainsFunc(t.Attrs, func(attr nethtml.Attribute) bool { return attr.Key == key })
}

// String is the tag's canonical spelling.
func (t Tag) String() string {
	var out strings.Builder
	out.WriteByte('<')
	if t.Close {
		out.WriteByte('/')
	}
	out.WriteString(t.Name)
	for _, attr := range t.Attrs {
		out.WriteString(" " + attr.Key)
		if !slices.Contains(bare, attr.Key) {
			out.WriteString(`="` + attr.Val + `"`)
		}
	}
	out.WriteByte('>')
	return out.String()
}

// IsInline reports whether the tag is one of the inline elements (<u>, <mark>, <span>).
func (t Tag) IsInline() bool { return t.Name == "span" || slices.Contains(rules.Marks, t.Name) }

// IsBlock reports whether the tag is one that wraps a paragraph or a heading.
func (t Tag) IsBlock() bool { return slices.Contains(rules.Blocks, t.Name) }

// IsTable reports whether the tag is part of a table (<table>, <tr>, <th>, <td>).
func (t Tag) IsTable() bool {
	return t.Name == "table" || t.Name == "tr" || t.Name == "th" || t.Name == "td"
}

// IsFigure reports whether the tag is an image's <figure> or its <figcaption>.
func (t Tag) IsFigure() bool { return t.Name == "figure" || t.Name == "figcaption" }

// ParseTag reads raw as exactly one formatting tag, keeping only the attributes and values the
// element allows. It reports false for anything else: text, several tags, another element.
func ParseTag(raw string) (Tag, bool) {
	tags, ok := ParseTags(raw)
	if !ok || len(tags) != 1 {
		return Tag{}, false
	}
	return tags[0], true
}

// ParseTags reads raw as a run of formatting tags parted by whitespace only.
func ParseTags(raw string) ([]Tag, bool) {
	tokens := nethtml.NewTokenizer(strings.NewReader(raw))
	var tags []Tag
	for {
		switch tokens.Next() {
		case nethtml.ErrorToken:
			return tags, len(tags) > 0 && tokens.Err() == io.EOF
		case nethtml.TextToken:
			if strings.TrimSpace(string(tokens.Text())) != "" {
				return nil, false
			}
		case nethtml.StartTagToken, nethtml.EndTagToken:
			token := tokens.Token()
			tag := Tag{Name: token.Data, Close: token.Type == nethtml.EndTagToken}
			if !tag.IsInline() && !tag.IsBlock() && !tag.IsTable() && !tag.IsFigure() {
				return nil, false
			}
			if !tag.Close {
				tag.Attrs = kept(tag.Name, token.Attr)
			}
			tags = append(tags, tag)
		default:
			return nil, false
		}
	}
}

// kept is the attributes of an element name that allowlist.json allows, in attributeOrder.
func kept(name string, attrs []nethtml.Attribute) []nethtml.Attribute {
	var out []nethtml.Attribute
	for _, key := range attributeOrder {
		for _, attr := range attrs {
			if attr.Namespace == "" && attr.Key == key && allowed(name, key, attr.Val) {
				value := attr.Val
				if slices.Contains(bare, key) {
					value = ""
				}
				out = append(out, nethtml.Attribute{Key: key, Val: value})
				break
			}
		}
	}
	return out
}

func allowed(name, key, value string) bool {
	cell := name == "th" || name == "td"
	block := slices.Contains(rules.Blocks, name)
	figure := name == "figure"
	switch key {
	case "data-color":
		return name == "span" && slices.Contains(rules.Colors, value)
	case "data-bg":
		return cell && slices.Contains(rules.Colors, value)
	case "data-align":
		return (cell || block) && slices.Contains(rules.Align, value) ||
			figure && slices.Contains(rules.Figure.Align, value)
	case "data-indent":
		return block && within(value, 1, rules.Indent)
	case "colspan", "rowspan":
		// 1 is every cell's span already, so it is never written.
		return cell && within(value, 2, rules.Span)
	case "data-width":
		return figure && (slices.Contains(rules.Figure.Widths, value) || within(value, 1, rules.Figure.Pixels))
	case "data-border":
		// A border is on or off, so whatever value it was written with means on.
		return figure
	}
	return false
}

// within reports whether value is a plain decimal number from low to high.
func within(value string, low, high int) bool {
	number, err := strconv.Atoi(value)
	return err == nil && strconv.Itoa(number) == value && number >= low && number <= high
}
