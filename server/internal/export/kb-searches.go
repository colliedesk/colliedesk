package export

import "time"

// KBSearch is one search a reader made on the help center: the words as kept (an email address or a
// run of 6 or more digits already masked), how many articles it found and the article opened from its
// results. Nothing about the reader is kept, so nothing is exported.
type KBSearch struct {
	Query           string    `json:"query"`
	Results         int       `json:"results"`
	OpenedArticleID *string   `json:"opened_article_id"`
	CreatedAt       time.Time `json:"created_at"`
}

func (w *Writer) KBSearch(record KBSearch) error {
	return w.record("kb-searches.ndjson", record)
}
