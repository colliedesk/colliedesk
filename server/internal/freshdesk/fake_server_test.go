package freshdesk_test

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAPIKey = "fd-test-key-DO-NOT-PRINT"

var fileBodies = map[string]string{"9001": "PDF-BYTES-1", "9002": "PNG-BYTES", "9003": "log line 1", "inline-5001": inlineImageBody, "inline-kb-7001": kbImageBody}

const (
	pngSignature     = "\x89PNG\r\n\x1a\n"
	inlineImageBody  = pngSignature + "INLINE-1"
	accountImageBody = pngSignature + "ACCOUNT"
	kbImageBody      = pngSignature + "KNOWLEDGE-BASE"
)

// fakeFreshdesk serves JSON shaped like the public Freshdesk API v2 from
// testdata over TLS, with page size 2, Link headers, rate-limit headers, an
// archive, a trashed ticket, search totals and attachment URLs on a separate
// "storage" host. Fields toggle failure scenarios.
type fakeFreshdesk struct {
	t                    *testing.T
	api, files, internal *httptest.Server
	roots                *x509.CertPool
	mu                   sync.Mutex
	requests             map[string]int
	fileAuthSeen         bool
	throttleContacts     bool
	remainingZeroAgents  bool
	foreignNextLink      bool
	archiveForbidden     bool
	allArchived          bool
	noTickets            bool
	failSearch           bool
	searchTicketTotal    int
	failAttachment       string
	redirectFile         string
	stallFile            string
	archiveError         string
	stallPath            string
	extraReplyOnTicket6  bool
	ticket1Updated       bool
	// canned serves the canned response fixtures; without it the account has no canned responses.
	canned bool
	// cannedExtra are responses added to folder 801; cannedCountBoost is added to its responses_count.
	cannedExtra      []map[string]any
	cannedCountBoost int
	accountID        int
	fileBody         map[string]string
	fileLength       map[string]string
	// fileType overrides the Content-Type of a file; inline images default to image/png.
	fileType map[string]string
	// fileStatus makes a file answer with the given HTTP status.
	fileStatus map[string]int
	// inlineImages puts the fixtures' message-HTML images in place: one on the file host (its
	// signature changes on every read), one on the account host, three that Freshdesk does not host.
	inlineImages bool
	// inlineHTTP writes the account host image with an http:// URL, as old tickets do.
	inlineHTTP bool
	// bigInlineImages replaces the file host image with three images (inline-6001..6003) in one message.
	bigInlineImages bool
	onRequest       func(path string)
	// failPathTimes makes the next N API requests to a path answer 500.
	failPathTimes map[string]int
	// failPathStatus makes every API request to a path answer with the given status.
	failPathStatus map[string]int
	// solutions serves the knowledge base fixture (testdata/solutions.json); without it every solutions
	// path answers 404, an account without a knowledge base. solutionsEdit changes the fixture first.
	solutions     bool
	solutionsEdit func(map[string]any)
	// remaining replaces the X-Ratelimit-Remaining of every API response (default 399 of 400).
	remaining string
}

func newFakeFreshdesk(t *testing.T) *fakeFreshdesk {
	t.Helper()
	fake := &fakeFreshdesk{t: t, requests: map[string]int{}, searchTicketTotal: -1, fileBody: map[string]string{}, fileLength: map[string]string{}, fileType: map[string]string{}, fileStatus: map[string]int{}}
	fake.api = httptest.NewTLSServer(http.HandlerFunc(fake.serveAPI))
	fake.files = httptest.NewTLSServer(http.HandlerFunc(fake.serveFile))
	fake.internal = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.hit("/internal" + r.URL.Path)
		_, _ = w.Write([]byte("internal secret"))
	}))
	fake.roots = x509.NewCertPool()
	for _, server := range []*httptest.Server{fake.api, fake.files, fake.internal} {
		fake.roots.AddCert(server.Certificate())
	}
	t.Cleanup(func() { fake.api.Close(); fake.files.Close(); fake.internal.Close() })
	return fake
}

func (f *fakeFreshdesk) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[path]
}

