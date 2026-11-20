package helpcenter

import (
	"strconv"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

// headingIDs names headings as goldmark does ("Reset password" is reset-password, the next one
// reset-password-1, …) so anchors keep working, but remembers the next free suffix of each name:
// goldmark tries every suffix from 1 again, which took 24 s for 25,000 headings of one name.
type headingIDs struct {
	used map[string]bool
	next map[string]int
}

// reservedIDs are the ids the article page gives its own elements: the templates' and those the
// chat (chat.js, surface "article") draws. A heading of one of these names takes the next suffix, so
// "## Main" is main-1 and the skip link still leads to <main id="main">.
var reservedIDs = []string{
	"main", "q-top", "q", "ask-launch", "chatbox", "contents-title", "rail-contents-title", "rail-title",
	"topics", "sections", "articles", "results", "helpful", "helpful-title", "helpful-hint", "helpful-missing",
	"chat", "chat-title", "q-article", "q-article-note", "q-article-count", "w-article", "resume-article",
	"cn-t", "cn-d", "h-article-email", "h-article-name", "h-article-note", "h-article-email-err",
	"h-article-note-hint", "h-article-t", "downloads-title",
}

func newHeadingIDs() *headingIDs {
	ids := &headingIDs{used: map[string]bool{}, next: map[string]int{}}
	for _, id := range reservedIDs {
		ids.used[id] = true
	}
	return ids
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
