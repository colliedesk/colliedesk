package store

import "strings"

func escapeLikePattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// maxSearchWords bounds a conversation search: each word reads the conversation's messages once more,
// and words past the eighth rarely narrow a list a person reads.
const maxSearchWords = 8

// searchWords are the words of a search, each once whatever its case, the first maxSearchWords of them.
func searchWords(search string) []string {
	var words []string
	seen := map[string]bool{}
	for _, word := range strings.Fields(search) {
		if key := strings.ToLower(word); !seen[key] && len(words) < maxSearchWords {
			seen[key] = true
			words = append(words, word)
		}
	}
	return words
}
