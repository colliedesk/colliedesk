package mailimage_test

import (
	"html"
	"regexp"
	"testing"
)

func regexpAll(t *testing.T, text, pattern string) []string {
	t.Helper()
	var found []string
	for _, match := range regexp.MustCompile(pattern).FindAllStringSubmatch(text, -1) {
		found = append(found, match[1])
	}
	return found
}

func htmlUnescape(s string) string { return html.UnescapeString(s) }
