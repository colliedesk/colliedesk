package domain

import (
	"fmt"
	"slices"
	"strings"
)

// KBListColumnNames are the columns of the knowledge base article list a member can show after
// Article, which is always there.
var KBListColumnNames = []string{"status", "collection", "author", "tags", "languages", "created", "updated", "votes"}

// kbListDefaultColumns is what a member sees before choosing.
var kbListDefaultColumns = []string{"status", "author", "updated"}

// KBListColumns is a member's columns of the knowledge base article list, in order. Default is true
// while they have not chosen.
type KBListColumns struct {
	Columns []string `json:"columns"`
	Default bool     `json:"default"`
}

// EffectiveKBListColumns turns what is stored for a member into what they see: nil is the default, and
// a name this server does not know (written by a newer one) is skipped.
func EffectiveKBListColumns(stored []string) KBListColumns {
	if stored == nil {
		return KBListColumns{Columns: slices.Clone(kbListDefaultColumns), Default: true}
	}
	columns := []string{}
	for _, name := range stored {
		if slices.Contains(KBListColumnNames, name) && !slices.Contains(columns, name) {
			columns = append(columns, name)
		}
	}
	return KBListColumns{Columns: columns}
}

// CheckKBListColumns refuses a choice with a column that does not exist or one named twice.
func CheckKBListColumns(columns []string) error {
	for index, name := range columns {
		if !slices.Contains(KBListColumnNames, name) {
			return fmt.Errorf("there is no column %q: choose from %s", name, strings.Join(KBListColumnNames, ", "))
		}
		if slices.Contains(columns[:index], name) {
			return fmt.Errorf("%q is named twice: name each column once", name)
		}
	}
	return nil
}
