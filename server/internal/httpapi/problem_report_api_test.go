package httpapi_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const (
	reportPath      = "/api/v1/support/problem-reports"
	supportSlug     = "hc-labs-support"
	reportBuildName = "0.0.42-test"
)

type reportFixture struct {
	api             http.Handler
	clock           *testClock
	customer        string // Acme's admin session
	customerID      string
	support         string // the HC Labs support desk's admin session
	other           string // a third, unrelated workspace
	ownConversation string // an Acme conversation that must never leave Acme
}

func newReportFixture(t *testing.T, mutate func(*httpapi.Config)) reportFixture {
	t.Helper()
	clock := &testClock{now: trialStart}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Now = clock.Now
		config.Version = reportBuildName
		config.ProblemReportWorkspace = supportSlug
		if mutate != nil {
			mutate(config)
		}
	})
	service = service.WithClock(clock.Now)
	f := reportFixture{api: api, clock: clock}
	f.support = createSession(t, api, service, "HC Labs Support", "support@hclabs.example.com")
	if got := workspaceSlug(t, api, f.support); got != supportSlug {
		t.Fatalf("support workspace slug %q, want %q", got, supportSlug)
	}
	f.customer = createSession(t, api, service, "Acme Desk", "ada@acme.example.com")
	f.other = createSession(t, api, service, "Third Desk", "admin@third.example.com")
	f.customerID = workspaceID(t, api, f.customer)
	if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/display-name", map[string]any{"display_name": "Ada Admin"}, f.customer, nil); response.Code != http.StatusOK {
		t.Fatalf("set display name status=%d body=%s", response.Code, response.Body.String())
	}
	contact := create(t, api, f.customer, "/api/v1/contacts", map[string]any{"name": "Zed Customer", "email": "zed@customer-secret.example.com"})
	f.ownConversation = create(t, api, f.customer, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Refund for broken kettle"})
	create(t, api, f.customer, "/api/v1/conversations/"+f.ownConversation+"/messages", map[string]any{"kind": "note", "body": "Customer mentioned card ending 4242"})
	return f
}

func goodReport() map[string]any {
	return map[string]any{
		"description": "The inbox spins forever after I open a conversation.",
		"diagnostics": map[string]any{
			"app_version": "0.0.41", "user_agent": "Mozilla/5.0 (X11; Linux) TestBrowser/1.0",
			"viewport": map[string]any{"width": 1280, "height": 720}, "page_path": "/conversations/abc",
			"client_errors": []any{map[string]any{"message": "TypeError: x is undefined", "source": "window.onerror", "at": "2026-10-19T08:00:00Z"}},
			"failed_api_calls": []any{
				map[string]any{"method": "GET", "route": "/api/v1/conversations", "status": 500, "request_id": "9f3ac1d2b7e4", "at": "2026-10-19T08:00:01Z"},
				map[string]any{"method": "POST", "route": "/api/v1/conversations/{id}/messages", "status": 502, "request_id": "0badc0ffee11", "at": "2026-10-19T08:00:02Z"},
			},
			"product_events": []any{"conversation_opened", "inbox_searched"},
		},
	}
}

type supportConversation struct {
	ID        string `json:"id"`
	ContactID string `json:"contact_id"`
	Subject   string `json:"subject"`
	Tags      []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

type supportMessage struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Body        string `json:"body"`
	Attachments []struct {
		ID          string `json:"id"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type"`
		Size        int64  `json:"size"`
	} `json:"attachments"`
}

