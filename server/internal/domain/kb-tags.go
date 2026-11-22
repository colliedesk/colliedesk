package domain

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// An article has at most MaxKBArticleTags tags of at most MaxKBTagLength characters each. Its tags are
// shared by its translations and are not the tags of conversations.
const (
	MaxKBArticleTags = 20
	MaxKBTagLength   = 40
)

// CleanKBTags is a list of article tags as stored: each trimmed, in lower case and with runs of white
// space as one space, without repeats and sorted. An empty tag, a longer one or too many are refused,
// never cut. The answer is never nil, so an empty list clears the tags.
func CleanKBTags(tags []string) ([]string, error) {
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.Join(strings.Fields(tag), " "))
		switch {
		case tag == "":
			return nil, invalidKB("A tag is empty; leave it out to remove it.")
		case utf8.RuneCountInString(tag) > MaxKBTagLength:
			return nil, invalidKB("A tag holds at most %d characters.", MaxKBTagLength)
		}
		cleaned = append(cleaned, tag)
	}
	slices.Sort(cleaned)
	cleaned = slices.Compact(cleaned)
	if len(cleaned) > MaxKBArticleTags {
		return nil, invalidKB("An article has at most %d tags.", MaxKBArticleTags)
	}
	return cleaned, nil
}
