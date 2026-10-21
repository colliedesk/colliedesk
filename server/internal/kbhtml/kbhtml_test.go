package kbhtml_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/kbhtml"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

type fixture struct {
	Cases []struct {
		Name    string   `json:"name"`
		HTML    string   `json:"html"`
		Iframes []string `json:"iframes"`
		Videos  []string `json:"videos"`
		Dropped []string `json:"dropped"`
		Titles  []string `json:"titles"`
	} `json:"cases"`
}

// shown is what a browser makes of out: the address of every iframe with its attributes, the
// elements, and the visible text with whitespace collapsed.
type shown struct {
	iframes  []string
	videos   []string
	attrs    []map[string]string
	elements []string
	text     string
}

func parse(t *testing.T, out string) shown {
	t.Helper()
	nodes, err := html.ParseFragment(strings.NewReader(out), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	var result shown
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		switch node.Type {
		case html.TextNode:
			text.WriteString(node.Data)
		case html.ElementNode:
			result.elements = append(result.elements, node.Data)
			if node.Data == "iframe" {
				attrs := map[string]string{}
				for _, attr := range node.Attr {
					attrs[attr.Key] = attr.Val
				}
				result.iframes = append(result.iframes, attrs["src"])
				result.attrs = append(result.attrs, attrs)
				return
			}
			if node.Data == "video" {
				for _, attr := range node.Attr {
					if attr.Key == "src" {
						result.videos = append(result.videos, attr.Val)
					}
				}
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
	result.text = strings.Join(strings.Fields(text.String()), " ")
	return result
}

// TestFragmentFollowsTheSharedCases runs testdata/cases.json, which the editor preview runs too.
func TestFragmentFollowsTheSharedCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases fixture
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases.Cases {
		t.Run(c.Name, func(t *testing.T) {
			out, dropped := kbhtml.Fragment(c.HTML)
			got := parse(t, out)
			if !reflect.DeepEqual(append([]string{}, got.iframes...), append([]string{}, c.Iframes...)) {
				t.Errorf("iframes %q, want %q (out %s)", got.iframes, c.Iframes, out)
			}
			if !reflect.DeepEqual(append([]string{}, got.videos...), append([]string{}, c.Videos...)) {
				t.Errorf("videos %q, want %q (out %s)", got.videos, c.Videos, out)
			}
			if got.text != "" {
				t.Errorf("shows the text %q (out %s)", got.text, out)
			}
			if !reflect.DeepEqual(append([]string{}, dropped...), append([]string{}, c.Dropped...)) {
				t.Errorf("dropped %q, want %q", dropped, c.Dropped)
			}
			if c.Titles != nil {
				var titles []string
				for _, attrs := range got.attrs {
					titles = append(titles, attrs["title"])
				}
				if !reflect.DeepEqual(titles, c.Titles) {
					t.Errorf("titles %q, want %q (out %s)", titles, c.Titles, out)
				}
			}
			for _, element := range got.elements {
				if element != "iframe" && element != "video" {
					t.Errorf("out %s holds <%s>", out, element)
				}
			}
			for _, attrs := range got.attrs {
				if attrs["sandbox"] != "allow-scripts allow-same-origin allow-presentation allow-popups" ||
					attrs["referrerpolicy"] != "strict-origin-when-cross-origin" || attrs["loading"] != "lazy" {
					t.Errorf("iframe %v lacks the forced attributes", attrs)
				}
				for key := range attrs {
					switch key {
					case "src", "title", "width", "height", "allow", "allowfullscreen", "loading", "sandbox", "referrerpolicy":
					default:
						t.Errorf("iframe keeps %s=%q", key, attrs[key])
					}
				}
			}
		})
	}
}

func TestFragmentRebuildsAPlayerWithOnlyItsAllowedAttributes(t *testing.T) {
	out, dropped := kbhtml.Fragment(`<IFRAME SRC="https://www.youtube.com/embed/jNQXAC9IVRw?start=65" title='Me &amp; "you" <b>' width="560" height="315" allow="fullscreen; encrypted-media; camera" allowfullscreen></IFRAME>`)
	want := `<iframe src="https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65" title="Me &amp; &#34;you&#34; &lt;b&gt;" width="560" height="315" allow="encrypted-media" allowfullscreen="" sandbox="allow-scripts allow-same-origin allow-presentation allow-popups" referrerpolicy="strict-origin-when-cross-origin" loading="lazy"></iframe>`
	if out != want {
		t.Errorf("out\n%s\nwant\n%s", out, want)
	}
	if !reflect.DeepEqual(dropped, []string{"iframe allow camera"}) {
		t.Errorf("dropped %q", dropped)
	}
}

func TestFragmentGivesAPlayerWithoutATitleOrAllowTheSiteDefaults(t *testing.T) {
	out, _ := kbhtml.Fragment(`<iframe src="https://player.vimeo.com/video/1"></iframe>`)
	if !strings.Contains(out, ` title="Vimeo video" allow="encrypted-media; picture-in-picture"`) {
		t.Errorf("out %s has no title or allow", out)
	}
}

// A <video> of the media library keeps only its allowed attributes; with the rule off, nothing.
func TestVideoRuleKeepsALibraryVideoOnly(t *testing.T) {
	rules := kbhtml.Load()
	id := "/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	out, dropped := rules.Fragment(`<video src="` + id + `" poster="` + id + `/poster" title="Tour" aria-label="Product tour" width="640" preload="auto" autoplay muted playsinline onplay="x()"><source src="https://evil.example/a.mp4"><track src="x"></video>`)
	want := `<video src="` + id + `" title="Tour" aria-label="Product tour" width="640" preload="metadata" muted="" playsinline="" controls=""></video>`
	if out != want {
		t.Errorf("out\n%s\nwant\n%s", out, want)
	}
	if !reflect.DeepEqual(dropped, []string{"video preload=auto", "video poster", "video autoplay", "video onplay", "source", "track"}) {
		t.Errorf("dropped %q", dropped)
	}
	for _, bad := range []string{
		`<video src="` + id + `X"></video>`,
		`<video src="https://evil.example/a.mp4"></video>`,
		`<video src="` + strings.ToUpper(id) + `"></video>`,
		`<video src="` + id + `?x=1"></video>`,
	} {
		if out, _ := rules.Fragment(bad); strings.Contains(out, "<video") {
			t.Errorf("%s rendered %s", bad, out)
		}
	}
	rules.Video.Enabled = false
	if out, _ := rules.Fragment(`<video src="` + id + `"></video>`); out != "" {
		t.Errorf("with the rule off a video rendered %s", out)
	}
}

// A page that knows the library plays the video from the address the library gives, with its
// poster, and its size when the author gave none; a video the library does not know is left out.
func TestFragmentInPlaysFromTheLibrary(t *testing.T) {
	id := "0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"
	library := func(want string) (kbhtml.Video, bool) {
		if want != id {
			return kbhtml.Video{}, false
		}
		return kbhtml.Video{Src: "/help/acme/media/" + id, Poster: `/help/acme/media/` + id + `/poster/"x<`, Width: 1280, Height: 720}, true
	}
	out, dropped := kbhtml.FragmentIn(`<video src="/api/v1/kb/media/`+id+`" muted></video><video src="/api/v1/kb/media/`+id+`" width="320"></video><video src="/api/v1/kb/media/1b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b"></video>`, library)
	want := `<video src="/help/acme/media/` + id + `" preload="metadata" muted="" poster="/help/acme/media/` + id + `/poster/&#34;x&lt;" width="1280" height="720" controls=""></video>` +
		`<video src="/help/acme/media/` + id + `" width="320" preload="metadata" poster="/help/acme/media/` + id + `/poster/&#34;x&lt;" controls=""></video>`
	if out != want {
		t.Errorf("out\n%s\nwant\n%s", out, want)
	}
	if !reflect.DeepEqual(dropped, []string{"video (not in the library)"}) {
		t.Errorf("dropped %q", dropped)
	}
}

// The Freshdesk import keeps a player as the editor writes it, and the help center keeps all of it.
func TestPlayerSnippetKeepsAPlayerTheHelpCenterShowsWhole(t *testing.T) {
	// YouTube's Share → Embed code.
	embed := `<iframe width="560" height="315" src="https://www.youtube.com/embed/jNQXAC9IVRw?si=abc&amp;start=7" title="YouTube video player" frameborder="0" allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share" referrerpolicy="strict-origin-when-cross-origin" allowfullscreen></iframe>`
	nodes, err := html.ParseFragment(strings.NewReader(embed), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		t.Fatal(err)
	}
	snippet, ok := kbhtml.PlayerSnippet(nodes[0])
	want := `<iframe src="https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=7" title="YouTube video player" width="560" height="315" allow="accelerometer; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share" allowfullscreen="" loading="lazy"></iframe>`
	if !ok || snippet != want {
		t.Fatalf("snippet %q %v\nwant %q", snippet, ok, want)
	}
	if _, dropped := kbhtml.Fragment(snippet); len(dropped) != 0 {
		t.Errorf("the help center leaves out %q of the snippet", dropped)
	}
	for _, other := range []string{`<iframe src="https://evil.example/embed/x"></iframe>`, `<iframe srcdoc="x"></iframe>`, `<video src="x"></video>`} {
		nodes, _ := html.ParseFragment(strings.NewReader(other), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
		if snippet, ok := kbhtml.PlayerSnippet(nodes[0]); ok {
			t.Errorf("%s kept as %s", other, snippet)
		}
	}
}
