package helpcenter

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

func TestArticleMarkdownNeverRendersScriptOrUnsafeHTML(t *testing.T) {
	cases := map[string]string{
		"raw script":             "Hello <script>alert(1)</script>",
		"raw block":              "<div onclick=\"alert(1)\">x</div>",
		"javascript link":        "[click](javascript:alert(1))",
		"mixed case scheme":      "[click](JaVaScRiPt:alert(1))",
		"scheme with tab":        "[click](java\tscript:alert(1))",
		"entity in scheme":       "[click](java&#x09;script:alert(1))",
		"data link":              "[click](data:text/html;base64,PHNjcmlwdD4=)",
		"vbscript autolink":      "<vbscript:msgbox(1)>",
		"unknown autolink":       "<intent:evil>",
		"javascript image":       "![x](javascript:alert(1))",
		"onerror in alt":         "![\" onerror=\"alert(1)](https://example.com/a.png)",
		"style attribute table":  "| a |\n|:-:|\n| b |",
		"iframe of another site": "<iframe src=\"https://evil.example/embed/x\"></iframe>",
		"iframe javascript":      "<iframe src=\"javascript:alert(1)\"></iframe>",
		"iframe data":            "<iframe src=\"data:text/html,<script>alert(1)</script>\"></iframe>",
		"iframe srcdoc":          "<iframe srcdoc=\"<script>alert(1)</script>\"></iframe>",
		"svg onload":             "<svg onload=\"alert(1)\"><script>alert(1)</script></svg>",
		"object and embed":       "<object data=\"x.swf\"></object>\n\n<embed src=\"x.swf\">",
		"style element":          "<style>body{display:none}</style>",
		"inline image onerror":   "Hi <img src=x onerror=\"alert(1)\"> there",
		"outside video":          "<video src=\"https://evil.example/a.mp4\" autoplay controls></video>",
		"inline html link":       "See <a href=\"javascript:alert(1)\">this</a>.",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			html, err := renderArticle(body, "/help/acme")
			if err != nil {
				t.Fatal(err)
			}
			lower := strings.ToLower(string(html))
			for _, bad := range []string{"<script", "onclick", "onerror=\"", "onload=\"", "style", "<div", "<svg", "<object", "<embed", "<img src=\"x", "<video", "evil.example", "srcdoc", "<a href=\"javascript"} {
				if strings.Contains(lower, bad) {
					t.Errorf("%q rendered %q, which holds %q", body, html, bad)
				}
			}
			for _, scheme := range []string{"javascript", "vbscript", "data", "intent", "java"} {
				for _, attribute := range []string{"href=\"", "src=\""} {
					if strings.Contains(lower, attribute+scheme) {
						t.Errorf("%q rendered %q, which links %s", body, html, scheme)
					}
				}
			}
		})
	}
}

