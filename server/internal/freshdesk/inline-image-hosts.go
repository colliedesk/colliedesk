package freshdesk

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Hosts an image may be fetched from. Exact patterns only: a tenant portal of another Freshdesk
// account, or a bucket anyone can name, is not Freshdesk's own storage.
var (
	attachmentHost  = regexp.MustCompile(`^([a-z0-9]+-)?attachment\.freshdesk\.com$`)
	s3PathHost      = regexp.MustCompile(`^s3([.-][a-z0-9-]+)?\.amazonaws\.com$`)
	s3AssetBucket   = regexp.MustCompile(`^cdn\.freshdesk\.com\.s3([.-][a-z0-9-]+)?\.amazonaws\.com$`)
	imgSrc          = regexp.MustCompile(`(?is)(<img\b[^>]*?\ssrc\s*=\s*)(?:"([^"]*)"|'([^']*)')`)
	rasterMediaType = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}
)

// mapImageSources rewrites the src of every <img>. fn gets the attribute value with HTML entities
// decoded and returns the new value, or false to leave the tag as written. The new value is escaped.
func mapImageSources(markup string, fn func(src string) (string, bool)) string {
	return imgSrc.ReplaceAllStringFunc(markup, func(tag string) string {
		parts := imgSrc.FindStringSubmatch(tag)
		src := parts[2]
		if src == "" {
			src = parts[3]
		}
		replacement, ok := fn(html.UnescapeString(src))
		if !ok {
			return tag
		}
		return parts[1] + `"` + html.EscapeString(replacement) + `"`
	})
}

func parseImageSrc(src string) (*url.URL, bool) {
	if strings.HasPrefix(src, "//") {
		src = "https:" + src
	}
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, false
	}
	return u, true
}

// allowedImageURL says whether the importer may fetch from u: the importing account's own host, the
// Freshdesk attachment hosts and Freshdesk's public asset bucket. It checks the host only, so it also
// guards every redirect hop of a download.
func (r *run) allowedImageURL(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.EqualFold(u.Host, r.client.base.Host), r.opts.hooks.imageHosts[u.Host]:
		return true
	case attachmentHost.MatchString(host), s3AssetBucket.MatchString(host):
		return true
	}
	return s3PathHost.MatchString(host) && strings.HasPrefix(u.Path, "/cdn.freshdesk.com/")
}

// hostedImageURL returns the URL of an image Freshdesk hosts, upgraded to https (old tickets hold
// http:// links; only an allowed host is upgraded). A custom support domain is not recognised: such
// images are reported by looksHosted instead.
func (r *run) hostedImageURL(src string) (*url.URL, bool) {
	u, ok := parseImageSrc(src)
	if !ok || !r.allowedImageURL(u) {
		return nil, false
	}
	u.Scheme = "https"
	return u, true
}

// looksHosted finds images that name Freshdesk in their URL but are not fetched (a custom support
// domain, another tenant's portal): they stay links and the report warns about them.
func (r *run) looksHosted(markup string) bool {
	found := false
	mapImageSources(markup, func(src string) (string, bool) {
		if _, ok := r.hostedImageURL(src); !ok && strings.Contains(strings.ToLower(src), "freshdesk") && !strings.HasPrefix(src, "data:") {
			found = true
		}
		return "", false
	})
	return found
}

// sniffRaster returns the raster media type of the first bytes of data, or "".
func sniffRaster(data []byte) string {
	sniffed := http.DetectContentType(data)
	if i := strings.IndexByte(sniffed, ';'); i >= 0 {
		sniffed = sniffed[:i]
	}
	if rasterMediaType[sniffed] {
		return sniffed
	}
	return ""
}
