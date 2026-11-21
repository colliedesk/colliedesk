package results

import (
	"math"
	"time"
)

// HelpCenter is the results page's help center section, read live on every request: help center
// article views next to the conversations opened, per window (docs/internals/results.md › Help center).
type HelpCenter struct {
	// Timezone is the workspace's: every day and week below is a calendar day or week there.
	Timezone string                       `json:"timezone"`
	Windows  map[string]*HelpCenterWindow `json:"windows"`
}

// HelpCenterWindow is the last Days calendar days, today included, as the half-open range [From, To).
type HelpCenterWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Views are help center article views; Requests are the conversations opened, as the inbox lists them
	// with source=native, created_after=From and created_before=To (spam is never listed there).
	Views    int64 `json:"views"`
	Requests int64 `json:"requests"`
	// Score is Views per conversation opened, nil when no conversation was opened.
	Score *float64 `json:"score"`
	// Weeks start on Monday, the first and the last clipped to the window, so they add up to it.
	Weeks []HelpCenterWeek `json:"weeks"`
	Days  []HelpCenterDay  `json:"days"`
	// Articles are the articles viewed or used in a reply in the window, most viewed first, at most
	// MaxHelpCenterArticles of them; ArticlesTotal counts them all.
	Articles      []HelpCenterArticle `json:"articles"`
	ArticlesTotal int                 `json:"articles_total"`
}

// HelpCenterWeek is one week of a window.
type HelpCenterWeek struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	FirstDay string    `json:"first_day"`
	LastDay  string    `json:"last_day"`
	Views    int64     `json:"views"`
	Requests int64     `json:"requests"`
	Score    *float64  `json:"score"`
}

// HelpCenterDay is one calendar day of a window.
type HelpCenterDay struct {
	Day      string `json:"day"`
	Views    int64  `json:"views"`
	Requests int64  `json:"requests"`
}

// HelpCenterArticle is one article's row. Helpful and Votes are the votes its page shows now, whatever
// the window; Uses are the sent replies that linked it or were an AI draft quoting it, each reply once.
type HelpCenterArticle struct {
	ID             string `json:"id"`
	Number         int64  `json:"number"`
	Title          string `json:"title"`
	Views          int64  `json:"views"`
	Helpful        int64  `json:"helpful"`
	Votes          int64  `json:"votes"`
	HelpfulPercent *int   `json:"helpful_percent"`
	Uses           int64  `json:"uses"`
}

// MaxHelpCenterArticles bounds the article rows of a window.
const MaxHelpCenterArticles = 50

// DateLayout is how a calendar day is written.
const DateLayout = "2006-01-02"

// Period is the last days calendar days in loc up to now, today included, as [from, to).
func Period(now time.Time, loc *time.Location, days int) (from, to time.Time) {
	local := now.In(loc)
	end := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
	return time.Date(local.Year(), local.Month(), local.Day()+1-days, 0, 0, 0, 0, loc).UTC(), end.UTC()
}

// Score is views per request to one decimal, nil without requests.
func Score(views, requests int64) *float64 {
	if requests == 0 {
		return nil
	}
	score := math.Round(float64(views)/float64(requests)*10) / 10
	return &score
}

// HelpCenterSpan builds a window's days and weeks from the views and requests of each calendar day
// (keyed by DateLayout); Articles are left to the caller.
func HelpCenterSpan(from, to time.Time, loc *time.Location, views, requests map[string]int64) *HelpCenterWindow {
	window := &HelpCenterWindow{From: from, To: to, Weeks: []HelpCenterWeek{}, Days: []HelpCenterDay{}, Articles: []HelpCenterArticle{}}
	start := from.In(loc)
	for day := start; day.Before(to); day = time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc) {
		key := day.Format(DateLayout)
		next := time.Date(day.Year(), day.Month(), day.Day()+1, 0, 0, 0, 0, loc)
		window.Days = append(window.Days, HelpCenterDay{Day: key, Views: views[key], Requests: requests[key]})
		window.Views += views[key]
		window.Requests += requests[key]
		if len(window.Weeks) == 0 || day.Weekday() == time.Monday {
			window.Weeks = append(window.Weeks, HelpCenterWeek{From: day.UTC(), FirstDay: key})
		}
		week := &window.Weeks[len(window.Weeks)-1]
		week.To, week.LastDay = next.UTC(), key
		week.Views += views[key]
		week.Requests += requests[key]
	}
	for index := range window.Weeks {
		window.Weeks[index].Score = Score(window.Weeks[index].Views, window.Weeks[index].Requests)
	}
	window.Score = Score(window.Views, window.Requests)
	return window
}
