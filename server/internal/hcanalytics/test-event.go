package hcanalytics

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

// TestPath is the page a test event names, so a team can tell it from its readers' page views.
const TestPath = "/colliedesk-analytics-test"

// TestResult is what a test send learned.
type TestResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Test sends one test page view to target now, outside the queue, as the person who asked for it
// (their browser and address), and says what the service answered. GA4 first checks the event at
// its validation endpoint, which names a wrong measurement ID but never checks the API secret, then
// sends it for real so it shows in the Realtime report. Plausible answers 202 to everything and says
// only whether its bot filter dropped the event.
func (s *Sender) Test(ctx context.Context, target Target, userAgent, client string) TestResult {
	event := pageview.Event{WorkspaceID: target.WorkspaceID, Kind: pageview.Home, Title: "Colliedesk analytics test",
		Address: strings.TrimSuffix(s.options.Origin, "/") + TestPath, Locale: "en", Client: client, UserAgent: userAgent,
		At: s.options.Now()}
	item := &delivery{target: target, event: event}
	if target.Provider == GA4 {
		s.mu.Lock()
		item.clientID, item.session = s.readerIDs(event)
		s.mu.Unlock()
		if result, ok := s.validateGA4(ctx, item); !ok {
			return result
		}
	}
	request, err := s.pageViewRequest(ctx, item, "/mp/collect")
	if err != nil {
		return TestResult{Message: serviceName(target.Provider) + " could not be reached."}
	}
	response, failure := s.do(request, target.Provider)
	if failure != "" {
		return TestResult{Message: failure}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	_ = response.Body.Close()
	if target.Provider == GA4 {
		return TestResult{OK: true, Message: "Google found nothing wrong with the event and Colliedesk sent a page view of " + TestPath +
			". Google does not check the API secret: open Reports › Realtime in GA4, where the page view shows within a minute when the secret is right."}
	}
	if response.Header.Get("X-Plausible-Dropped") != "" {
		return TestResult{Message: "Plausible accepted the event but its bot filter dropped it, so it will not show. Send the test from a browser on a public network."}
	}
	return TestResult{OK: true, Message: "Plausible accepted a page view of " + TestPath + " for " + target.Domain +
		". It shows on that site's dashboard within a minute; if it does not, check that the domain is exactly the site's in Plausible."}
}

// validateGA4 sends the event to GA4's validation endpoint and reports false with Google's
// messages when it found something wrong.
func (s *Sender) validateGA4(ctx context.Context, item *delivery) (TestResult, bool) {
	request, err := s.pageViewRequest(ctx, item, "/debug/mp/collect")
	if err != nil {
		return TestResult{Message: "GA4 could not be reached."}, false
	}
	response, failure := s.do(request, GA4)
	if failure != "" {
		return TestResult{Message: failure}, false
	}
	defer response.Body.Close()
	var answer struct {
		ValidationMessages []struct {
			FieldPath   string `json:"fieldPath"`
			Description string `json:"description"`
		} `json:"validationMessages"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&answer); err != nil {
		return TestResult{Message: "GA4 answered something Colliedesk could not read."}, false
	}
	if len(answer.ValidationMessages) == 0 {
		return TestResult{}, true
	}
	var lines []string
	for _, message := range answer.ValidationMessages {
		lines = append(lines, strings.TrimSpace(message.Description))
	}
	return TestResult{Message: "Google refused the event: " + strings.Join(lines, " ") + " Check the measurement ID of the web stream."}, false
}
