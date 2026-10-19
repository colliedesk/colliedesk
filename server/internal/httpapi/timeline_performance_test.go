package httpapi_test

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// A ticket opens on its message list, so that read must stay fast with real mail: a long thread of HTML mail
// with many remote images each, rewritten to proxy addresses on the way out, and no call to any image host.
// p95 at or below 100 ms, the budget of the other inbox reads (docs/internals/performance.md).
func TestTimelineReadWithHeavyHTMLMailStaysInBudget(t *testing.T) {
	host := newImageHost(t, serveBytes("image/png", tinyPNG))
	desk := newLimitDesk(t, allowing(host), nil)
	admin, _ := desk.workspace(t, "Timeline Perf", "timeline-perf.example.com")
	contactID := create(t, desk.api, admin, "/api/v1/contacts", map[string]any{"name": "Perf Contact", "email": "perf@sample.example"})
	conversationID := create(t, desk.api, admin, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Long thread"})
	var html strings.Builder
	for i := 0; i < 30; i++ {
		html.WriteString(`<p>` + strings.Repeat("some text ", 40) + `</p><img src="` + host.URL + `/chart-` + string(rune('a'+i%26)) + `.png" width="600" alt="chart">`)
	}
	if _, err := desk.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,body_html,email_meta)
		SELECT workspace_id,id,'incoming','Body '||g,$2||'<!-- '||g||' -->','{"from":"c@customer.example","authentication":"pass","via":"platform"}'
		FROM conversations, generate_series(1,60) g WHERE id=$1`, conversationID, html.String()); err != nil {
		t.Fatal(err)
	}
	var samples []time.Duration
	for i := 0; i < 40; i++ {
		started := time.Now()
		response := request(t, desk.api, http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, admin, nil)
		samples = append(samples, time.Since(started))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/api/v1/mail-image?u=") {
			t.Fatalf("status=%d", response.Code)
		}
	}
	sort.Slice(samples, func(a, b int) bool { return samples[a] < samples[b] })
	if p95 := samples[len(samples)*95/100]; p95 > 100*time.Millisecond {
		t.Fatalf("60 messages with 30 images each: p95 %v, budget 100ms", p95)
	}
	if host.hits.Load() != 0 {
		t.Fatalf("reading the timeline called an image host %d times", host.hits.Load())
	}
	t.Logf("p95 %v", samples[len(samples)*95/100])
}