func (f *fakeFreshdesk) hit(path string) int {
	f.mu.Lock()
	f.requests[path]++
	n := f.requests[path]
	hook := f.onRequest
	f.mu.Unlock()
	if hook != nil {
		hook(path)
	}
	return n
}

func (f *fakeFreshdesk) stall(r *http.Request) {
	select {
	case <-r.Context().Done():
	case <-time.After(20 * time.Second):
	}
}

func (f *fakeFreshdesk) serveAPI(w http.ResponseWriter, r *http.Request) {
	n := f.hit(r.URL.Path)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testAPIKey+":X"))
	if r.Header.Get("Authorization") != want {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "invalid_credentials", "message": "You have to be logged in to perform this action."})
		return
	}
	f.mu.Lock()
	if status := f.failPathStatus[r.URL.Path]; status != 0 {
		f.mu.Unlock()
		http.Error(w, "refused", status)
		return
	}
	failing := f.failPathTimes[r.URL.Path] > 0
	if failing {
		f.failPathTimes[r.URL.Path]--
	}
	f.mu.Unlock()
	if failing {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if f.stallPath == r.URL.Path {
		f.stall(r)
		return
	}
	w.Header().Set("X-Ratelimit-Total", "400")
	w.Header().Set("X-Ratelimit-Remaining", "399")
	if f.remaining != "" {
		w.Header().Set("X-Ratelimit-Remaining", f.remaining)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v2/"), "/")
	switch {
	case strings.HasPrefix(r.URL.Path, "/helpdesk/attachments/"):
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(accountImageBody))
	case r.URL.Path == "/api/v2/account":
		f.mu.Lock()
		id := f.accountID
		f.mu.Unlock()
		if id == 0 {
			id = 777
		}
		writeJSON(w, http.StatusOK, map[string]any{"account_id": id, "account_name": "Acme", "account_domain": "acme.freshdesk.com"})
	case r.URL.Path == "/api/v2/search/tickets" || r.URL.Path == "/api/v2/search/contacts":
		f.search(w, r)
	case r.URL.Path == "/api/v2/agents":
		if f.remainingZeroAgents {
			w.Header().Set("X-Ratelimit-Remaining", "0")
		}
		if f.foreignNextLink {
			w.Header().Set("Link", fmt.Sprintf("<%s/api/v2/agents?page=2>; rel=\"next\"", f.files.URL))
			writeJSON(w, http.StatusOK, f.list("agents.json"))
			return
		}
		f.page(w, r, f.list("agents.json"))
	case r.URL.Path == "/api/v2/companies":
		f.page(w, r, f.list("companies.json"))
	case r.URL.Path == "/api/v2/contacts":
		if f.throttleContacts && n == 1 {
			w.Header().Set("Retry-After", "7")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"message": "You have exceeded the limit of requests per minute"})
			return
		}
		f.page(w, r, f.list("contacts.json"))
	case len(parts) == 2 && parts[0] == "contacts":
		f.object(w, "contact-"+parts[1]+".json")
	case r.URL.Path == "/api/v2/tickets":
		if f.allArchived || f.noTickets {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		if query := r.URL.Query(); query.Get("order_by") == "created_at" && query.Get("order_type") == "desc" {
			f.newestTickets(w, r)
			return
		}
		f.page(w, r, f.ticketList(r.URL.Query().Get("updated_since")))
	case f.noTickets && parts[0] == "tickets":
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	case len(parts) == 3 && parts[1] == "archived":
		f.archived(w, parts[2], n)
	case len(parts) == 4 && parts[1] == "archived" && parts[3] == "conversations":
		if f.archiveForbidden {
			writeJSON(w, http.StatusForbidden, map[string]string{"message": "archive is not enabled"})
			return
		}
		name := "archived-conversations-" + parts[2] + ".json"
		if f.allArchived && parts[2] != "3" {
			name = "conversations-" + parts[2] + ".json"
		}
		f.page(w, r, f.withFiles(f.list(name), n))
	case len(parts) == 2 && parts[0] == "tickets":
		f.ticket(w, parts[1], n)
	case len(parts) == 3 && parts[2] == "conversations":
		items := f.list("conversations-" + parts[1] + ".json")
		if parts[1] == "6" && f.extraReplyOnTicket6 {
			items = append(items, map[string]any{"id": 7007, "body": "<p>Any news?</p>", "body_text": "Any news?", "incoming": true, "private": false,
				"user_id": 299, "source": 0, "ticket_id": 6, "attachments": []any{}, "created_at": "2025-04-05T10:00:00Z", "updated_at": "2025-04-05T10:00:00Z"})
		}
		f.page(w, r, f.withFiles(items, n))
	case r.URL.Path == "/api/v2/canned_response_folders":
		f.page(w, r, f.cannedFolders())
	case len(parts) == 3 && parts[0] == "canned_response_folders" && parts[2] == "responses":
		f.page(w, r, f.cannedResponses(parts[1]))
	case f.solutions && r.URL.Path == "/api/v2/products":
		f.page(w, r, []map[string]any{})
	case f.solutions && (parts[0] == "solutions" || r.URL.Path == "/api/v2/settings/helpdesk"):
		f.serveSolutions(w, r, parts)
	case len(parts) == 3 && parts[2] == "time_entries":
		f.page(w, r, f.list("time-entries-"+parts[1]+".json"))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}
}

// solutionsFixture reads testdata/solutions.json with the file host filled in and solutionsEdit applied.
func (f *fakeFreshdesk) solutionsFixture() map[string]any {
	data, err := os.ReadFile(filepath.Join("testdata", "solutions.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	var fixture map[string]any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(data), "{{files}}", f.files.URL)), &fixture); err != nil {
		f.t.Fatalf("fixture solutions.json: %v", err)
	}
	f.mu.Lock()
	edit := f.solutionsEdit
	f.mu.Unlock()
	if edit != nil {
		edit(fixture)
	}
	return fixture
}

// items reads fixture[group][key] as a list; a missing key is an empty list.
func items(fixture map[string]any, group, key string) []map[string]any {
	byKey, _ := fixture[group].(map[string]any)
	list, _ := byKey[key].([]any)
	out := []map[string]any{}
	for _, item := range list {
		out = append(out, item.(map[string]any))
	}
	return out
}

func (f *fakeFreshdesk) serveSolutions(w http.ResponseWriter, r *http.Request, parts []string) {
	fixture := f.solutionsFixture()
	switch {
	case r.URL.Path == "/api/v2/settings/helpdesk":
		writeJSON(w, http.StatusOK, fixture["settings"])
	case len(parts) == 2 && parts[1] == "categories":
		f.page(w, r, items(map[string]any{"all": map[string]any{"categories": fixture["categories"]}}, "all", "categories"))
	case len(parts) == 4 && parts[1] == "categories" && parts[3] == "folders":
		f.page(w, r, items(fixture, "category_folders", parts[2]))
	case len(parts) == 4 && parts[1] == "folders" && parts[3] == "subfolders":
		f.page(w, r, items(fixture, "subfolders", parts[2]))
	case len(parts) == 4 && parts[1] == "folders" && parts[3] == "articles":
		f.page(w, r, items(fixture, "articles", parts[2]))
	case len(parts) == 5 && parts[1] == "folders" && parts[3] == "articles":
		byLanguage, _ := fixture["translations"].(map[string]any)
		language, _ := byLanguage[parts[4]].(map[string]any)
		f.page(w, r, items(map[string]any{"t": language}, "t", parts[2]))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}
}

func (f *fakeFreshdesk) cannedFolders() []map[string]any {
	if !f.canned {
		return []map[string]any{}
	}
	folders := f.list("canned-folders.json")
	folders[0]["responses_count"] = folders[0]["responses_count"].(float64) + float64(len(f.cannedExtra)+f.cannedCountBoost)
	return folders
}

func (f *fakeFreshdesk) cannedResponses(folder string) []map[string]any {
	if !f.canned {
		return []map[string]any{}
	}
	items := f.list("canned-responses-" + folder + ".json")
	if folder == "801" {
		items = append(items, f.cannedExtra...)
	}
	return items
}

func (f *fakeFreshdesk) search(w http.ResponseWriter, r *http.Request) {
	if f.failSearch {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid query"})
		return
	}
	query := r.URL.Query().Get("query")
	total := 0
	switch {
	case strings.HasSuffix(r.URL.Path, "/tickets"):
		total = 5 // live tickets 1, 2, 4, 6 and trashed 5; archived are excluded
		if f.allArchived {
			total = 1
		}
		if f.noTickets {
			total = 0
		}
		if f.searchTicketTotal >= 0 {
			total = f.searchTicketTotal
		}
		if query != `"created_at:>'2000-01-01'"` {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unexpected query " + query})
			return
		}
	case query == `"active:true"`:
		total = 2
	case query == `"active:false"`:
		total = 1
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unexpected query " + query})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "results": []any{}})
}

func (f *fakeFreshdesk) archived(w http.ResponseWriter, id string, n int) {
	switch {
	case f.archiveForbidden:
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "archive is not enabled"})
	case f.archiveError == id:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "internal error"})
	case f.allArchived && id != "4" && id != "5":
		if item := f.listedTicket(id); item != nil {
			writeJSON(w, http.StatusOK, f.withFiles([]map[string]any{item}, n)[0])
			return
		}
		f.object(w, "archived-"+id+".json")
	default:
		f.object(w, "archived-"+id+".json")
	}
}

