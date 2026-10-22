package helpcenter

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"regexp"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// The pages a preview shows.
const (
	PreviewHome    = "home"
	PreviewArticle = "article"
)

// previewBase is where the links and media of a preview point: the signed-in person's knowledge base
// routes, which serve this workspace's images (base/images/<id>) and the logo (base/media/<id>) with the
// session, whether the help center is on or off. The frame showing a preview follows no link.
const previewBase = "/api/v1/kb"

// previewPolicy is the preview document's own Content-Security-Policy for an app at origin. The
// document is shown in a frame of the app, same-origin so its images load with the session; the policy
// therefore names paths of the origin rather than all of it ('self'), as the email frame does: images only from the workspace's
// image and media routes (a CSS url() cannot reach any other route with the cookie), style only from
// the help center's hashed sheets and the inline brand block (a theme not saved has no hashed brand
// sheet), and nothing runs, submits, plays or changes the base address.
func previewPolicy(origin string) string {
	return "default-src 'none'; img-src " + origin + domain.KBImagePath + " " + origin + domain.KBMediaPath + "; style-src " + origin + pathPrefix +
		strings.TrimPrefix(stylePath, "/") + " 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'"
}

// previewStyle follows the brand in the preview's inline block. Links and controls take no pointer from
// the first paint, so a click before the app holds the frame's links cannot navigate it; a video or
// player, which the policy would block, is a plain box saying where it plays.
const previewStyle = "a,button,input,label,summary{pointer-events:none}" +
	".preview-video{display:flex;align-items:center;justify-content:center;aspect-ratio:16/9;margin:0 0 1.25rem;padding:1rem;" +
	"border:1px dashed var(--rule);border-radius:8px;background:var(--surface);color:var(--muted);text-align:center}"

// previewVideo is what stands for each <video> and player iframe of a previewed article. kbhtml writes
// both as an empty element with escaped attributes, and the rest of the text is escaped by goldmark,
// so the pattern finds exactly those.
const previewVideo = `<span class="preview-video">Video shown on the live help center</span>`

var playerElement = regexp.MustCompile(`<(?:video|iframe)\b[^<>]*></(?:video|iframe)>`)

// The policy goes right after the charset, before anything loads; the brand goes last in the head,
// after the help center's sheet whose defaults it overrides (as the brand sheet does on public pages).
var previewHead, headEnd = []byte("<meta charset=\"utf-8\">\n"), []byte("</head>")

// sampleArticle is the text an article preview shows when nothing is published yet.
const (
	sampleTitle = "An example article"
	sampleText  = "This is how an article reads in your help center. A paragraph has [a link](https://example.com) in it.\n\n" +
		"## Steps\n\n1. Open **Settings**.\n2. Choose *Help center*.\n3. Save.\n\n> A quote stands out from the text.\n\n`Ctrl+K` opens search."
)

// Preview renders a page of the workspace's help center with theme as the brand, through the real
// templates, from its live collections and published articles only, whether the help center is on or
// off, read in one transaction. theme must be clean (domain.CleanTheme); a logo or favicon that is not
// this workspace's image is a domain.ThemeError, one in the trash is left out as on the public pages.
// The answer is a whole document under previewPolicy for a sandboxed frame without scripts: the help
// center's sheet loads by its hashed, immutable address, and the candidate brand is inline.
func (s *Site) Preview(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme, which string) ([]byte, error) {
	if which != PreviewHome && which != PreviewArticle {
		return nil, errors.New("help center preview: unknown page " + which)
	}
	read, err := s.config.Store.HelpCenterPreview(ctx, workspaceID, theme, which == PreviewArticle, homeArticles, articleNeighbours)
	if err != nil {
		return nil, err
	}
	v := s.newView(read.Site, previewBase)
	t := buildTree(read.Collections)
	var data page
	switch {
	case which == PreviewHome:
		data = s.homeOf(v, t, read.Articles)
	case read.Article != nil:
		data, _ = s.articleOf(v, t, *read.Article, read.Articles)
		data.Body = template.HTML(playerElement.ReplaceAllLiteralString(string(data.Body), previewVideo)) //nolint:gosec // the rendered body with whole elements swapped for a constant
	default:
		if data, err = s.sampleArticle(v); err != nil {
			return nil, err
		}
	}
	// A favicon shows nowhere in a frame; the preview has no public address; the brand goes inline.
	data.Style, data.BrandStyle, data.Favicon, data.Canonical = pathPrefix+strings.TrimPrefix(stylePath, "/")+s.pages.styleName, "", "", ""
	if data.Logo != nil {
		data.Logo.URL = domain.KBMediaPath + *read.Site.Theme.LogoMediaID
	}
	body := s.pages.render(which, data)
	at, end := bytes.Index(body, previewHead), bytes.Index(body, headEnd)
	if at < 0 || end < at {
		return nil, errors.New("help center preview: the layout has no charset and head end to follow")
	}
	at += len(previewHead)
	var out bytes.Buffer
	out.Grow(len(body) + len(v.brand.sheet) + len(previewStyle) + 400)
	out.Write(body[:at])
	out.WriteString(`<meta http-equiv="Content-Security-Policy" content="` + previewPolicy(s.config.Origin) + "\">\n")
	out.Write(body[at:end])
	out.WriteString("<style>\n")
	out.Write(v.brand.sheet)
	out.WriteString(previewStyle + "\n</style>\n")
	out.Write(body[end:])
	return out.Bytes(), nil
}

// sampleArticle is the article page of the sample text, for a help center with nothing published.
func (s *Site) sampleArticle(v view) (page, error) {
	body, err := renderArticle(sampleText, v.base)
	if err != nil {
		return page{}, err
	}
	data := s.page(v, sampleTitle, []link{{Name: "Home", URL: v.home}})
	data.Heading, data.Body = sampleTitle, body
	now := time.Now().UTC()
	data.Updated, data.UpdatedISO = now.Format("2 Jan 2006"), now.Format(time.RFC3339)
	return data, nil
}
