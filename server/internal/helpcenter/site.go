// Package helpcenter serves the public help center of every workspace: plain HTML rendered on the
// server from the published knowledge base articles, with no script. A self-hosted server answers on
// <origin>/help/<slug>; a server with a help center domain answers on <slug>.<domain> and redirects
// the path there. docs/internals/help-center.md records the decisions.
package helpcenter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/kbhtml"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

const (
	// pathPrefix is where a server without a help center domain serves each help center.
	pathPrefix = "/help/"
	// stylePath holds the style sheet below every help center and below /help/. "_" is never a slug.
	stylePath = "/_/"
	// collectionPageSize is how many articles a collection page lists.
	collectionPageSize = 50
	// homeArticles is how many articles of each collection the home page lists.
	homeArticles = 6
	// articleNeighbours is how many articles of its collection an article page lists beside it.
	articleNeighbours = 40
	// searchResults is how many matches a search shows; maxSearchChars bounds the query.
	searchResults  = 25
	maxSearchChars = 200
	excerptChars   = 220
	// descriptionChars bounds an article page's meta description.
	descriptionChars = 160
)

// contentSecurityPolicy lets a page load its own style sheet, images and media, and frame the video
// players of kbhtml's allowlist, and nothing else; no script of the page may run, and no other site
// may frame it. A help center on the app's own origin therefore cannot reach a signed-in person's
// session.
var contentSecurityPolicy = "default-src 'none'; img-src 'self'; style-src 'self'; frame-src " + strings.Join(kbhtml.FrameSources(), " ") +
	"; media-src 'self'; script-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// slugShape is the shape the workspaces table enforces; anything else cannot be a workspace.
var slugShape = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// articleAddress is <number> or <number>-<slug>.
var articleAddress = regexp.MustCompile(`^([1-9][0-9]{0,17})(?:-(.*))?$`)

// Config is what the help center needs from the server.
type Config struct {
	Store   *store.Store
	Objects attachments.Storage
	// Origin is APP_ORIGIN; Domain is HELP_CENTER_DOMAIN, empty for path addresses only.
	Origin, Domain string
	// Version is the server build; a new build changes every page's ETag.
	Version string
	// Allow reports whether the client of request may load one more page (or run one more search),
	// and how long to wait when not. Nil allows everything.
	Allow func(request *http.Request, search bool) (bool, time.Duration)
	// AllowLookup reports whether the client of request may make the server look up one more host
	// name it does not know (a privileged query), and how long to wait when not. Nil allows everything.
	AllowLookup func(request *http.Request) (bool, time.Duration)
	// Client names the address a request comes from (behind trusted proxies), for the bound on videos
	// sent at once; nil uses the connection's address.
	Client func(request *http.Request) string
}

// Site serves every workspace's help center.
type Site struct {
	config     Config
	originHost string
	pages      pages
	// renders renders article pages, at most once per text (render.go).
	renders *renders
	// notFound and notFoundOnHost are the 404 page of an address that is no open help center.
	notFound, notFoundOnHost []byte
	// videos bounds the videos being sent at once (streams.go).
	videos *streams
	// hosts finds the workspaces of custom domains (custom-host.go).
	hosts *customHosts
}

// New builds the help center from its configuration. It panics when a template is broken, which the
// tests catch.
func New(config Config) *Site {
	config.Origin = strings.TrimRight(config.Origin, "/")
	config.Domain = strings.ToLower(strings.TrimSpace(config.Domain))
	site := &Site{config: config, pages: parsePages(), renders: defaultRenders(), videos: newStreams(), hosts: newCustomHosts(time.Now)}
	if parsed, err := url.Parse(config.Origin); err == nil {
		site.originHost = strings.ToLower(parsed.Hostname())
	}
	site.notFound = site.pages.render("missing", page{Lang: "en", Title: "Page not found", Style: pathPrefix + strings.TrimPrefix(stylePath, "/") + site.pages.styleName})
	site.notFoundOnHost = site.pages.render("missing", page{Lang: "en", Title: "Page not found", Style: stylePath + site.pages.styleName})
	return site
}

