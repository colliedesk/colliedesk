package prompt

import (
	"regexp"
	"slices"
	"strings"
)

// Placeholders that stand for a contact identifier in what the model reads.
const (
	phonePlaceholder   = "[phone]"
	emailPlaceholder   = "[email]"
	idPlaceholder      = "[id number]"
	accountPlaceholder = "[account number]"
)

// labelFiller is what may stand between a label and the digits it names (":", "no.", "-").
const labelFiller = `(?:\s*(?:no\.?|number|:|-|#)\s*)*\s*`

const labelStart = `(?:^|[^\p{L}\p{N}])`

// The frame's words for a bank account and an identity document; a channel adds its language's own.
var (
	accountWords = []string{"bank account", "account", "acct", "a/c"}
	idWords      = []string{"passport", "national id", "id card", "id no", "identity card", "tax id", "ssn"}
)

var (
	contactEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)
	ibanPattern         = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){3,7}(?: ?[A-Z0-9]{1,3})?\b`)
	// A phone number: international (+ or 00) or national (a leading 0), 9 to 15 digits in groups.
	phonePattern = regexp.MustCompile(`(?:\(\+\d{1,3}\)|\+\d{1,3}|00\d{1,3}|0)(?:[ .-]?\d){8,13}`)
	groupSplit   = regexp.MustCompile(`[ .-]`)
)

// ContactLabels are a channel's words, in its language, that name a bank account or an identity document
// before its number.
type ContactLabels struct {
	Account, ID []string
	// Patterns are the channel's own shapes of an identifier no label names (a national ID number, a
	// phone number written without its prefix), each masked as its kind.
	Patterns []MaskPattern
}

// MaskPattern is one of a channel's patterns (RE2 syntax, checked when it was saved) and what it masks:
// phone, id_number, account_number or hidden.
type MaskPattern struct {
	Pattern, Mask string
}

// maskPlaceholders are what a channel's pattern puts in place of what it matches.
var maskPlaceholders = map[string]string{"phone": phonePlaceholder, "id_number": idPlaceholder, "account_number": accountPlaceholder, "hidden": hidden}

// contactMasker replaces a person's identifiers with typed placeholders.
type contactMasker struct {
	// A labelled bank account or ID: group 1 the label and filler, group 2 the number.
	account, id *regexp.Regexp
	patterns    []compiledPattern
}

type compiledPattern struct {
	pattern     *regexp.Regexp
	placeholder string
}

func newContactMasker(labels ContactLabels) contactMasker {
	m := contactMasker{
		account: labelled(accountWords, labels.Account, `\d(?: ?\d)*`),
		id:      labelled(idWords, labels.ID, `[A-Z]?\d(?: ?\d)*`),
	}
	for _, p := range labels.Patterns {
		placeholder, known := maskPlaceholders[p.Mask]
		compiled, err := regexp.Compile(p.Pattern)
		// The options were checked when saved; a pattern that no longer compiles masks nothing.
		if known && err == nil {
			m.patterns = append(m.patterns, compiledPattern{compiled, placeholder})
		}
	}
	return m
}

// labelled matches one of the words, what may stand between it and a number, and the number.
func labelled(frame, channel []string, number string) *regexp.Regexp {
	words := append(slices.Clone(frame), channel...)
	slices.SortStableFunc(words, func(a, b string) int { return len(b) - len(a) })
	return regexp.MustCompile(`(?i)(` + labelStart + `(?:` + quoteAll(words) + `)` + labelFiller + `)(` + number + `)`)
}

// mask replaces phone numbers, email addresses, labelled ID and bank account numbers and IBANs. Amounts,
// dates, references and codes stay. It runs after credentials were hidden.
func (m contactMasker) mask(text string) string {
	text = maskLabelled(m.account, text, accountPlaceholder, func(n int) bool { return n >= 6 && n <= 19 })
	text = maskLabelled(m.id, text, idPlaceholder, func(n int) bool { return n >= 6 && n <= 14 })
	text = ibanPattern.ReplaceAllString(text, accountPlaceholder)
	text = contactEmailPattern.ReplaceAllString(text, emailPlaceholder)
	// The channel's own shapes go before the frame's phone numbers, so a number of its own shape is
	// named as its kind.
	for _, p := range m.patterns {
		text = p.pattern.ReplaceAllLiteralString(text, p.placeholder)
	}
	return maskStandalone(phonePattern, text, phonePlaceholder, phoneShaped)
}

// maskLabelled keeps the label and hides the digits after it when their count is one accept allows.
func maskLabelled(pattern *regexp.Regexp, text, placeholder string, accept func(digits int) bool) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if !accept(digitCount(parts[2])) {
			return match
		}
		return parts[1] + placeholder
	})
}

// maskStandalone replaces the matches that are not part of a longer run of digits and that accept
// approves; a match turned down does not hide what starts inside it.
func maskStandalone(pattern *regexp.Regexp, text, placeholder string, accept func(match, rest string) bool) string {
	var out strings.Builder
	last, from := 0, 0
	for from < len(text) {
		span := pattern.FindStringIndex(text[from:])
		if span == nil {
			break
		}
		start, end := from+span[0], from+span[1]
		from = start + 1
		if start > 0 && isDigitByte(text[start-1]) || end < len(text) && isDigitByte(text[end]) {
			continue
		}
		if accept != nil && !accept(text[start:end], text[end:]) {
			continue
		}
		out.WriteString(text[last:start])
		out.WriteString(placeholder)
		last, from = end, end
	}
	out.WriteString(text[last:])
	return out.String()
}

// phoneShaped tells a phone number from a date and a time or a list of small numbers: after its first
// two groups every group has at least three digits, and no colon follows it.
func phoneShaped(match, rest string) bool {
	if strings.HasPrefix(rest, ":") {
		return false
	}
	groups := groupSplit.Split(match, -1)
	for _, group := range groups[min(2, len(groups)):] {
		if len(group) < 3 {
			return false
		}
	}
	return true
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }
