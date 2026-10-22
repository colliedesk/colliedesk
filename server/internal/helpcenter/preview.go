package helpcenter

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// The pages a preview shows.
const (
	PreviewHome    = "home"
	PreviewArticle = "article"
)

// previewBase is where the links and media of a preview point: the signed-in person's knowledge base
// routes, which serve this workspace's images (base/images/<id>) and videos (base/media/<id>) with the
// session, whether the help center is on or off. The frame showing a preview follows no link.
const previewBase = "/api/v1/kb"

// previewPolicy is the preview document's own Content-Security-Policy. The document is shown in a frame
// of the app, so it carries its rules itself: its style is inline (a theme not saved has no hashed brand
// sheet), images come from the app's origin, and nothing runs, submits or changes the base address.
const previewPolicy = "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; script-src 'none'; form-action 'none'; base-uri 'none'"

// previewHead is where the policy and the style go: right after the charset, before anything loads.
var previewHead = []byte("<meta charset=\"utf-8\">\n")

// sampleArticle is the text an article preview shows when nothing is published yet.
const (
	sampleTitle = "An example article"
	sampleText  = "This is how an article reads in your help center. A paragraph has [a link](https://example.com) in it.\n\n" +
		"## Steps\n\n1. Open **Settings**.\n2. Choose *Help center*.\n3. Save.\n\n> A quote stands out from the text.\n\n`Ctrl+K` opens search."
)

// Preview renders a page of the workspace's help center with theme as the brand, through the real
// templates, from its live collections and published articles only, whether the help center is on or
// off. theme must be clean (domain.CleanTheme) and its logo and favicon checked to be this workspace's;
// one not in the library outside its trash is left out, as on the public pages. The answer is a whole
// document with its style inline and its own policy, for a sandboxed frame without scripts.
func (s *Site) Preview(ctx context.Context, workspaceID string, theme domain.HelpCenterTheme, which string) ([]byte, error) {
	site, first, err := s.config.Store.HelpCenterPreviewSite(ctx, workspaceID, theme)
	if err != nil {
		return nil, err
	}
	v := s.newView(site, previewBase)
	var data page
	switch which {
	case PreviewHome:
		data, err = s.homePage(ctx, v)
	case PreviewArticle:
		data, err = s.previewArticle(ctx, v, first)
	default:
		return nil, errors.New("help center preview: unknown page " + which)
	}
	if err != nil {
		return nil, err
	}
	// The style goes inline; a favicon shows nowhere in a frame; the preview has no public address.
	data.Style, data.BrandStyle, data.Favicon, data.Canonical = "", "", "", ""
	if data.Logo != nil {
		data.Logo.URL = domain.KBMediaPath + *site.Theme.LogoMediaID
	}
	body := s.pages.render(which, data)
	at := bytes.Index(body, previewHead)
	if at < 0 {
		return nil, errors.New("help center preview: the layout has no charset to follow")
	}
	at += len(previewHead)
	var out bytes.Buffer
	out.Grow(len(body) + len(s.pages.style) + len(v.brand.sheet) + 200)
	out.Write(body[:at])
	out.WriteString(`<meta http-equiv="Content-Security-Policy" content="` + previewPolicy + "\">\n<style>\n")
	out.Write(s.pages.style)
	out.Write(v.brand.sheet)
	out.WriteString("</style>\n")
	out.Write(body[at:])
	return out.Bytes(), nil
}

// previewArticle is the page of the published article with number first, or of the sample text when
// there is none.
func (s *Site) previewArticle(ctx context.Context, v view, first int64) (page, error) {
	if first > 0 {
		article, err := s.config.Store.HelpCenterArticle(ctx, v.site.WorkspaceID, first)
		if err == nil {
			data, _, err := s.articlePage(ctx, v, article)
			return data, err
		}
		// Unpublished since the count: the sample stands in.
		if !errors.Is(err, domain.ErrKBArticleNotFound) {
			return page{}, err
		}
	}
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