func TestArticleMarkdownShowsAllowedPlayersAndNoOtherHTML(t *testing.T) {
	html, err := renderArticle("Intro\n\n<iframe src=\"https://www.youtube.com/embed/jNQXAC9IVRw?start=65\" title=\"Tour\" allowfullscreen></iframe>\n\nWatch <iframe src=\"https://player.vimeo.com/video/76979871\"></iframe> inline, <b>bold</b> stays text.\n\n<div class=\"note\">\nNote <span>here</span>\n</div>\n\n<video src=\"/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b\" controls></video>", "/help/acme")
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	forced := ` sandbox="allow-scripts allow-same-origin allow-presentation allow-popups" referrerpolicy="strict-origin-when-cross-origin" loading="lazy"></iframe>`
	for _, want := range []string{
		`<iframe src="https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65" title="Tour" allow="encrypted-media; picture-in-picture" allowfullscreen=""` + forced,
		`<p>Watch <iframe src="https://player.vimeo.com/video/76979871" title="Vimeo video" allow="encrypted-media; picture-in-picture"` + forced + ` inline, bold stays text.</p>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %s\nwant it to hold %s", got, want)
		}
	}
	// A library video waits for the media library: until then the page shows no broken player.
	// Other HTML goes with its text, as it always did; words between inline tags are Markdown and stay.
	for _, bad := range []string{"<b>", "<div", "<span", "Note", "here", "<video", "/api/v1/", "youtube.com/"} {
		if strings.Contains(got, bad) {
			t.Errorf("rendered %s, which holds %s", got, bad)
		}
	}
}

func TestArticleMarkdownServesKnowledgeBaseImagesFromTheHelpCenter(t *testing.T) {
	id := "0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	html, err := renderArticle("![Settings](/api/v1/kb/images/"+id+")\n\n![Logo](https://cdn.example.com/logo.png)\n\n[download](/api/v1/kb/images/"+id+")", "https://acme.help.example")
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{
		`<img src="https://acme.help.example/images/` + id + `" alt="Settings" loading="lazy">`,
		`<a href="https://cdn.example.com/logo.png">Logo</a>`,
		`<a href="https://acme.help.example/images/` + id + `">download</a>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %s\nwant it to hold %s", got, want)
		}
	}
	if strings.Contains(got, "/api/v1/") || strings.Contains(got, "<img src=\"https://cdn") {
		t.Errorf("rendered %s: an API path or an outside image is left", got)
	}
}

func TestArticleMarkdownKeepsEverydayFormatting(t *testing.T) {
	html, err := renderArticle("## Reset your password\n\n1. Open **Settings**\n2. Choose ~~old~~ new\n\n- [x] done\n\nVisit https://example.com or [mail us](mailto:help@example.com).\n\n```\ncode <b>\n```", "")
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{`<h2 id="reset-your-password">`, "<strong>Settings</strong>", "<del>old</del>", `type="checkbox"`,
		`<a href="https://example.com">https://example.com</a>`, `<a href="mailto:help@example.com">mail us</a>`, "code &lt;b&gt;"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered %s\nwant it to hold %s", got, want)
		}
	}
}

func TestExcerptIsPlainTextCutAtAWord(t *testing.T) {
	got := excerpt("# Title\n\nOpen **Settings** and [choose](https://x.example) the plan.\n\n```\nsecret code\n```\n\nMore words follow here.", 40)
	if got != "Title Open Settings and choose the plan…" {
		t.Fatalf("excerpt=%q", got)
	}
	if got := excerpt("Sign in as an **admin**.\nThen `open` it.", 80); got != "Sign in as an admin. Then open it." {
		t.Fatalf("inline excerpt=%q", got)
	}
	if got := excerpt("Short text.", 40); got != "Short text." {
		t.Fatalf("short excerpt=%q", got)
	}
}

// One line of quote or list markers nests as deep as it is long, and the Markdown parser takes
// quadratic time in that depth (seconds at 100,000 characters). Past 16 levels on a line, a marker is
// shown as text.
func TestArticleMarkdownNestsAtMost16LevelsOnALine(t *testing.T) {
	html, err := renderArticle(strings.Repeat("> ", 16)+"deep\n\n"+strings.Repeat(">", 18)+" deeper\n\n```\n"+strings.Repeat(">", 20)+"\n```", "")
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if count := strings.Count(got, "<blockquote>"); count != 32 {
		t.Errorf("rendered %d quotes, want 16 + 16:\n%s", count, got)
	}
	if !strings.Contains(got, "<p>&gt;&gt; deeper</p>") || !strings.Contains(got, "<code>"+strings.Repeat("&gt;", 20)+"\n</code>") {
		t.Errorf("rendered %s", got)
	}
	links := strings.Repeat("[a](b) ", 2000) + strings.Repeat("[c](", 17) + "[d](e"
	if html, _ := renderArticle(links, ""); strings.Count(string(html), "<a href") != 2000 || !strings.Contains(string(html), strings.Repeat("[c](", 17)+"[d](e") {
		t.Errorf("links rendered %s", html)
	}
	if html, _ := renderArticle(strings.Repeat("1. ", 17)+"x", ""); strings.Count(string(html), "<ol>") != 16 || !strings.Contains(string(html), "1. x") {
		t.Errorf("ordered lists rendered %s", html)
	}
	for name, body := range map[string]string{
		"quotes":        strings.Repeat(">", 100000),
		"spaced quotes": strings.Repeat("> ", 50000),
		"ordered lists": strings.Repeat("1. ", 33333),
		"bullet lists":  strings.Repeat("+ ", 50000),
		"mixed":         strings.Repeat("> 1) - ", 14285),
		"links":         strings.Repeat("[a](", 25000),
		"images":        strings.Repeat("![](", 25000),
		"closed links":  strings.Repeat("[a](", 25000) + ")",
		"nested links":  strings.Repeat("[a](()", 16666) + ")",
	} {
		started := time.Now()
		if _, err := renderArticle(body, ""); err != nil {
			t.Fatal(err)
		}
		excerpt(body, descriptionChars)
		if took := time.Since(started); took > 150*time.Millisecond {
			t.Errorf("%s took %s", name, took)
		}
	}
}