func (f *fakeFreshdesk) listedTicket(id string) map[string]any {
	for _, item := range f.list("tickets.json") {
		if fmt.Sprint(item["id"]) == id {
			if (id == "6" && f.extraReplyOnTicket6) || (id == "1" && f.ticket1Updated) {
				item["updated_at"] = "2025-04-05T10:00:00Z"
			}
			return item
		}
	}
	return nil
}

func (f *fakeFreshdesk) ticket(w http.ResponseWriter, id string, n int) {
	if id == "5" {
		f.object(w, "trashed-5.json")
		return
	}
	if item := f.listedTicket(id); item != nil && id != "4" && !f.allArchived {
		writeJSON(w, http.StatusOK, f.withFiles([]map[string]any{item}, n)[0])
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
}

func (f *fakeFreshdesk) ticketList(since string) []map[string]any {
	from, err := time.Parse(time.RFC3339, since)
	if err != nil {
		f.t.Errorf("tickets listed without a valid updated_since: %q", since)
	}
	var items []map[string]any
	for _, item := range f.list("tickets.json") {
		if (fmt.Sprint(item["id"]) == "6" && f.extraReplyOnTicket6) || (fmt.Sprint(item["id"]) == "1" && f.ticket1Updated) {
			item["updated_at"] = "2025-04-05T10:00:00Z"
		}
		updated, _ := time.Parse(time.RFC3339, item["updated_at"].(string))
		if !updated.Before(from) {
			delete(item, "attachments")
			delete(item, "description")
			delete(item, "description_text")
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i]["updated_at"].(string) < items[j]["updated_at"].(string) })
	return items
}

// newestTickets answers a list ordered by created_at desc, as the preview asks for the newest ticket:
// ids grow with created_at in the fixture.
func (f *fakeFreshdesk) newestTickets(w http.ResponseWriter, r *http.Request) {
	items := f.ticketList(r.URL.Query().Get("updated_since"))
	sort.Slice(items, func(i, j int) bool { return items[i]["id"].(float64) > items[j]["id"].(float64) })
	if n, _ := strconv.Atoi(r.URL.Query().Get("per_page")); n > 0 && n < len(items) {
		items = items[:n]
	}
	writeJSON(w, http.StatusOK, items)
}

// withFiles points attachment URLs at the file host. The version number is
// how many times the parent was fetched; version 1 of file 9001 has expired.
func (f *fakeFreshdesk) withFiles(items []map[string]any, version int) []map[string]any {
	data, _ := json.Marshal(items)
	text := strings.ReplaceAll(string(data), "{{files}}/9001", fmt.Sprintf("%s/9001?v=%d", f.files.URL, version))
	text = strings.ReplaceAll(text, "{{files}}", f.files.URL)
	text = f.inlineMarkup(text, version)
	var out []map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return out
}

