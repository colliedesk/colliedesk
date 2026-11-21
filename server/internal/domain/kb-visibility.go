package domain

import (
	"fmt"
	"slices"
	"strings"
)

// Who can see an article or a collection. An article's effective visibility is the most restrictive of
// its own and that of every live collection it is in, their parents included.
const (
	// KBVisibleEveryone is the help center, help chat and the team.
	KBVisibleEveryone = "everyone"
	// KBVisibleHelpChat is help chat, which may quote the article without linking it, and the team.
	KBVisibleHelpChat = "help_chat"
	// KBVisibleAgents is the team only.
	KBVisibleAgents = "agents"
)

// KBVisibilities are the visibilities from the least to the most restrictive.
var KBVisibilities = []string{KBVisibleEveryone, KBVisibleHelpChat, KBVisibleAgents}

// CheckKBVisibility refuses a value that is not one of KBVisibilities.
func CheckKBVisibility(visibility string) error {
	if !slices.Contains(KBVisibilities, visibility) {
		return &InvalidKBError{Message: "visibility is everyone, help_chat (help chat may use it, the help center never shows it) or agents (the team only)."}
	}
	return nil
}

// KBVisibilityRank orders visibilities: 0 everyone, 1 help_chat, 2 agents. Anything else counts as
// agents, as the database's kb_visibility_rank does, so a mistake hides an article rather than showing it.
func KBVisibilityRank(visibility string) int {
	switch visibility {
	case KBVisibleEveryone:
		return 0
	case KBVisibleHelpChat:
		return 1
	}
	return 2
}

// KBVisibilityCollection is the collection that keeps an article more restricted than its own
// visibility, with that collection's own visibility.
type KBVisibilityCollection struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

// KBVisibilityRaiseError refuses a change that would put published articles where more people see them
// while a collection they are in needs approval and no review approved their text as it is.
type KBVisibilityRaiseError struct {
	ArticleIDs []string
}

func (e *KBVisibilityRaiseError) Error() string {
	noun := "articles"
	if len(e.ArticleIDs) == 1 {
		noun = "article"
	}
	return fmt.Sprintf("%d %s would become public without an approved review: %s. Send each for review and approve it first.",
		len(e.ArticleIDs), noun, strings.Join(e.ArticleIDs, ", "))
}