// TestArticlesRenderAsThePreviewDoes runs kbhtml/testdata/articles.json, which the editor preview runs too.
func TestArticlesRenderAsThePreviewDoes(t *testing.T) {
	raw, err := os.ReadFile("../kbhtml/testdata/articles.json")
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Cases []struct {
			Name, Markdown, Text string
			Paragraphs, Iframes  int
		}
	}
	if err := json.Unmarshal(raw, &shared); err != nil || len(shared.Cases) == 0 {
		t.Fatalf("read the cases: %v", err)
	}
	for _, c := range shared.Cases {
		t.Run(c.Name, func(t *testing.T) {
			out, err := renderArticle(c.Markdown, "")
			if err != nil {
				t.Fatal(err)
			}
			nodes, err := html.ParseFragment(strings.NewReader(string(out)), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			counts := map[string]int{}
			var walk func(*html.Node)
			walk = func(node *html.Node) {
				if node.Type == html.TextNode {
					text.WriteString(node.Data)
				}
				if node.Type == html.ElementNode {
					counts[node.Data]++
					// Text inside an iframe is its fallback, which a browser never shows.
					if node.Data == "iframe" {
						return
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			for _, node := range nodes {
				walk(node)
			}
			if got := strings.Join(strings.Fields(text.String()), " "); got != c.Text {
				t.Errorf("text %q, want %q\n%s", got, c.Text, out)
			}
			if counts["p"] != c.Paragraphs || counts["iframe"] != c.Iframes {
				t.Errorf("%d paragraphs and %d iframes, want %d and %d\n%s", counts["p"], counts["iframe"], c.Paragraphs, c.Iframes, out)
			}
		})
	}
}

// Heading ids are goldmark's (a, a-1, a-2…), so an anchor link keeps working, without its quadratic
// search for a free suffix: 25,000 headings of one name took 24 s.
func TestHeadingIDsAreGoldmarksInLinearTime(t *testing.T) {
	headings := "# a\n# a\n# a-1\n# A\n# a\n# \n# !!\n# a 1\n## a-1\nb\n===\n# b\n# heading\n# Ünïcode é\n# a_b\n"
	want := regexp.MustCompile(` id="[^"]*"`).FindAllString(func() string {
		var out bytes.Buffer
		if err := goldmark.New(goldmark.WithParserOptions(parser.WithAutoHeadingID())).Convert([]byte(headings), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}(), -1)
	html, err := renderArticle(headings, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := regexp.MustCompile(` id="[^"]*"`).FindAllString(string(html), -1); strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("ids %q\nwant %q", got, want)
	}
	for name, body := range map[string]string{"atx": strings.Repeat("# a\n", 25000), "setext": strings.Repeat("a\n---\n", 16000)} {
		started := time.Now()
		if _, err := renderArticle(body, ""); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(started); took > 300*time.Millisecond {
			t.Errorf("%s headings took %s", name, took)
		}
	}
}
