package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/channels/zalo"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/google"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5/pgxpool"
)

type numbered struct {
	ID           string `json:"id"`
	Number       int64  `json:"number"`
	NumberSeries string `json:"number_series"`
	DisplayRef   string `json:"display_ref"`
}

// Every conversation gets the next number of its series: the default one shows "#n"; a channel can open
// its conversations in a series of its own with its own code, which customers type back and the inbox
// finds. A series only counts on: its next number never goes back.
func TestConversationsAreNumberedBySeriesAndFoundByTheirCode(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		keys, _ := tokenseal.ParseKeys("k1:AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=")
		config.Google = google.Config{Keys: keys}
	})
	admin := createSession(t, api, service, "Numbered Desk", "admin@numbered.example.com")
	other := createSession(t, api, service, "Numbered Other", "admin@numbered-other.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@numbered.example.com"})
	get := func(token, path string) numbered {
		t.Helper()
		var item numbered
		response := request(t, api, http.MethodGet, path, nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
		}
		decode(t, response, &item)
		return item
	}
	// Many at once: none share a number.
	var mu sync.Mutex
	var wg sync.WaitGroup
	numbers := map[int64]bool{}
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer"})
			item := get(admin, "/api/v1/conversations/"+id)
			mu.Lock()
			numbers[item.Number] = true
			mu.Unlock()
			if item.NumberSeries != "default" || item.DisplayRef != fmt.Sprintf("#%d", item.Number) {
				t.Errorf("default numbering = %+v", item)
			}
		}()
	}
	wg.Wait()
	for n := int64(1); n <= 12; n++ {
		if !numbers[n] {
			t.Fatalf("numbers = %v, want 1 to 12 once each", numbers)
		}
	}
	// A series of the channel's own, counting on from where another system stopped.
	put := func(token, series string, body map[string]any) *httptest.ResponseRecorder {
		return request(t, api, http.MethodPut, "/api/v1/conversation-number-series/"+series, body, token, nil)
	}
	if response := put(admin, "ref", map[string]any{"format": "REF-{yyyy}-{n:04}", "next": 170, "start": 170}); response.Code != http.StatusOK {
		t.Fatalf("put series: %d %s", response.Code, response.Body.String())
	}
	for _, c := range []struct {
		body map[string]any
		code int
	}{
		{map[string]any{"format": "REF-{yyyy}-{n:04}", "next": 169}, http.StatusBadRequest},
		{map[string]any{"format": "REF {n}"}, http.StatusBadRequest},
	} {
		if response := put(admin, "ref", c.body); response.Code != c.code {
			t.Fatalf("put %v: %d %s", c.body, response.Code, response.Body.String())
		}
	}
	d := zaloDesk{api: api, pool: pool, adminA: admin, oaA: fmt.Sprint(300000 + time.Now().UnixNano()%100000)}
	d.channelA = d.connect(t, admin, d.oaA, "oa-secret-a")
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+d.channelA, map[string]any{"settings": map[string]any{"number_series": "ref"}}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("channel series: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+d.channelA, map[string]any{"settings": map[string]any{"number_series": "missing"}}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an unknown series: %d", response.Code)
	}
	timestamp := fmt.Sprint(time.Now().UnixMilli())
	body, _ := json.Marshal(map[string]any{"app_id": "700001", "event_name": "user_send_text", "timestamp": timestamp,
		"sender": map[string]string{"id": "1110001"}, "recipient": map[string]string{"id": d.oaA}, "message": map[string]string{"msg_id": "n-1", "text": "the printer jams"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/channels/zalo/webhook", bytes.NewReader(body))
	req.Header.Set(zalo.SignatureHeader, "mac="+zalo.Sign("700001", body, timestamp, "oa-secret-a"))
	if response := serve(api, req); response.Code != http.StatusOK {
		t.Fatalf("delivery: %d", response.Code)
	}
	processJournal(t, pool)
	year := time.Now().UTC().Year()
	zaloConversation := queryString(t, pool, `SELECT conversation_id::text FROM messages WHERE external_id='zalo:n-1'`)
	if item := get(admin, "/api/v1/conversations/"+zaloConversation); item.DisplayRef != fmt.Sprintf("REF-%d-0170", year) || item.Number != 170 || item.NumberSeries != "ref" {
		t.Fatalf("the channel's numbering = %+v", item)
	}
	// Found by the code as typed, in this workspace only; a code of another year is another conversation.
	find := func(token, ref string) []string {
		t.Helper()
		var page struct {
			Items []numbered `json:"items"`
		}
		response := request(t, api, http.MethodGet, "/api/v1/conversations?ref="+ref, nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("?ref=%s: %d %s", ref, response.Code, response.Body.String())
		}
		decode(t, response, &page)
		var ids []string
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		return ids
	}
	for _, ref := range []string{fmt.Sprintf("REF-%d-0170", year), fmt.Sprintf("ref-%d-170", year)} {
		if got := find(admin, ref); len(got) != 1 || got[0] != zaloConversation {
			t.Fatalf("?ref=%s = %v", ref, got)
		}
	}
	for _, ref := range []string{fmt.Sprintf("REF-%d-0170", year-1), "nonsense", "%2312"} {
		if got := find(other, ref); len(got) != 0 {
			t.Fatalf("another workspace found %s: %v", ref, got)
		}
	}
	if got := find(admin, fmt.Sprintf("REF-%d-0170", year-1)); len(got) != 0 {
		t.Fatalf("another year's code found %v", got)
	}
	if got := find(admin, "%233"); len(got) != 1 {
		t.Fatalf("#3 = %v", got)
	}
	// A search for a code finds that conversation first, ahead of others that only mention it.
	code := fmt.Sprintf("REF-%d-0170", year)
	create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "About " + code + " again"})
	search := func(token, words string) []string {
		t.Helper()
		var page struct {
			Items []numbered `json:"items"`
		}
		response := request(t, api, http.MethodGet, "/api/v1/conversations?search="+url.QueryEscape(words), nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("search %s: %d %s", words, response.Code, response.Body.String())
		}
		decode(t, response, &page)
		var refs []string
		for _, item := range page.Items {
			refs = append(refs, item.DisplayRef)
		}
		return refs
	}
	if got := search(admin, strings.ToLower(code)); len(got) != 2 || got[0] != code {
		t.Fatalf("search %s = %v, want it first then the one that mentions it", code, got)
	}
	if got := search(admin, "#3"); len(got) == 0 || got[0] != "#3" {
		t.Fatalf("search #3 = %v", got)
	}
	// Another series' code is not this one: #170 is no conversation of the default series.
	if got := search(admin, "#170"); len(got) != 0 {
		t.Fatalf("search #170 = %v", got)
	}
	if got := search(other, code); len(got) != 0 {
		t.Fatalf("another workspace's search found %v", got)
	}
	// The series are listed with what comes next.
	response := request(t, api, http.MethodGet, "/api/v1/conversation-number-series", nil, admin, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"series":"ref"`) || !strings.Contains(response.Body.String(), `"next":171`) {
		t.Fatalf("series: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodGet, "/api/v1/conversation-number-series", nil, other, nil); strings.Contains(response.Body.String(), `"ref"`) {
		t.Fatalf("another workspace sees the series: %s", response.Body.String())
	}
}

func processJournal(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	journal := pipeline.New(store.New(pool).WithChannels(registry.New(registry.Config{})), nil)
	if _, err := journal.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// The database renders and reads codes as domain.NumberFormat does: one format, one code.
func TestTheDatabaseRendersAndReadsCodesLikeTheirFormat(t *testing.T) {
	_, _, pool, _ := testAPIWithConfig(t, nil)
	for _, c := range []struct {
		format string
		n      int64
		year   int
	}{{"#{n}", 12, 2026}, {"REF-{yyyy}-{n:04}", 123, 2026}, {"REF-{yyyy}-{n:04}", 12345, 2027}, {"T{n:03}.{yyyy}", 7, 2025}, {"{yyyy}/{n:02}", 3, 2026}} {
		format, err := domain.ParseNumberFormat(c.format)
		if err != nil {
			t.Fatal(err)
		}
		want := format.Render(c.n, c.year)
		if got := queryString(t, pool, `SELECT conversation_display_ref($1,$2,$3)`, c.format, c.n, c.year); got != want {
			t.Fatalf("%s renders %d as %q in the database, %q in Go", c.format, c.n, got, want)
		}
		read := queryString(t, pool, `SELECT COALESCE((SELECT n||'/'||COALESCE(year::text,'-') FROM conversation_ref_read($1,$2)),'none')`, c.format, " "+strings.ToLower(want)+" ")
		wantRead := fmt.Sprintf("%d/%d", c.n, c.year)
		if !format.HasYear() {
			wantRead = fmt.Sprintf("%d/-", c.n)
		}
		if read != wantRead {
			t.Fatalf("%s reads %q as %s, want %s", c.format, want, read, wantRead)
		}
	}
	for code, want := range map[string]string{"REF-2026-0000123": "123/2026", "REF-2026-123": "123/2026", "REF-2026-": "none", "REF-26-0123": "none",
		"XREF-2026-0123": "none", "REF-2026-0123x": "none", "REF-2026-0": "none", "#12": "none", "": "none", "REF.2026-0123": "none"} {
		if got := queryString(t, pool, `SELECT COALESCE((SELECT n||'/'||COALESCE(year::text,'-') FROM conversation_ref_read('REF-{yyyy}-{n:04}',$1)),'none')`, code); got != want {
			t.Fatalf("%q reads as %s, want %s", code, got, want)
		}
	}
	if got := queryString(t, pool, `SELECT COALESCE((SELECT n::text FROM conversation_ref_read('T.{n}','TX5')),'none')`); got != "none" {
		t.Fatalf("a dot in a format is a dot, not any character: %s", got)
	}
}

// A conversation imported with its number moves the series past it, so the next one made never takes it:
// in a series whose code shows the year, only a number of this year does (another year's is another
// code); in one whose code does not, any number does. A code a customer was given stays theirs after the
// series' format changes, and a code in the new format names only what was opened in it.
func TestImportedNumbersAndFormatChangesKeepEveryCodeOne(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Imported Numbers", "admin@imported-numbers.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@imported-numbers.example.com"})
	workspace := queryString(t, pool, `SELECT workspace_id::text FROM contacts WHERE id=$1`, contact)
	year := time.Now().UTC().Year()
	if response := request(t, api, http.MethodPut, "/api/v1/conversation-number-series/ref", map[string]any{"format": "REF-{yyyy}-{n:04}", "next": 170}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("series: %d %s", response.Code, response.Body.String())
	}
	imported := func(series string, number, numberYear int) {
		t.Helper()
		if _, err := pool.Exec(context.Background(), `INSERT INTO conversations(workspace_id,contact_id,subject,channel,number_series,number,number_year)
			VALUES($1,$2,'Imported','imported',$3,$4,$5)`, workspace, contact, series, number, numberYear); err != nil {
			t.Fatalf("import %s %d/%d: %v", series, number, numberYear, err)
		}
	}
	next := func(series string) string {
		return queryString(t, pool, `SELECT next::text FROM conversation_number_series WHERE workspace_id=$1 AND series=$2`, workspace, series)
	}
	imported("default", 1, year)
	imported("default", 40, year-3)
	imported("ref", 180, year)
	imported("ref", 900, year-1)
	if got := next("default") + "," + next("ref"); got != "41,181" {
		t.Fatalf("next after the import = %s, want 41 (any year, its code shows none) and 181 (this year only)", got)
	}
	// Made after the import: numbered past it, never a duplicate.
	id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "New"})
	if got := queryString(t, pool, `SELECT display_ref FROM conversations WHERE id=$1`, id); got != "#41" {
		t.Fatalf("made after the import = %s", got)
	}
	// An import taking the number next would give is skipped over.
	imported("default", 42, year)
	id = create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Newer"})
	if got := queryString(t, pool, `SELECT display_ref FROM conversations WHERE id=$1`, id); got != "#43" {
		t.Fatalf("made after an import of the next number = %s", got)
	}
	// The format changes: the codes given keep finding their conversations; the new format names the new ones.
	if response := request(t, api, http.MethodPut, "/api/v1/conversation-number-series/ref", map[string]any{"format": "T{n:03}"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("format change: %d %s", response.Code, response.Body.String())
	}
	imported("ref", 181, year)
	find := func(ref string) string {
		t.Helper()
		var page struct {
			Items []numbered `json:"items"`
		}
		decode(t, request(t, api, http.MethodGet, "/api/v1/conversations?ref="+url.QueryEscape(ref), nil, admin, nil), &page)
		var refs []string
		for _, item := range page.Items {
			refs = append(refs, item.DisplayRef)
		}
		return strings.Join(refs, ",")
	}
	for ref, want := range map[string]string{
		fmt.Sprintf("REF-%d-0180", year): fmt.Sprintf("REF-%d-0180", year), fmt.Sprintf("ref-%d-180", year): fmt.Sprintf("REF-%d-0180", year),
		"T180": "", "T181": "T181", fmt.Sprintf("REF-%d-0181", year): "", "#41": "#41", "#40": "#40",
	} {
		if got := find(ref); got != want {
			t.Fatalf("?ref=%s = %q, want %q", ref, got, want)
		}
	}
}