// Wrap answers help center requests and passes the others to next (the app, the API and MCP). next
// is reached only on APP_ORIGIN's host, and on loopback and IP address hosts (health checks, the
// reverse proxy's certificate question): there the paths under /help/ are the help center and every
// other path goes on. Every other host name belongs to the help center: a host name below the help
// center domain, a claim's CNAME target, or an active custom domain serve it, whatever the path, and
// any other name, such as a custom domain just removed, gets the help center's 404. So the app, its
// sign-in and its session never answer on a name the server does not own. Requests to APP_ORIGIN's
// host and below HELP_CENTER_DOMAIN never look a custom domain up.
func (s *Site) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		host := requestHost(request.Host)
		if slug, ok := hostSlug(host, s.config.Domain, s.originHost); ok {
			s.serve(response, request, address{slug: slug, sub: request.URL.Path})
			return
		}
		if _, ok := claimOfHost(host, s.config.Domain); ok {
			s.serveHost(response, request, host, true)
			return
		}
		if !s.appHost(host) {
			if candidate, ok := s.customHostCandidate(host); ok {
				s.serveHost(response, request, candidate, false)
				return
			}
			s.writeNotFound(response, true)
			return
		}
		if request.URL.Path == strings.TrimSuffix(pathPrefix, "/") {
			s.writeNotFound(response, false)
			return
		}
		if rest, ok := strings.CutPrefix(request.URL.Path, pathPrefix); ok {
			slug, sub, _ := strings.Cut(rest, "/")
			s.serve(response, request, address{slug: slug, base: pathPrefix + slug, sub: "/" + sub})
			return
		}
		next.ServeHTTP(response, request)
	})
}

// appHost reports whether the app answers on a request's host name (requestHost): APP_ORIGIN's host,
// localhost, or an IP address.
func (s *Site) appHost(host string) bool {
	return host == s.originHost || host == "localhost" || net.ParseIP(strings.Trim(host, "[]")) != nil
}

// serveHost answers a request on a host name that belongs to a workspace's help center through the
// lookup (custom-host.go): an active custom domain, or a claim's CNAME target (claim), which moves to
// the help center's address. A name that belongs to none is the help center's 404.
func (s *Site) serveHost(response http.ResponseWriter, request *http.Request, host string, claim bool) {
	workspaceID, found, wait, err := s.workspaceOfHost(request, host)
	switch {
	case err != nil:
		s.writeError(response, err)
	case wait > 0:
		s.writeBusy(response, wait, "")
	case !found:
		s.writeNotFound(response, true)
	default:
		s.serve(response, request, address{workspaceID: workspaceID, host: host, claim: claim, sub: request.URL.Path})
	}
}

// requestHost is a request's host name in lower case, without port or trailing dot.
func requestHost(host string) string {
	host = strings.ToLower(host)
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	return strings.TrimSuffix(host, ".")
}

// address is where a request reached a help center: a slug (on its host name or below /help/), or the
// workspace and host name of an active custom domain or of a claim's CNAME target (claim). base is the
// path prefix the help center's own links start with ("" on a host name of its own) and sub the path
// below it.
type address struct {
	slug              string
	workspaceID, host string
	claim             bool
	base, sub         string
}

// movedTo is where an old address of a help center with an active custom domain moves: the same path
// (as the request escaped it) and query below the custom domain. Only the stored domain and the path
// are used, never a host name the request names, so it cannot point anywhere else.
func (s *Site) movedTo(request *http.Request, at address, site domain.HelpCenterSite) string {
	sub := request.URL.EscapedPath()
	if at.base != "" {
		rest, ok := strings.CutPrefix(sub, at.base)
		if !ok || rest != "" && rest[0] != '/' {
			rest = (&url.URL{Path: at.sub}).EscapedPath()
		}
		sub = rest
	}
	if sub == "" {
		sub = "/"
	}
	target := SiteURL(s.config.Origin, s.config.Domain, site.Slug, site.CustomHost) + sub
	if request.URL.RawQuery != "" {
		target += "?" + request.URL.RawQuery
	}
	return target
}

