package helpcenter

import (
	"encoding/json"
	"html/template"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/yuin/goldmark/ast"
)

// Structured data (schema.org JSON-LD) for search engines and AI assistants: a BreadcrumbList on every
// page, an Article on an article page, and a FAQPage when the article is a list of questions
// (questionList). Each block is JSON written by encoding/json, which escapes <, >, &, U+2028 and
// U+2029, so nothing an author writes can end the script element; the page's CSP runs no script, and
// a JSON-LD block is data, never run.

const (
	// faqAnswerChars bounds the text of one answer in a FAQPage.
	faqAnswerChars = 1000
	// headlineChars bounds an Article's headline, as search engines show it.
	headlineChars = 110
)

// faqItem is one question of an article that is a list of questions, and its answer as plain words.
type faqItem struct {
	question, answer string
}

// ldBlock is one JSON-LD block of a page.
func ldBlock(kind string, fields map[string]any) template.JS {
	fields["@context"], fields["@type"] = "https://schema.org", kind
	data, err := json.Marshal(fields)
	if err != nil {
		return ""
	}
	return template.JS(data) //nolint:gosec // encoding/json escapes every character that could leave the script element
}

// absolute is a link of the help center's own (a path) as an absolute address on the host it serves on.
func (v view) absolute(path string) string {
	parsed, err := url.Parse(v.address)
	if err != nil {
		return path
	}
	return parsed.Scheme + "://" + parsed.Host + path
}

// breadcrumbs is the BreadcrumbList of a page: its trail and the page itself.
func (v view) breadcrumbs(trail []link, current link) template.JS {
	items := make([]map[string]any, 0, len(trail)+1)
	for index, crumb := range append(append([]link{}, trail...), current) {
		items = append(items, map[string]any{"@type": "ListItem", "position": index + 1, "name": crumb.Name, "item": v.absolute(crumb.URL)})
	}
	return ldBlock("BreadcrumbList", map[string]any{"itemListElement": items})
}

// articleData is the Article of an article page whose canonical address is canonical.
func (v view) articleData(article domain.PublicKBArticle, canonical, description string) template.JS {
	headline := article.Title
	if utf8.RuneCountInString(headline) > headlineChars {
		headline = string([]rune(headline)[:headlineChars-1]) + "…"
	}
	fields := map[string]any{"headline": headline, "url": canonical, "mainEntityOfPage": canonical, "inLanguage": v.site.Locale,
		"dateModified": article.UpdatedAt.UTC().Format(time.RFC3339), "publisher": map[string]any{"@type": "Organization", "name": v.title()}}
	if description != "" {
		fields["description"] = description
	}
	if article.PublishedAt != nil {
		fields["datePublished"] = article.PublishedAt.UTC().Format(time.RFC3339)
	}
	return ldBlock("Article", fields)
}

// faqData is the FAQPage of an article that is a list of questions.
func faqData(items []faqItem) template.JS {
	questions := make([]map[string]any, len(items))
	for index, item := range items {
		questions[index] = map[string]any{"@type": "Question", "name": item.question, "acceptedAnswer": map[string]any{"@type": "Answer", "text": item.answer}}
	}
	return ldBlock("FAQPage", map[string]any{"mainEntity": questions})
}

// questionList reads an article as a list of questions when it is one: at least two headings of its
// highest level, every one of them ending with "?", each followed by an answer before the next one.
// Text before the first of them is an introduction and is left out. Anything else is not a list of
// questions and gets no FAQPage. document is the page's own parse of source.
func questionList(document ast.Node, source []byte) []faqItem {
	top := 0
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		if heading, ok := node.(*ast.Heading); ok && (top == 0 || heading.Level < top) {
			top = heading.Level
		}
	}
	if top == 0 {
		return nil
	}
	var items []faqItem
	var answer strings.Builder
	finish := func() bool {
		if len(items) == 0 {
			return true
		}
		words := strings.Join(strings.Fields(answer.String()), " ")
		if words == "" {
			return false
		}
		if utf8.RuneCountInString(words) > faqAnswerChars {
			words = string([]rune(words)[:faqAnswerChars-1]) + "…"
		}
		items[len(items)-1].answer = words
		return true
	}
	for node := document.FirstChild(); node != nil; node = node.NextSibling() {
		if heading, ok := node.(*ast.Heading); ok && heading.Level == top {
			if !finish() {
				return nil
			}
			question := strings.Join(strings.Fields(plainText(heading, source)), " ")
			if !strings.HasSuffix(question, "?") {
				return nil
			}
			items = append(items, faqItem{question: question})
			answer.Reset()
			continue
		}
		if len(items) > 0 {
			answer.WriteString(plainText(node, source) + " ")
		}
	}
	if len(items) < 2 || !finish() {
		return nil
	}
	return items
}

// plainText is the words of a block as excerpt reads them: code and raw HTML left out.
func plainText(block ast.Node, source []byte) string {
	var words strings.Builder
	_ = ast.Walk(block, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
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
	return words.String()
}