// inlineMarkup fills the image placeholders of the fixtures (inside JSON strings) or removes them.
func (f *fakeFreshdesk) inlineMarkup(text string, version int) string {
	images := map[string]string{"{{inline-files}}": "", "{{inline-account}}": "", "{{inline-external}}": ""}
	if f.inlineImages {
		images["{{inline-files}}"] = fmt.Sprintf(`<img src=\"%s/inline-5001?Signature=abc&amp;Expires=%d\">`, f.files.URL, version)
		if f.bigInlineImages {
			images["{{inline-files}}"] = fmt.Sprintf(`<img src=\"%[1]s/inline-6001\"><img src=\"%[1]s/inline-6002\"><img src=\"%[1]s/inline-6003\">`, f.files.URL)
		}
		account := f.api.URL
		if f.inlineHTTP {
			account = strings.Replace(account, "https://", "http://", 1)
		}
		images["{{inline-account}}"] = fmt.Sprintf(`<img src=\"%s/helpdesk/attachments/6001\">`, account)
		images["{{inline-external}}"] = fmt.Sprintf(`<img src=\"https://images.example.org/pixel.png\"> <img src=\"https://freshdesk.com.evil.example/a.png\"> <img src=\"https://evilfreshdesk.com/b.png\"> `+
			`<img data-src=\"%s/helpdesk/attachments/6002\" src=\"https://images.example.org/lazy.gif\">`, f.api.URL)
	}
	for token, markup := range images {
		text = strings.ReplaceAll(text, token, markup)
	}
	return text
}

func (f *fakeFreshdesk) page(w http.ResponseWriter, r *http.Request, items []map[string]any) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	start, end := (page-1)*2, page*2
	if start > len(items) {
		start = len(items)
	}
	if end >= len(items) {
		end = len(items)
	} else {
		next := *r.URL
		query := next.Query()
		query.Set("page", strconv.Itoa(page+1))
		next.RawQuery = query.Encode()
		w.Header().Set("Link", fmt.Sprintf("<https://%s%s>; rel=\"next\"", r.Host, next.RequestURI()))
	}
	writeJSON(w, http.StatusOK, items[start:end])
}

func (f *fakeFreshdesk) object(w http.ResponseWriter, name string) {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (f *fakeFreshdesk) list(name string) []map[string]any {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		return []map[string]any{}
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		f.t.Fatalf("fixture %s: %v", name, err)
	}
	return items
}

func (f *fakeFreshdesk) serveFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/")
	f.hit("/files/" + id)
	f.mu.Lock()
	if r.Header.Get("Authorization") != "" {
		f.fileAuthSeen = true
	}
	failing, redirect, stalling := f.failAttachment == id, f.redirectFile == id, f.stallFile == id
	body, overridden := f.fileBody[id]
	contentType, status := f.fileType[id], f.fileStatus[id]
	length := f.fileLength[id]
	f.mu.Unlock()
	switch {
	case id == "9001" && r.URL.Query().Get("v") == "1":
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<Error><Code>AccessDenied</Code><Message>Request has expired</Message></Error>"))
	case status != 0:
		http.Error(w, "refused", status)
	case failing:
		http.Error(w, "internal error", http.StatusInternalServerError)
	case redirect:
		http.Redirect(w, r, f.internal.URL+"/latest/meta-data", http.StatusFound)
	case stalling:
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("log"))
		w.(http.Flusher).Flush()
		f.stall(r)
	case overridden:
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		} else if strings.HasPrefix(id, "inline-") {
			w.Header().Set("Content-Type", "image/png")
		}
		if length != "" {
			w.Header().Set("Content-Length", length)
		}
		_, _ = w.Write([]byte(body))
	case fileBodies[id] != "":
		w.Header().Set("Content-Type", "application/octet-stream")
		if strings.HasPrefix(id, "inline-") {
			w.Header().Set("Content-Type", "image/png")
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write([]byte(fileBodies[id]))
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