// serve answers one request to the help center at address at.
func (s *Site) serve(response http.ResponseWriter, request *http.Request, at address) {
	slug, base, sub := at.slug, at.base, at.sub
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if sub == stylePath+s.pages.styleName || base == pathPrefix+"_" && sub == "/"+s.pages.styleName {
		s.writeStyle(response, request, s.pages.style)
		return
	}
	if at.host == "" && !slugShape.MatchString(slug) {
		s.writeNotFound(response, base == "")
		return
	}
	if s.config.Allow != nil {
		if allowed, wait := s.config.Allow(request, sub == "/search"); !allowed {
			s.writeBusy(response, wait, base)
			return
		}
	}
	ctx := request.Context()
	var site domain.HelpCenterSite
	var open bool
	var err error
	if at.host != "" {
		site, open, err = s.config.Store.OpenHelpCenterWorkspace(ctx, at.workspaceID)
	} else {
		site, open, err = s.config.Store.OpenHelpCenter(ctx, slug)
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	switch {
	// A claim's CNAME target is only where the custom domain's DNS points: a visitor who opens it
	// moves to the help center's address, the custom domain once it is active.
	case at.claim && open:
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, s.movedTo(request, at, site), http.StatusMovedPermanently)
		return
	// A custom domain removed or replaced in another process may still be remembered here; the
	// workspace's own row decides.
	case at.host != "" && (at.claim || !open || site.CustomHost != at.host):
		s.writeNotFound(response, true)
		return
	// An open help center with an active custom domain serves only there; its other addresses move.
	case at.host == "" && open && site.CustomHost != "":
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, s.movedTo(request, at, site), http.StatusMovedPermanently)
		return
	// A server with a help center domain serves each help center on its own host; the path address
	// moves there, whether or not the workspace exists, so the redirect tells nothing about it.
	case base != "" && s.config.Domain != "" && email.ValidateHostedSlug(slug) == nil:
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, s.movedTo(request, at, domain.HelpCenterSite{Slug: slug}), http.StatusMovedPermanently)
		return
	// Unknown, off and locked are one answer, and none of them ever reaches the ETag.
	case !open:
		s.writeNotFound(response, base == "")
		return
	}
	view := s.newView(site, base)
	switch {
	case sub == "/" || sub == "":
		// One address for the home page: no trailing slash below /help/, no query.
		if request.URL.RawQuery != "" || base != "" && request.URL.Path != base {
			moveTo(response, request, view.home)
			return
		}
		s.home(response, request, view)
	case sub == "/search":
		s.search(response, request, view)
	case strings.HasPrefix(sub, "/c/"):
		s.collection(response, request, view, strings.TrimPrefix(sub, "/c/"))
	case strings.HasPrefix(sub, "/a/"):
		s.article(response, request, view, strings.TrimPrefix(sub, "/a/"))
	case strings.HasPrefix(sub, "/images/"):
		s.image(response, request, view, strings.TrimPrefix(sub, "/images/"))
	case strings.HasPrefix(sub, "/media/"):
		s.video(response, request, view, strings.TrimPrefix(sub, "/media/"))
	case strings.HasPrefix(sub, stylePath):
		s.brandAsset(response, request, view, strings.TrimPrefix(sub, stylePath))
	case sub == "/robots.txt" && base == "":
		s.robots(response, request, view)
	case sub == "/sitemap.xml":
		s.sitemap(response, request, view)
	case sub == "/llms.txt":
		s.llms(response, request, view)
	default:
		if old, ok := ParseLegacyPath(sub); ok {
			s.legacy(response, request, view, old)
			return
		}
		s.writeSiteNotFound(response, view)
	}
}

// view is one help center as its pages show it.
type view struct {
	site domain.HelpCenterSite
	base string
	home string
	etag string
	// address is the help center's public address, for canonical links.
	address string
	brand   brand
}

func (s *Site) newView(site domain.HelpCenterSite, base string) view {
	home := base
	if home == "" {
		home = "/"
	}
	// Everything a page shows besides the knowledge base itself: the build, the trial (noindex), the
	// title, language and name the header uses, the address and the brand. The brand counts what is
	// shown, not only the saved theme: a logo moved to the trash changes no content version.
	brand := newBrand(site)
	address := SiteURL(s.config.Origin, s.config.Domain, site.Slug, site.CustomHost)
	sum := sha256.Sum256([]byte(strings.Join([]string{site.Slug, strconv.FormatInt(site.ContentVersion, 10), strconv.FormatBool(site.Trial),
		site.Title, site.Locale, site.Name, base, address, s.config.Version, s.pages.fingerprint, brand.key}, "\x00")))
	return view{site: site, base: base, home: home, etag: `"` + hex.EncodeToString(sum[:12]) + `"`, address: address, brand: brand}
}

func (v view) title() string {
	if v.site.Title != "" {
		return v.site.Title
	}
	return v.site.Name
}

// fresh answers 304 and reports true when the client already holds this version of the page.
func fresh(response http.ResponseWriter, request *http.Request, etag string) bool {
	header := response.Header()
	header.Set("ETag", etag)
	header.Set("Cache-Control", "public, max-age=0, must-revalidate")
	for _, candidate := range strings.Split(request.Header.Get("If-None-Match"), ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == etag || candidate == "*" {
			response.WriteHeader(http.StatusNotModified)
			return true
		}
	}
	return false
}

