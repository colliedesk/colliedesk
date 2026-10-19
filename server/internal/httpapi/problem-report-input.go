package httpapi

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits of one problem report. The web app's diagnostics ring keeps no more than these; the server
// enforces them again because any client can call the route.
const (
	maxProblemDescription = 5000
	maxProblemErrors      = 20
	maxProblemCalls       = 20
	maxProblemEvents      = 30
	maxProblemScreenshots = 3
	maxProblemShotBytes   = 5 << 20
)

// diagnosticsInput is what the web app gathered in the browser. It holds ids, routes, status codes and
// error messages; it never holds what a customer wrote.
type diagnosticsInput struct {
	AppVersion     string            `json:"app_version"`
	UserAgent      string            `json:"user_agent"`
	Viewport       *viewportInput    `json:"viewport"`
	PagePath       string            `json:"page_path"`
	ClientErrors   []clientErrorItem `json:"client_errors"`
	FailedAPICalls []failedCallItem  `json:"failed_api_calls"`
	ProductEvents  []string          `json:"product_events"`
}

type viewportInput struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type clientErrorItem struct {
	Message string `json:"message"`
	Source  string `json:"source"`
	At      string `json:"at"`
}

type failedCallItem struct {
	Method    string `json:"method"`
	Route     string `json:"route"`
	Status    int    `json:"status"`
	RequestID string `json:"request_id"`
	At        string `json:"at"`
}

// oneLine collapses every run of whitespace (line breaks included) and cuts the text at max runes, so a
// value cannot start a new line or section in the report.
func oneLine(value string, max int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > max {
		return string([]rune(value)[:max]) + "…"
	}
	return value
}

// withoutQuery drops the query string and fragment: a search term or an id in the address could be a
// customer's words.
func withoutQuery(path string) string {
	if cut := strings.IndexAny(path, "?#"); cut >= 0 {
		path = path[:cut]
	}
	return path
}

func cleanTime(value string) (string, bool) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format(time.RFC3339), true
}

// clean returns the diagnostics with every field limited, or the reason it is refused.
func (d diagnosticsInput) clean() (diagnosticsInput, string) {
	switch {
	case len(d.ClientErrors) > maxProblemErrors:
		return d, fmt.Sprintf("Send at most %d client errors.", maxProblemErrors)
	case len(d.FailedAPICalls) > maxProblemCalls:
		return d, fmt.Sprintf("Send at most %d failed API calls.", maxProblemCalls)
	case len(d.ProductEvents) > maxProblemEvents:
		return d, fmt.Sprintf("Send at most %d product events.", maxProblemEvents)
	case d.Viewport != nil && (d.Viewport.Width < 0 || d.Viewport.Height < 0 || d.Viewport.Width > 20000 || d.Viewport.Height > 20000):
		return d, "The viewport must be between 0 and 20000 pixels."
	}
	cleaned := diagnosticsInput{AppVersion: oneLine(d.AppVersion, 40), UserAgent: oneLine(d.UserAgent, 300), Viewport: d.Viewport,
		PagePath: oneLine(withoutQuery(d.PagePath), 300)}
	for _, item := range d.ClientErrors {
		at, ok := cleanTime(item.At)
		if !ok {
			return d, "Each client error needs an RFC 3339 time in at."
		}
		cleaned.ClientErrors = append(cleaned.ClientErrors, clientErrorItem{Message: oneLine(item.Message, 2000), Source: oneLine(item.Source, 100), At: at})
	}
	for _, item := range d.FailedAPICalls {
		at, ok := cleanTime(item.At)
		switch {
		case !ok:
			return d, "Each failed API call needs an RFC 3339 time in at."
		case item.Status < 100 || item.Status > 599:
			return d, "Each failed API call needs an HTTP status between 100 and 599."
		}
		cleaned.FailedAPICalls = append(cleaned.FailedAPICalls, failedCallItem{Method: strings.ToUpper(oneLine(item.Method, 10)),
			Route: oneLine(withoutQuery(item.Route), 300), Status: item.Status, RequestID: oneLine(item.RequestID, 64), At: at})
	}
	for _, event := range d.ProductEvents {
		if name := oneLine(event, 60); name != "" {
			cleaned.ProductEvents = append(cleaned.ProductEvents, name)
		}
	}
	return cleaned, ""
}
