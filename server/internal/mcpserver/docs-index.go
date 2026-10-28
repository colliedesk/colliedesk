package mcpserver

import (
	"embed"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// userDocs is a copy of docs/user (go:embed cannot reach outside the module). The test
// TestEmbeddedUserGuidesMatchTheDocsDirectory fails when they differ; `make sync-user-docs` copies.
//
//go:embed userdocs/*.md
var userDocs embed.FS

// docSection is one heading of a guide with the text under it, ready to be scored.
type docSection struct {
	page, title, heading, text string
	headingTerms, bodyTerms    map[string]int
	titleTerms                 map[string]bool
}

var (
	docIndexOnce sync.Once
	docIndex     []docSection
)

func docSections() []docSection {
	docIndexOnce.Do(func() { docIndex = buildDocIndex(userDocs, "userdocs") })
	return docIndex
}

func buildDocIndex(files fs.FS, dir string) []docSection {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		panic("read embedded user guides: " + err.Error())
	}
	var sections []docSection
	for _, entry := range entries {
		if entry.Name() == "README.md" {
			continue // the index of the guides, not a guide
		}
		data, err := fs.ReadFile(files, dir+"/"+entry.Name())
		if err != nil {
			panic("read embedded user guide " + entry.Name() + ": " + err.Error())
		}
		sections = append(sections, splitGuide(strings.TrimSuffix(entry.Name(), ".md"), string(data))...)
	}
	return sections
}

// splitGuide cuts a guide at its headings. Lines inside code fences never start a section.
func splitGuide(page, markdown string) []docSection {
	var sections []docSection
	title, heading := page, ""
	var body []string
	inFence := false
	flush := func() {
		text := strings.TrimSpace(strings.Join(body, "\n"))
		if text != "" {
			sections = append(sections, newSection(page, title, heading, text))
		}
		body = nil
	}
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "<!--") && strings.HasSuffix(line, "-->") {
			continue // the markers around generated parts are not text to search or quote
		}
		level := headingLevel(line)
		if inFence || level == 0 {
			body = append(body, line)
			continue
		}
		flush()
		heading = strings.TrimSpace(line[level:])
		if level == 1 {
			title = heading
		}
	}
	flush()
	return sections
}

func headingLevel(line string) int {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || len(line) <= level || line[level] != ' ' {
		return 0
	}
	return level
}

func newSection(page, title, heading, text string) docSection {
	section := docSection{page: page, title: title, heading: heading, text: text,
		headingTerms: countTerms(heading), bodyTerms: countTerms(text), titleTerms: map[string]bool{}}
	for term := range countTerms(title) {
		section.titleTerms[term] = true
	}
	return section
}

var stopWords = map[string]bool{"a": true, "an": true, "the": true, "of": true, "to": true, "in": true, "on": true, "for": true, "and": true, "or": true,
	"is": true, "are": true, "it": true, "how": true, "do": true, "i": true, "my": true, "with": true, "from": true, "as": true, "at": true, "by": true, "be": true}

// countTerms splits text into lower-case word stems and counts them. Stems are crude on purpose:
// they only have to make "invites", "invited" and "inviting" meet.
func countTerms(text string) map[string]int {
	counts := map[string]int{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !stopWords[word] {
			counts[stem(word)]++
		}
	}
	return counts
}

func stem(word string) string {
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if len(word) > len(suffix)+3 && strings.HasSuffix(word, suffix) {
			word = strings.TrimSuffix(word, suffix)
			break
		}
	}
	if len(word) > 4 && strings.HasSuffix(word, "e") {
		word = strings.TrimSuffix(word, "e")
	}
	return word
}

type docHit struct {
	section docSection
	score   int
}

// searchDocs ranks sections for the query: a term in the heading counts most, then the guide title,
// then how often it appears in the text. Ties keep the order of the guides, so a query always ranks
// the same way.
func searchDocs(sections []docSection, query string) []docHit {
	terms := slices.Sorted(func(yield func(string) bool) {
		for term := range countTerms(query) {
			if !yield(term) {
				return
			}
		}
	})
	var hits []docHit
	for _, section := range sections {
		score := 0
		for _, term := range terms {
			if section.headingTerms[term] > 0 {
				score += 6
			}
			if section.titleTerms[term] {
				score += 3
			}
			if count := section.bodyTerms[term]; count > 0 {
				score += 4 + 2*min(count-1, 4)
			}
		}
		if score > 0 {
			hits = append(hits, docHit{section, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	return hits
}

// excerpt returns about 400 characters of the section around its first matching word.
func excerpt(section docSection, query string) string {
	text := strings.Join(strings.Fields(section.text), " ")
	start := 0
	lower := strings.ToLower(text)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if index := strings.Index(lower, word); index >= 0 && !stopWords[word] {
			start = max(0, index-80)
			break
		}
	}
	runes := []rune(text)
	if start > len(runes) {
		start = 0
	}
	end := min(len(runes), start+400)
	result := string(runes[start:end])
	if start > 0 {
		result = "…" + result
	}
	if end < len(runes) {
		result += "…"
	}
	return result
}