func (f reportFixture) conversations(t *testing.T, token string) []supportConversation {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/conversations?page_size=100", nil, token, nil)
	var page struct {
		Items []supportConversation `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func (f reportFixture) messages(t *testing.T, token, conversation string) []supportMessage {
	t.Helper()
	response := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+conversation+"/messages", nil, token, nil)
	var page struct {
		Items []supportMessage `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func (f reportFixture) report(t *testing.T, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, f.api, http.MethodPost, reportPath, body, token, nil)
}

// reported files a good report and returns the single conversation it opened in the support desk.
func (f reportFixture) reported(t *testing.T, body any) (supportConversation, supportMessage, *httptest.ResponseRecorder) {
	t.Helper()
	response := f.report(t, f.customer, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("report status=%d body=%s", response.Code, response.Body.String())
	}
	items := f.conversations(t, f.support)
	if len(items) != 1 {
		t.Fatalf("support desk has %d conversations, want 1: %+v", len(items), items)
	}
	messages := f.messages(t, f.support, items[0].ID)
	if len(messages) != 1 {
		t.Fatalf("report conversation has %d messages, want 1", len(messages))
	}
	return items[0], messages[0], response
}

func TestProblemReportOpensOneConversationInTheSupportDeskWithEverythingToCheckIt(t *testing.T) {
	f := newReportFixture(t, nil)
	conversation, message, response := f.reported(t, goodReport())

	var created struct {
		Reference string `json:"reference"`
	}
	decode(t, response, &created)
	requestID := response.Header().Get("X-Request-Id")
	if created.Reference == "" || created.Reference != requestID {
		t.Errorf("reference %q, want the report's request id %q", created.Reference, requestID)
	}
	if conversation.Subject != "Problem report: Acme Desk" {
		t.Errorf("subject %q", conversation.Subject)
	}
	if len(conversation.Tags) != 1 || conversation.Tags[0].Name != "problem-report" {
		t.Errorf("tags %+v, want problem-report", conversation.Tags)
	}
	var contact struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/contacts/"+conversation.ContactID, nil, f.support, nil), &contact)
	if contact.Email != "ada@acme.example.com" || contact.Name != "Ada Admin" {
		t.Errorf("contact %+v, want the reporter ada@acme.example.com / Ada Admin", contact)
	}
	if message.Kind != "incoming" {
		t.Errorf("message kind %q, want incoming", message.Kind)
	}
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.customer, nil), &me)
	for _, want := range []string{
		"The inbox spins forever after I open a conversation.",
		"9f3ac1d2b7e4", "0badc0ffee11", "GET /api/v1/conversations", "500", "502",
		"TypeError: x is undefined", "window.onerror", "conversation_opened, inbox_searched",
		"/conversations/abc", "TestBrowser/1.0", "1280", "720", "0.0.41",
		me.Agent.ID, "ada@acme.example.com", "Ada Admin", "admin",
		f.customerID, "Acme Desk", "acme-desk", reportBuildName, "self-hosted", requestID,
	} {
		if !strings.Contains(message.Body, want) {
			t.Errorf("report body lacks %q:\n%s", want, message.Body)
		}
	}
	if strings.Contains(message.Body, "PostHog:") {
		t.Errorf("a server without product analytics links PostHog:\n%s", message.Body)
	}
}

func TestProblemReportNeverCarriesTheReportersOwnConversations(t *testing.T) {
	f := newReportFixture(t, nil)
	conversation, message, _ := f.reported(t, goodReport())
	for _, forbidden := range []string{"Refund for broken kettle", "Zed Customer", "customer-secret", "card ending 4242", f.ownConversation} {
		if strings.Contains(message.Body, forbidden) || strings.Contains(conversation.Subject, forbidden) {
			t.Errorf("the report leaks %q:\n%s", forbidden, message.Body)
		}
	}
	// The reporter's workspace gained nothing, and a third workspace sees nothing.
	if own := f.conversations(t, f.customer); len(own) != 1 || own[0].ID != f.ownConversation {
		t.Errorf("the reporter's own conversations changed: %+v", own)
	}
	if third := f.conversations(t, f.other); len(third) != 0 {
		t.Errorf("an unrelated workspace sees %+v", third)
	}
	if response := request(t, f.api, http.MethodGet, "/api/v1/conversations/"+conversation.ID, nil, f.customer, nil); response.Code != http.StatusNotFound {
		t.Errorf("the reporter reads the support desk's conversation: status=%d", response.Code)
	}
}

