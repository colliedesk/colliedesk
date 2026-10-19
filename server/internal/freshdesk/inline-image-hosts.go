package freshdesk

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/mailimage"
)

// Hosts an image may be fetched from. Exact patterns only: a tenant portal of another Freshdesk
// account, or a bucket anyone can name, is not Freshdesk's own storage.
var (
	// attachmentHost: Freshdesk's inline attachment gateways per data center ("attachment", "euc-attachment", "indattachment").
	attachmentHost = regexp.MustCompile(`^(euc-|ind)?attachment\.freshdesk\.com$`)
	s3PathHost     = regexp.MustCompile(`^s3([.-][a-z0-9-]+)?\.amazonaws\.com$`)
	// s3AssetBucket: Freshdesk's public buckets (uploaded content and static assets such as icons).
	s3AssetBucket = regexp.MustCompile(`^(cdn|assets)\.freshdesk\.com\.s3([.-][a-z0-9-]+)?\.amazonaws\.com$`)
	// A redirect from a gateway goes through the tenant's own host to the tenant's attachment CDN,
	// for example <tenant>.freshdesk.com then <tenant>.attachments8.freshdesk.com.
	tenantHost = regexp.MustCompile(`^[a-z0-9-]+\.(attachments[a-z0-9-]*\.)?freshdesk\.com$`)
)

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
	return s3PathHost.MatchString(host) && (strings.HasPrefix(u.Path, "/cdn.freshdesk.com/") || strings.HasPrefix(u.Path, "/assets.freshdesk.com/"))
}

// hostedImageURL returns the URL of an image Freshdesk hosts, upgraded to https (old tickets hold
// http:// links; only an allowed host is upgraded). A custom support domain is not recognised: such
// images are reported by looksHosted instead.
func (r *run) hostedImageURL(src string) (*url.URL, bool) {
	u, ok := mailimage.ParseSrc(src)
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
	mailimage.MapSources(markup, func(src string) (string, bool) {
		if _, ok := r.hostedImageURL(src); !ok && strings.Contains(strings.ToLower(src), "freshdesk") && !strings.HasPrefix(src, "data:") {
			found = true
		}
		return "", false
	})
	return found
}

// allowedRedirectURL says whether a redirect hop of an image download may go to u: anything an
// image may be fetched from, and Freshdesk tenant and attachment CDN hosts. A tenant host is accepted
// only as a hop, never as the address written in a message, so another tenant's portal is not a source.
func (r *run) allowedRedirectURL(u *url.URL) bool {
	return r.allowedImageURL(u) || tenantHost.MatchString(strings.ToLower(u.Hostname()))
}
