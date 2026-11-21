package pageview_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

const browser = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

func visit(method string, status int, header map[string]string, preview bool) pageview.Visit {
	h := http.Header{}
	for name, value := range header {
		h.Set(name, value)
	}
	return pageview.Visit{Method: method, Status: status, Header: h, Preview: preview}
}

func TestIsReader(t *testing.T) {
	cases := []struct {
		name  string
		visit pageview.Visit
		want  bool
	}{
		{"a browser", visit(http.MethodGet, 200, map[string]string{"User-Agent": browser}, false), true},
		{"a phone browser", visit(http.MethodGet, 200, map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 14; Cubot KingKong) AppleWebKit/537.36 Chrome/126.0 Mobile Safari/537.36"}, false), true},
		{"Googlebot", visit(http.MethodGet, 200, map[string]string{"User-Agent": "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"}, false), false},
		{"Bingbot", visit(http.MethodGet, 200, map[string]string{"User-Agent": "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)"}, false), false},
		{"an AI crawler", visit(http.MethodGet, 200, map[string]string{"User-Agent": "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)"}, false), false},
		{"a link preview", visit(http.MethodGet, 200, map[string]string{"User-Agent": "facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)"}, false), false},
		{"a headless browser", visit(http.MethodGet, 200, map[string]string{"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 HeadlessChrome/126.0 Safari/537.36"}, false), false},
		{"curl", visit(http.MethodGet, 200, map[string]string{"User-Agent": "curl/8.5.0"}, false), false},
		{"no user agent", visit(http.MethodGet, 200, nil, false), false},
		{"a HEAD", visit(http.MethodHead, 200, map[string]string{"User-Agent": browser}, false), false},
		{"a 304", visit(http.MethodGet, 304, map[string]string{"User-Agent": browser}, false), false},
		{"a 404", visit(http.MethodGet, 404, map[string]string{"User-Agent": browser}, false), false},
		{"a prefetch", visit(http.MethodGet, 200, map[string]string{"User-Agent": browser, "Sec-Purpose": "prefetch"}, false), false},
		{"a prerender", visit(http.MethodGet, 200, map[string]string{"User-Agent": browser, "Sec-Purpose": "prefetch;prerender"}, false), false},
		{"an old prefetch", visit(http.MethodGet, 200, map[string]string{"User-Agent": browser, "Purpose": "prefetch"}, false), false},
		{"a team preview", visit(http.MethodGet, 200, map[string]string{"User-Agent": browser}, true), false},
	}
	for _, c := range cases {
		if got := pageview.IsReader(c.visit); got != c.want {
			t.Errorf("%s: IsReader = %v, want %v", c.name, got, c.want)
		}
	}
}

type recorder struct{ events []pageview.Event }

func (r *recorder) PageViewed(event pageview.Event) { r.events = append(r.events, event) }

func TestFanoutGivesEveryConsumerTheEvent(t *testing.T) {
	first, second := &recorder{}, &recorder{}
	fanout := pageview.Fanout{first, nil, second}
	fanout.PageViewed(pageview.Event{Kind: pageview.Article, Address: "/a/1-x"})
	if len(first.events) != 1 || len(second.events) != 1 || second.events[0].Address != "/a/1-x" {
		t.Fatalf("first %v, second %v", first.events, second.events)
	}
}