func TestProblemReportLinksPostHogOnlyWhenProductAnalyticsIsOn(t *testing.T) {
	f := newReportFixture(t, func(config *httpapi.Config) {
		config.ProductAnalytics = &httpapi.ProductAnalytics{Key: "phc_test", Host: "https://us.i.posthog.com", Realm: "cloud"}
	})
	_, message, _ := f.reported(t, goodReport())
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/auth/me", nil, f.customer, nil), &me)
	want := "PostHog: https://us.posthog.com/project/666761/person/" + me.Agent.ID
	if !strings.Contains(message.Body, "\n"+want+"\n") && !strings.HasSuffix(strings.TrimSpace(message.Body), want) {
		t.Errorf("report lacks the line %q:\n%s", want, message.Body)
	}
	if !strings.Contains(message.Body, "cloud") {
		t.Errorf("report does not name the realm cloud:\n%s", message.Body)
	}
}

func TestProblemReportNeedsOnlyADescription(t *testing.T) {
	f := newReportFixture(t, nil)
	_, message, _ := f.reported(t, map[string]any{"description": "  Plain text only  "})
	if !strings.Contains(message.Body, "Plain text only") || !strings.Contains(message.Body, "ada@acme.example.com") {
		t.Errorf("body %q", message.Body)
	}
}

func TestProblemReportDropsQueryStringsAndLimitsLongText(t *testing.T) {
	f := newReportFixture(t, nil)
	body := goodReport()
	diag := body["diagnostics"].(map[string]any)
	diag["page_path"] = "/conversations/abc?q=customer+words#frag"
	diag["failed_api_calls"] = []any{map[string]any{"method": "GET", "route": "/api/v1/conversations?q=secret+search", "status": 500, "request_id": "9f3ac1d2b7e4", "at": "2026-10-19T08:00:01Z"}}
	diag["client_errors"] = []any{map[string]any{"message": strings.Repeat("e", 9000), "source": "window.onerror", "at": "2026-10-19T08:00:00Z"}}
	_, message, _ := f.reported(t, body)
	for _, leaked := range []string{"customer+words", "secret+search", "frag", strings.Repeat("e", 2001)} {
		if strings.Contains(message.Body, leaked) {
			t.Errorf("report keeps %.20q...:\n%.600s", leaked, message.Body)
		}
	}
	if !strings.Contains(message.Body, "/api/v1/conversations") {
		t.Errorf("the route without its query is missing:\n%s", message.Body)
	}
}