func securityHeaders(header http.Header) {
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// writePage sends a rendered page with its cache and robots headers.
func (s *Site) writePage(response http.ResponseWriter, request *http.Request, v view, status int, body []byte) {
	header := response.Header()
	securityHeaders(header)
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	if v.site.Trial {
		header.Set("X-Robots-Tag", "noindex")
	}
	if status != http.StatusOK {
		header.Del("ETag")
		header.Set("Cache-Control", "no-store")
	}
	response.WriteHeader(status)
	if request.Method != http.MethodHead {
		_, _ = response.Write(body)
	}
}

// writeNotFound is the one answer for a help center that does not exist, is off or is locked, and
// for a malformed address: the same status, headers and bytes (one copy for help center hosts, one
// for paths, which differ only in where the style sheet is).
func (s *Site) writeNotFound(response http.ResponseWriter, onHost bool) {
	body := s.notFound
	if onHost {
		body = s.notFoundOnHost
	}
	header := response.Header()
	securityHeaders(header)
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Content-Length", strconv.Itoa(len(body)))
	header.Set("Cache-Control", "no-store")
	header.Set("X-Robots-Tag", "noindex")
	response.WriteHeader(http.StatusNotFound)
	_, _ = response.Write(body)
}

func (s *Site) writeSiteNotFound(response http.ResponseWriter, v view) {
	body := s.pages.render("missing", s.page(v, "Page not found", nil))
	header := response.Header()
	header.Del("ETag")
	securityHeaders(header)
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Robots-Tag", "noindex")
	response.WriteHeader(http.StatusNotFound)
	_, _ = response.Write(body)
}

func (s *Site) writeBusy(response http.ResponseWriter, wait time.Duration, base string) {
	body := s.pages.render("busy", page{Lang: "en", Title: "Too many requests", Style: base + stylePath + s.pages.styleName})
	header := response.Header()
	securityHeaders(header)
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	response.WriteHeader(http.StatusTooManyRequests)
	_, _ = response.Write(body)
}

func (s *Site) writeError(response http.ResponseWriter, err error) {
	if !errors.Is(err, context.Canceled) {
		log.Printf("help center: %v", err)
	}
	header := response.Header()
	header.Del("ETag")
	securityHeaders(header)
	header.Set("Cache-Control", "no-store")
	http.Error(response, "Something went wrong. Try again in a moment.", http.StatusInternalServerError)
}

// writeStyle sends a style sheet: site.css or a brand sheet.
func (s *Site) writeStyle(response http.ResponseWriter, request *http.Request, style []byte) {
	header := response.Header()
	header.Set("Content-Type", "text/css; charset=utf-8")
	header.Set("X-Content-Type-Options", "nosniff")
	// The name holds the content's hash, so it never changes under the same address.
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("Content-Length", strconv.Itoa(len(style)))
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write(style)
	}
}

// video plays a library video that a published article shows (<id>), with ranges, or its poster
// (<id>/poster/<version>), by the rule of image. A poster's address holds the end of its content
// address, so an older version is a 404, never another picture a cache would keep.
func (s *Site) video(response http.ResponseWriter, request *http.Request, v view, address string) {
	site := v.site
	id, version, poster := strings.Cut(address, "/poster/")
	if s.config.Objects == nil || strings.Contains(id, "/") {
		s.writeSiteNotFound(response, v)
		return
	}
	video, err := s.config.Store.HelpCenterVideo(request.Context(), site.WorkspaceID, id)
	if errors.Is(err, domain.ErrKBMediaNotFound) || err == nil && !strings.HasPrefix(video.StorageKey, site.WorkspaceID+"/") {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	served := attachments.Served{Key: video.StorageKey, ContentType: video.ContentType, Size: video.Size, ETag: video.SHA256,
		Cache: "public, max-age=31536000, immutable"}
	if poster {
		if video.PosterKey == "" || !strings.HasPrefix(video.PosterKey, site.WorkspaceID+"/") || len(version) != 8 || !strings.HasSuffix(video.PosterKey, version) {
			s.writeSiteNotFound(response, v)
			return
		}
		served = attachments.Served{Key: video.PosterKey, ContentType: video.PosterContentType, ETag: video.PosterKey[strings.LastIndex(video.PosterKey, "/")+1:],
			Cache: "public, max-age=31536000, immutable"}
	}
	if !poster {
		client := s.client(request)
		if !s.videos.enter(client) {
			securityHeaders(response.Header())
			response.Header().Set("Cache-Control", "no-store")
			response.Header().Set("Retry-After", "5")
			http.Error(response, "Too many videos are playing; try again in a moment.", http.StatusServiceUnavailable)
			return
		}
		defer s.videos.leave(client)
	}
	if !attachments.Serve(response, request, s.config.Objects, served) {
		s.writeSiteNotFound(response, v)
	}
}

func (s *Site) client(request *http.Request) string {
	if s.config.Client != nil {
		return s.config.Client(request)
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

// image streams an image that a published article shows. It never changes under its id, so any cache
// may keep it; an image no published article shows any more is the help center's 404.
func (s *Site) image(response http.ResponseWriter, request *http.Request, v view, id string) {
	site := v.site
	if s.config.Objects == nil {
		s.writeSiteNotFound(response, v)
		return
	}
	image, err := s.config.Store.HelpCenterImage(request.Context(), site.WorkspaceID, id)
	// Storage keys start with the owning workspace id; a row pointing at another workspace's bytes is never served.
	if errors.Is(err, domain.ErrKBImageNotFound) || err == nil && !strings.HasPrefix(image.StorageKey, site.WorkspaceID+"/") {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	s.streamImage(response, request, v, image)
}
