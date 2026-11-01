package prompt

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// issuedLabelPattern finds a label in the model's answer: "Customer A", "Customer 27", "Team 3".
var issuedLabelPattern = regexp.MustCompile(`Customer (?:[A-Z]|\d+)|Team \d+`)

// people issues the labels that stand for names in one prompt: customers are "Customer A", "Customer
// B", ..., the team (people and the channel's own bot) "Team 1", "Team 2", ... Labels are stable within
// the prompt and mean nothing outside it.
type people struct {
	names     map[string]string // label -> the real name, for the labels that have one
	issued    []string
	byKey     map[string]string // person key -> label
	customers int
	team      int
}

func newPeople() *people {
	return &people{names: map[string]string{}, byKey: map[string]string{}}
}

// label is the label of a person known by key (their identity on the channel) and name.
func (p *people) label(key, name string, team bool) string {
	if key == "" {
		key = "name:" + name
	}
	label, ok := p.byKey[key]
	if !ok {
		if team {
			p.team++
			label = fmt.Sprintf("Team %d", p.team)
		} else {
			p.customers++
			label = customerLabel(p.customers)
		}
		p.byKey[key] = label
		p.issued = append(p.issued, label)
	}
	if name = strings.TrimSpace(name); name != "" && p.names[label] == "" {
		p.names[label] = name
	}
	return label
}

func customerLabel(n int) string {
	if n <= 26 {
		return "Customer " + string(rune('A'+n-1))
	}
	return fmt.Sprintf("Customer %d", n)
}

// minNameLetters is how many letters a name needs before it is replaced in free text: shorter ones
// would rewrite ordinary words.
const minNameLetters = 3

// anonymiser puts the label in place of every whole-word occurrence of a name that was issued one, the
// longest name first.
func (p *people) anonymiser() func(string) string {
	type entry struct{ name, label string }
	var entries []entry
	seen := map[string]bool{}
	for _, label := range p.issued {
		name := p.names[label]
		letters := 0
		for _, r := range name {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters < minNameLetters || seen[name] {
			continue
		}
		seen[name] = true
		entries = append(entries, entry{name, label})
	}
	if len(entries) == 0 {
		return func(text string) string { return text }
	}
	slices.SortStableFunc(entries, func(a, b entry) int { return len(b.name) - len(a.name) })
	labelOf := make(map[string]string, len(entries))
	quoted := make([]string, len(entries))
	for i, e := range entries {
		labelOf[e.name], quoted[i] = e.label, regexp.QuoteMeta(e.name)
	}
	pattern := regexp.MustCompile(strings.Join(quoted, "|"))
	return func(text string) string {
		return replaceWords(text, pattern, func(found string) (string, bool) { label, ok := labelOf[found]; return label, ok })
	}
}

// Restore puts the real names back in place of the labels this prompt issued, so a subject or a reason
// the model wrote reads as a person would. A label it did not issue stays as written.
func (c Context) Restore(text string) string {
	if len(c.names) == 0 {
		return text
	}
	return replaceWords(text, issuedLabelPattern, func(found string) (string, bool) { name, ok := c.names[found]; return name, ok })
}

// replaceWords replaces the matches of pattern that stand as whole words and that replacement approves.
func replaceWords(text string, pattern *regexp.Regexp, replacement func(found string) (string, bool)) string {
	var out strings.Builder
	last := 0
	for _, span := range pattern.FindAllStringIndex(text, -1) {
		start, end := span[0], span[1]
		with, ok := replacement(text[start:end])
		if !ok || startsInsideWord(text, start) || endsInsideWord(text, end) {
			continue
		}
		out.WriteString(text[last:start])
		out.WriteString(with)
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

func startsInsideWord(text string, start int) bool {
	r, _ := utf8.DecodeLastRuneInString(text[:start])
	return start > 0 && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func endsInsideWord(text string, end int) bool {
	r, _ := utf8.DecodeRuneInString(text[end:])
	return end < len(text) && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