func TestProblemReportIsNotConfiguredOnAServerWithoutASupportDesk(t *testing.T) {
	for name, slug := range map[string]string{"unset": "", "no such workspace": "missing-desk"} {
		t.Run(name, func(t *testing.T) {
			f := newReportFixture(t, func(config *httpapi.Config) { config.ProblemReportWorkspace = slug })
			response := f.report(t, f.customer, goodReport())
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"not_configured"`) {
				t.Fatalf("status=%d body=%s, want 404 not_configured", response.Code, response.Body.String())
			}
			if items := f.conversations(t, f.support); len(items) != 0 {
				t.Errorf("a conversation was opened anyway: %+v", items)
			}
		})
	}
}

func TestProblemReportRequiresASignedInAgent(t *testing.T) {
	f := newReportFixture(t, nil)
	if response := f.report(t, "", goodReport()); response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", response.Code)
	}
	if items := f.conversations(t, f.support); len(items) != 0 {
		t.Fatalf("an anonymous report opened %+v", items)
	}
}

func TestProblemReportRejectsInvalidInputWith422(t *testing.T) {
	f := newReportFixture(t, nil)
	manyErrors := make([]any, 21)
	for i := range manyErrors {
		manyErrors[i] = map[string]any{"message": "boom", "source": "window.onerror", "at": "2026-10-19T08:00:00Z"}
	}
	manyCalls := make([]any, 21)
	for i := range manyCalls {
		manyCalls[i] = map[string]any{"method": "GET", "route": "/api/v1/tags", "status": 500, "request_id": "9f3ac1d2b7e4", "at": "2026-10-19T08:00:00Z"}
	}
	manyEvents := make([]any, 31)
	for i := range manyEvents {
		manyEvents[i] = "conversation_opened"
	}
	with := func(key string, value any) map[string]any {
		body := goodReport()
		body["diagnostics"].(map[string]any)[key] = value
		return body
	}
	cases := map[string]struct {
		body  map[string]any
		field string
	}{
		"missing description":    {map[string]any{"diagnostics": map[string]any{}}, "description"},
		"blank description":      {map[string]any{"description": " \n\t "}, "description"},
		"description too long":   {map[string]any{"description": strings.Repeat("x", 5001)}, "description"},
		"too many client errors": {with("client_errors", manyErrors), "diagnostics"},
		"too many failed calls":  {with("failed_api_calls", manyCalls), "diagnostics"},
		"too many events":        {with("product_events", manyEvents), "diagnostics"},
		"status out of range":    {with("failed_api_calls", []any{map[string]any{"method": "GET", "route": "/x", "status": 99, "request_id": "9f3ac1d2b7e4", "at": "2026-10-19T08:00:00Z"}}), "diagnostics"},
		"bad timestamp":          {with("client_errors", []any{map[string]any{"message": "m", "source": "s", "at": "yesterday"}}), "diagnostics"},
		"negative viewport":      {with("viewport", map[string]any{"width": -1, "height": 10}), "diagnostics"},
	}
	for name, item := range cases {
		response := f.report(t, f.customer, item.body)
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"validation_failed"`) || !strings.Contains(response.Body.String(), `"`+item.field+`"`) {
			t.Errorf("%s: status=%d body=%s, want 422 validation_failed naming %s", name, response.Code, response.Body.String(), item.field)
		}
	}
	if response := request(t, f.api, http.MethodPost, reportPath, "not an object", f.customer, nil); response.Code != http.StatusBadRequest {
		t.Errorf("a body that is not an object: status=%d, want 400", response.Code)
	}
	if items := f.conversations(t, f.support); len(items) != 0 {
		t.Errorf("an invalid report opened %+v", items)
	}
}

func TestProblemReportLimitsEachAgentToFiveAnHour(t *testing.T) {
	f := newReportFixture(t, nil)
	for i := 0; i < 5; i++ {
		if response := f.report(t, f.customer, goodReport()); response.Code != http.StatusCreated {
			t.Fatalf("report %d: status=%d body=%s", i+1, response.Code, response.Body.String())
		}
	}
	refused := f.report(t, f.customer, goodReport())
	if refused.Code != http.StatusTooManyRequests || !strings.Contains(refused.Body.String(), `"rate_limited"`) || refused.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth report: status=%d retry-after=%q body=%s", refused.Code, refused.Header().Get("Retry-After"), refused.Body.String())
	}
	if response := f.report(t, f.other, goodReport()); response.Code != http.StatusCreated {
		t.Errorf("another agent was limited too: status=%d", response.Code)
	}
	if got := len(f.conversations(t, f.support)); got != 6 {
		t.Errorf("support desk has %d conversations, want 6 (five plus the other agent's)", got)
	}
	f.clock.set(f.clock.Now().Add(time.Hour + time.Minute))
	if response := f.report(t, f.customer, goodReport()); response.Code != http.StatusCreated {
		t.Errorf("after the hour: status=%d, want 201", response.Code)
	}
}

func TestProblemReportCanBeFiledByAnAgentTokenOfAnyTier(t *testing.T) {
	f := newReportFixture(t, nil)
	for _, tier := range []string{"read", "draft", "send", "admin"} {
		token := issueToken(t, f.api, f.customer, "report "+tier, tier).Token
		if response := f.report(t, token, map[string]any{"description": "Reported by a " + tier + " token"}); response.Code != http.StatusCreated {
			t.Errorf("%s token: status=%d body=%s", tier, response.Code, response.Body.String())
		}
	}
	items := f.conversations(t, f.support)
	if len(items) != 4 {
		t.Fatalf("support desk has %d conversations, want 4", len(items))
	}
	found := false
	for _, item := range items {
		for _, message := range f.messages(t, f.support, item.ID) {
			found = found || (strings.Contains(message.Body, "Reported by a read token") && strings.Contains(message.Body, "agent token (read)"))
		}
	}
	if !found {
		t.Error("the report of a token does not say it came from an agent token and its tier")
	}
}

