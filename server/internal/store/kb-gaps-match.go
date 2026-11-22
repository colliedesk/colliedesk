package store

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// KBGapMatchThreshold is the score at which two wordings are the same gap, and at which an article
// title answers a gap or a resolved conversation. 1 means the same words; "how to export invoices"
// and "export invoices" score 0.85, "export invoices to excel" and "how to export invoices" 0.8,
// "how to change my password" and "how to export invoices" 0.25.
const KBGapMatchThreshold = 0.6

// kbGapPrefixRunes is how long two words must be before one that starts the other matches it, so
// "invoice" matches "invoices" but "to" does not match "today".
const kbGapPrefixRunes = 4

// kbGapKey is the folded form of a text: the same words in any case or accent give the same key.
func kbGapKey(text string) string {
	return strings.Join(strings.Fields(FoldForAIRetrieval(text)), " ")
}

// kbGapTerms are the words of a key.
func kbGapTerms(key string) []string {
	return strings.FieldsFunc(key, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func kbGapTermsMatch(a, b string) bool {
	if a == b {
		return true
	}
	if utf8.RuneCountInString(a) < kbGapPrefixRunes || utf8.RuneCountInString(b) < kbGapPrefixRunes {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// kbGapScore is how much of two keys' words match, each word weighed by its length: the length of
// the matched words of both over the length of all words of both.
func kbGapScore(a, b string) float64 {
	termsA, termsB := kbGapTerms(a), kbGapTerms(b)
	matched, total := 0, 0
	weigh := func(terms, other []string) {
		for _, term := range terms {
			weight := utf8.RuneCountInString(term)
			total += weight
			for _, candidate := range other {
				if kbGapTermsMatch(term, candidate) {
					matched += weight
					break
				}
			}
		}
	}
	weigh(termsA, termsB)
	weigh(termsB, termsA)
	if total == 0 {
		return 0
	}
	return float64(matched) / float64(total)
}

// kbGapWordsQuery is a text search query that finds the gaps sharing a word, or the start of a long
// word, with key; empty when key has no words.
func kbGapWordsQuery(key string) string {
	var parts []string
	for _, term := range kbGapTerms(key) {
		if runes := []rune(term); len(runes) >= kbGapPrefixRunes {
			parts = append(parts, "'"+string(runes[:kbGapPrefixRunes])+"':*")
		} else {
			parts = append(parts, "'"+term+"'")
		}
	}
	return strings.Join(parts, " | ")
}
