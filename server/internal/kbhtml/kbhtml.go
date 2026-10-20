// Package kbhtml decides what raw HTML in a knowledge base article may be shown: a YouTube or Vimeo
// player, rebuilt from its video id, and (once the media library ships) a <video> of the workspace's
// library. Every other element is left out, keeping its text. The rules are allowlist.json, which the
// editor preview reads too; testdata/cases.json holds the cases both must agree on.
// docs/internals/knowledge-base.md records why the list is this short.
package kbhtml

import (
	_ "embed"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

//go:embed allowlist.json
var allowlistJSON []byte

// Embed is one video site an iframe may show.
type Embed struct {
	Name  string            `json:"name"`
	Title string            `json:"title"`
	Hosts []string          `json:"hosts"`
	Path  string            `json:"path"`
	Src   string            `json:"src"`
	Query map[string]string `json:"query"`

	path  *regexp.Regexp
	query map[string]*regexp.Regexp
}

// Rules is allowlist.json, read.
type Rules struct {
	Iframe struct {
		Embeds     []*Embed `json:"embeds"`
		Attributes []string `json:"attributes"`
		Allow      []string `json:"allow"`
		Forced     struct {
			Sandbox        string `json:"sandbox"`
			ReferrerPolicy string `json:"referrerpolicy"`
			Loading        string `json:"loading"`
		} `json:"forced"`
	} `json:"iframe"`
	Video struct {
		Enabled    bool     `json:"enabled"`
		Attributes []string `json:"attributes"`
		Src        string   `json:"src"`
		Poster     string   `json:"poster"`
		Preload    []string `json:"preload"`
		Flags      []string `json:"flags"`

		src, poster *regexp.Regexp
	} `json:"video"`
	Dimension struct {
		Pattern string `json:"pattern"`
		Min     int    `json:"min"`
		Max     int    `json:"max"`
		pattern *regexp.Regexp
	} `json:"dimension"`
	DropWithContent []string `json:"dropWithContent"`

	policy *bluemonday.Policy
}

var rules = Load()

// Load reads allowlist.json. It panics on a broken file, which every test of this package catches.
func Load() *Rules {
	var r Rules
	if err := json.Unmarshal(allowlistJSON, &r); err != nil {
		panic("kbhtml: allowlist.json: " + err.Error())
	}
	var sources []string
	for _, embed := range r.Iframe.Embeds {
		embed.path = regexp.MustCompile(embed.Path)
		embed.query = map[string]*regexp.Regexp{}
		for key, pattern := range embed.Query {
			embed.query[key] = regexp.MustCompile(pattern)
		}
		sources = append(sources, regexp.QuoteMeta(embed.Src))
	}
	r.Video.src = regexp.MustCompile(r.Video.Src)
	r.Video.poster = regexp.MustCompile(r.Video.Poster)
	r.Dimension.pattern = regexp.MustCompile(r.Dimension.Pattern)

	// The second gate: what the rebuild below writes must pass bluemonday unchanged, or it is left
	// out. It allows the rebuilt shapes only, with the forced attributes added after it.
	policy := bluemonday.NewPolicy()
	policy.AllowURLSchemes("https")
	policy.RequireParseableURLs(true)
	policy.AllowRelativeURLs(true)
	policy.AllowAttrs("src").Matching(regexp.MustCompile(`^(?:` + strings.Join(sources, "|") + `)[0-9A-Za-z_-]+(?:\?[0-9a-z]+=[0-9A-Za-z]+(?:&[0-9a-z]+=[0-9A-Za-z]+)*)?$`)).OnElements("iframe")
	policy.AllowAttrs("title").OnElements("iframe")
	policy.AllowAttrs("allow").Matching(regexp.MustCompile(`^[a-z-]+(?:; [a-z-]+)*$`)).OnElements("iframe")
	policy.AllowAttrs("allowfullscreen").OnElements("iframe")
	policy.AllowAttrs("width", "height").Matching(r.Dimension.pattern).OnElements("iframe", "video")
	policy.AllowAttrs("src").Matching(r.Video.src).OnElements("video")
	policy.AllowAttrs("poster").Matching(r.Video.poster).OnElements("video")
	policy.AllowAttrs("preload").Matching(regexp.MustCompile(`^(?:` + strings.Join(r.Video.Preload, "|") + `)$`)).OnElements("video")
	policy.AllowAttrs(append([]string{"controls"}, r.Video.Flags...)...).OnElements("video")
	r.policy = policy
	return &r
}

// FrameSources is the origin of every player an article may frame, for the page's frame-src.
func FrameSources() []string {
	var origins []string
	for _, embed := range rules.Iframe.Embeds {
		address, err := url.Parse(embed.Src)
		if err != nil {
			panic("kbhtml: allowlist.json: " + err.Error())
		}
		if origin := address.Scheme + "://" + address.Host; !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}
	return origins
}

// Fragment is (*Rules).Fragment with allowlist.json.
func Fragment(raw string) (string, []string) { return rules.Fragment(raw) }

// Fragment turns one piece of raw HTML, as the Markdown parser hands it over, into HTML a page may
// show, and names what it left out, in order and once each. A closing tag alone, or a comment, shows
// nothing and is not named.
func (r *Rules) Fragment(raw string) (string, []string) {
	nodes, err := nethtml.ParseFragment(strings.NewReader(raw), &nethtml.Node{Type: nethtml.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return "", []string{"html"}
	}
	f := fragment{rules: r}
	for _, node := range nodes {
		f.walk(node)
	}
	return f.out.String(), f.dropped
}

type fragment struct {
	rules   *Rules
	out     strings.Builder
	dropped []string
}

func (f *fragment) drop(what string) {
	if !slices.Contains(f.dropped, what) {
		f.dropped = append(f.dropped, what)
	}
}

func (f *fragment) walk(node *nethtml.Node) {
	switch node.Type {
	case nethtml.TextNode:
		f.out.WriteString(html.EscapeString(node.Data))
		return
	case nethtml.ElementNode:
	default:
		return
	}
	name := node.Data
	switch {
	case node.Namespace == "" && name == "iframe":
		f.iframe(node)
		return
	case node.Namespace == "" && name == "video":
		f.video(node)
		return
	case slices.Contains(f.rules.DropWithContent, name):
		f.drop(name)
		return
	}
	// Any other element is left out and its children are shown in its place.
	f.drop(name)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		f.walk(child)
	}
}

func attributes(node *nethtml.Node) (map[string]string, []string) {
	values := map[string]string{}
	var order []string
	for _, attr := range node.Attr {
		if attr.Namespace != "" {
			continue
		}
		if _, seen := values[attr.Key]; !seen {
			order = append(order, attr.Key)
		}
		values[attr.Key] = attr.Val
	}
	return values, order
}

// iframe writes a player rebuilt from the video id in its src, or nothing.
func (f *fragment) iframe(node *nethtml.Node) {
	r := f.rules
	given, order := attributes(node)
	src, hasSrc := given["src"]
	site, embed := r.embed(src)
	if site == nil {
		if !hasSrc {
			f.drop("iframe (no src)")
		} else {
			f.drop("iframe (" + hostOf(src) + ")")
		}
		return
	}
	var tag strings.Builder
	attr := func(key, value string) {
		tag.WriteString(" " + key + `="` + html.EscapeString(value) + `"`)
	}
	attr("src", embed)
	if title, ok := given["title"]; ok && strings.TrimSpace(title) != "" {
		attr("title", title)
	} else {
		attr("title", site.Title)
	}
	for _, key := range []string{"width", "height"} {
		if value, ok := given[key]; ok {
			if r.dimension(value) {
				attr(key, value)
			} else {
				f.drop("iframe " + key)
			}
		}
	}
	_, fullscreen := given["allowfullscreen"]
	if value, ok := given["allow"]; ok {
		var kept []string
		for _, token := range strings.Split(value, ";") {
			fields := strings.Fields(token)
			if len(fields) == 0 {
				continue
			}
			feature := strings.ToLower(fields[0])
			switch {
			case !slices.Contains(r.Iframe.Allow, feature):
				f.drop("iframe allow " + feature)
			// allowfullscreen and allow="fullscreen" say the same; browsers warn when both are there.
			case feature == "fullscreen" && fullscreen, slices.Contains(kept, feature):
			default:
				kept = append(kept, feature)
			}
		}
		if len(kept) > 0 {
			attr("allow", strings.Join(kept, "; "))
		}
	}
	if fullscreen {
		attr("allowfullscreen", "")
	}
	for _, key := range order {
		if !slices.Contains(r.Iframe.Attributes, key) || (key == "loading" && given[key] != r.Iframe.Forced.Loading) {
			f.drop("iframe " + key)
		}
	}
	candidate := "<iframe" + tag.String() + "></iframe>"
	if r.policy.Sanitize(candidate) != candidate {
		f.drop("iframe")
		return
	}
	attr("sandbox", r.Iframe.Forced.Sandbox)
	attr("referrerpolicy", r.Iframe.Forced.ReferrerPolicy)
	attr("loading", r.Iframe.Forced.Loading)
	f.out.WriteString("<iframe" + tag.String() + "></iframe>")
}

// video writes a player of a media library file, or nothing; with the library off, nothing.
func (f *fragment) video(node *nethtml.Node) {
	r := f.rules
	given, order := attributes(node)
	src, hasSrc := given["src"]
	switch {
	case !hasSrc:
		f.drop("video (no src)")
		return
	case !r.Video.src.MatchString(src):
		f.drop("video (" + hostOf(src) + ")")
		return
	case !r.Video.Enabled:
		f.drop("video (media library)")
		return
	}
	var tag strings.Builder
	attr := func(key, value string) {
		tag.WriteString(" " + key + `="` + html.EscapeString(value) + `"`)
	}
	attr("src", src)
	if poster, ok := given["poster"]; ok {
		if r.Video.poster.MatchString(poster) {
			attr("poster", poster)
		} else {
			f.drop("video poster")
		}
	}
	for _, key := range []string{"width", "height"} {
		if value, ok := given[key]; ok {
			if r.dimension(value) {
				attr(key, value)
			} else {
				f.drop("video " + key)
			}
		}
	}
	preload := "metadata"
	if value, ok := given["preload"]; ok {
		if slices.Contains(r.Video.Preload, value) {
			preload = value
		} else {
			f.drop("video preload=" + value)
		}
	}
	attr("preload", preload)
	for _, flag := range r.Video.Flags {
		if _, ok := given[flag]; ok {
			attr(flag, "")
		}
	}
	for _, key := range order {
		if !slices.Contains(r.Video.Attributes, key) {
			f.drop("video " + key)
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == nethtml.ElementNode {
			f.drop(child.Data)
		}
	}
	candidate := "<video" + tag.String() + "></video>"
	if r.policy.Sanitize(candidate) != candidate {
		f.drop("video")
		return
	}
	// Controls are not the author's choice: a video always has them.
	attr("controls", "")
	f.out.WriteString("<video" + tag.String() + "></video>")
}

func (r *Rules) dimension(value string) bool {
	if !r.Dimension.pattern.MatchString(value) {
		return false
	}
	number, err := strconv.Atoi(value)
	return err == nil && number >= r.Dimension.Min && number <= r.Dimension.Max
}

// trim drops what a browser drops around an address: spaces and control characters.
func trim(raw string) string {
	return strings.TrimFunc(raw, func(char rune) bool { return char <= ' ' })
}

// embed returns the player address an iframe may load, built again from the video id in raw, so no
// path, query or host of the author's comes through, and the site it plays from; nil when raw is no
// player of the list. An address with a space, control character or backslash inside is refused:
// browsers read those in ways a server parser does not.
func (r *Rules) embed(raw string) (*Embed, string) {
	raw = trim(raw)
	if strings.ContainsFunc(raw, func(char rune) bool { return char <= ' ' || char == 0x7f || char == '\\' }) {
		return nil, ""
	}
	address, err := url.Parse(raw)
	if err != nil || address.Scheme != "https" || address.Opaque != "" || address.User != nil || address.Port() != "" || address.Host == "" {
		return nil, ""
	}
	host := strings.ToLower(address.Hostname())
	for _, embed := range r.Iframe.Embeds {
		if !slices.Contains(embed.Hosts, host) {
			continue
		}
		match := embed.path.FindStringSubmatch(address.EscapedPath())
		if match == nil {
			return nil, ""
		}
		query := url.Values{}
		given := address.Query()
		for key, pattern := range embed.query {
			if value := given.Get(key); value != "" && pattern.MatchString(value) {
				query.Set(key, value)
			}
		}
		src := embed.Src + match[1]
		if len(query) > 0 {
			src += "?" + query.Encode()
		}
		return embed, src
	}
	return nil, ""
}

// hostOf names where a refused address points, for the preview's warning.
func hostOf(raw string) string {
	raw = trim(raw)
	if raw == "" {
		return "no src"
	}
	if !strings.ContainsFunc(raw, func(char rune) bool { return char <= ' ' || char == 0x7f || char == '\\' }) {
		if address, err := url.Parse(raw); err == nil && address.Scheme != "" {
			if address.Host != "" {
				return strings.ToLower(address.Host)
			}
			return strings.ToLower(address.Scheme) + ":"
		}
	}
	runes := []rune(raw)
	if len(runes) > 40 {
		runes = runes[:40]
	}
	return string(runes)
}