// pngBytes is a valid PNG header followed by filler of the given size.
func pngBytes(size int) []byte {
	data := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x42}, size)...)
	return data
}

func jpegBytes(size int) []byte {
	return append([]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), bytes.Repeat([]byte{0x43}, size)...)
}

type reportFile struct {
	name, contentType string
	data              []byte
}

func multipartReport(t *testing.T, description, diagnostics string, files ...reportFile) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("description", description); err != nil {
		t.Fatal(err)
	}
	if diagnostics != "" {
		if err := writer.WriteField("diagnostics", diagnostics); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="screenshots"; filename=%q`, file.name))
		header.Set("Content-Type", file.contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func (f reportFixture) reportWithFiles(t *testing.T, diagnostics string, files ...reportFile) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartReport(t, "The button does nothing.", diagnostics, files...)
	req := httptest.NewRequest(http.MethodPost, reportPath, body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+f.customer)
	return serve(f.api, req)
}

func TestProblemReportStoresScreenshotsAsAttachmentsOfTheSupportMessage(t *testing.T) {
	f := newReportFixture(t, nil)
	shot, photo := pngBytes(2000), jpegBytes(3000)
	response := f.reportWithFiles(t, `{"page_path":"/settings","product_events":["settings_section_opened"]}`,
		reportFile{"screen.png", "image/png", shot}, reportFile{"photo.jpg", "image/jpeg", photo})
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	items := f.conversations(t, f.support)
	if len(items) != 1 {
		t.Fatalf("support desk has %d conversations, want 1", len(items))
	}
	message := f.messages(t, f.support, items[0].ID)[0]
	if !strings.Contains(message.Body, "The button does nothing.") || !strings.Contains(message.Body, "/settings") {
		t.Errorf("body %q", message.Body)
	}
	if len(message.Attachments) != 2 {
		t.Fatalf("attachments %+v, want 2", message.Attachments)
	}
	want := map[string][]byte{"screen.png": shot, "photo.jpg": photo}
	for _, attachment := range message.Attachments {
		download := request(t, f.api, http.MethodGet, "/api/v1/attachments/"+attachment.ID, nil, f.support, nil)
		if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), want[attachment.Filename]) {
			t.Errorf("download %s: status=%d, %d bytes, want %d", attachment.Filename, download.Code, download.Body.Len(), len(want[attachment.Filename]))
		}
		if download := request(t, f.api, http.MethodGet, "/api/v1/attachments/"+attachment.ID, nil, f.customer, nil); download.Code != http.StatusNotFound {
			t.Errorf("the reporter reads the support desk's attachment: status=%d", download.Code)
		}
	}
}

func TestProblemReportRefusesScreenshotsThatAreNotSmallImages(t *testing.T) {
	f := newReportFixture(t, nil)
	cases := map[string]struct {
		files  []reportFile
		status int
	}{
		"gif":                   {[]reportFile{{"a.gif", "image/gif", []byte("GIF89a....")}}, http.StatusUnprocessableEntity},
		"text claiming png":     {[]reportFile{{"a.png", "image/png", []byte("just text, not an image")}}, http.StatusUnprocessableEntity},
		"four images":           {[]reportFile{{"1.png", "image/png", pngBytes(10)}, {"2.png", "image/png", pngBytes(11)}, {"3.png", "image/png", pngBytes(12)}, {"4.png", "image/png", pngBytes(13)}}, http.StatusUnprocessableEntity},
		"over 5 MB in total":    {[]reportFile{{"1.png", "image/png", pngBytes(2700 << 10)}, {"2.png", "image/png", pngBytes(2700 << 10)}}, http.StatusUnprocessableEntity},
		"far over the 5 MB cap": {[]reportFile{{"1.png", "image/png", pngBytes(7 << 20)}}, http.StatusUnprocessableEntity},
	}
	for name, item := range cases {
		response := f.reportWithFiles(t, "", item.files...)
		if response.Code != item.status {
			t.Errorf("%s: status=%d body=%.200s, want %d", name, response.Code, response.Body.String(), item.status)
		}
	}
	if items := f.conversations(t, f.support); len(items) != 0 {
		t.Errorf("a refused report opened %+v", items)
	}
}
