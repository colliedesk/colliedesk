package domain

import (
	"fmt"
	"strings"
)

// KBMediaKeptArticle is an article a replacement put the new item in.
type KBMediaKeptArticle struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
}

// KBMediaKept is an item a replacement that is not undone keeps as its earlier version.
type KBMediaKept struct {
	MediaID       string               `json:"media_id"`
	Name          string               `json:"-"`
	ReplacementID string               `json:"replacement_id"`
	ToMediaID     string               `json:"to_media_id"`
	ToName        string               `json:"-"`
	Articles      []KBMediaKeptArticle `json:"articles"`
}

// KBMediaKeptError refuses a purge of items that are the earlier version of a replacement still in
// place: undoing it needs them. The purge deleted nothing.
type KBMediaKeptError struct {
	Kept []KBMediaKept
}

func (e *KBMediaKeptError) Error() string {
	parts := make([]string, len(e.Kept))
	for i, kept := range e.Kept {
		titles := make([]string, len(kept.Articles))
		for j, article := range kept.Articles {
			titles[j] = fmt.Sprintf("%q", article.Title)
		}
		where := ""
		if len(titles) > 0 {
			where = " in " + strings.Join(titles, ", ")
		}
		parts[i] = fmt.Sprintf("%s is the earlier version of %s%s (replacement %s)",
			kbMediaLabel(kept.Name, kept.MediaID), kbMediaLabel(kept.ToName, kept.ToMediaID), where, kept.ReplacementID)
	}
	return "Nothing was purged: " + strings.Join(parts, "; ") +
		". Undo that replacement first to purge it; it may stay in the trash meanwhile."
}

func kbMediaLabel(name, id string) string {
	if name == "" {
		return "item " + id
	}
	return fmt.Sprintf("%q (%s)", name, id)
}
