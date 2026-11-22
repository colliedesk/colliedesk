package domain

import "time"

// KBSearchReportDays is the window of the search report: the last 7 days up to the moment it is read.
const KBSearchReportDays = 7

// MaxKBSearchReportRows is how many searches each list of the report shows.
const MaxKBSearchReportRows = 20

// KBSearchReport is what readers searched for on the help center from From to To: the most
// searched words, the words that found nothing, and the words whose results nobody opened. Searches
// with the same folded words count as one row.
type KBSearchReport struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Searches counts every search kept in the window.
	Searches      int64          `json:"searches"`
	Top           []KBSearchTerm `json:"top"`
	NoResults     []KBSearchTerm `json:"no_results"`
	NothingOpened []KBSearchTerm `json:"nothing_opened"`
}

// KBSearchTerm is one group of searches in a list of the report.
type KBSearchTerm struct {
	// Query is the newest masked words of the group, as a reader typed them.
	Query    string `json:"query"`
	Searches int64  `json:"searches"`
	// Opened counts the searches of the group from which the reader opened an article.
	Opened int64 `json:"opened"`
	// Results is how many articles the newest search of the group listed.
	Results        int       `json:"results"`
	LastSearchedAt time.Time `json:"last_searched_at"`
}
