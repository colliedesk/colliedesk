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
	"io"
	"log"
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
}

// Site serves every workspace's help center.
type Site struct {
	config     Config
	originHost string
	pages      pages
	// notFound and notFoundOnHost are the 404 page of an address that is no open help center.
	notFound, notFoundOnHost []byte
}

// New builds the help center from its configuration. It panics when a template is broken, which the
// tests catch.
func New(config Config) *Site {
	config.Origin = strings.TrimRight(config.Origin, "/")
	config.Domain = strings.ToLower(strings.TrimSpace(config.Domain))
	site := &Site{config: config, pages: parsePages()}
	if parsed, err := url.Parse(config.Origin); err == nil {
		site.originHost = strings.ToLower(parsed.Hostname())
	}
	site.notFound = site.pages.render("missing", page{Lang: "en", Title: "Page not found", Style: pathPrefix + strings.TrimPrefix(stylePath, "/") + site.pages.styleName})
	site.notFoundOnHost = site.pages.render("missing", page{Lang: "en", Title: "Page not found", Style: stylePath + site.pages.styleName})
	return site
}

// Wrap answers help center requests and passes every other request to next: a host name below the
// help center domain, and paths under /help/.
func (s *Site) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if slug, ok := hostSlug(request.Host, s.config.Domain, s.originHost); ok {
			s.serve(response, request, slug, "", request.URL.Path)
			return
		}
		if request.URL.Path == strings.TrimSuffix(pathPrefix, "/") {
			s.writeNotFound(response, false)
			return
		}
		if rest, ok := strings.CutPrefix(request.URL.Path, pathPrefix); ok {
			slug, sub, _ := strings.Cut(rest, "/")
			s.serve(response, request, slug, pathPrefix+slug, "/"+sub)
			return
		}
		next.ServeHTTP(response, request)
	})
}

// serve answers one request to the help center of slug; base is the path prefix the help center's
// own links start with ("" on its own host) and sub the path below it.
func (s *Site) serve(response http.ResponseWriter, request *http.Request, slug, base, sub string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		securityHeaders(response.Header())
		response.Header().Set("Cache-Control", "no-store")
		http.Error(response, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if sub == stylePath+s.pages.styleName || base == pathPrefix+"_" && sub == "/"+s.pages.styleName {
		s.writeStyle(response, request)
		return
	}
	if !slugShape.MatchString(slug) {
		s.writeNotFound(response, base == "")
		return
	}
	if s.config.Allow != nil {
		if allowed, wait := s.config.Allow(request, sub == "/search"); !allowed {
			s.writeBusy(response, wait, base)
			return
		}
	}
	// A server with a help center domain serves each help center on its own host; the path address
	// moves there, whether or not the workspace exists, so the redirect tells nothing about it.
	if base != "" && s.config.Domain != "" && email.ValidateHostedSlug(slug) == nil {
		target := URL(s.config.Origin, s.config.Domain, slug) + sub
		if request.URL.RawQuery != "" {
			target += "?" + request.URL.RawQuery
		}
		response.Header().Set("Cache-Control", "no-store")
		http.Redirect(response, request, target, http.StatusMovedPermanently)
		return
	}
	ctx := request.Context()
	site, open, err := s.config.Store.OpenHelpCenter(ctx, slug)
	if err != nil {
		s.writeError(response, err)
		return
	}
	// Unknown, off and locked are one answer, and none of them ever reaches the ETag.
	if !open {
		s.writeNotFound(response, base == "")
		return
	}
	view := s.newView(site, base)
	switch {
	case sub == "/" || sub == "":
		s.home(response, request, view)
	case sub == "/search":
		s.search(response, request, view)
	case strings.HasPrefix(sub, "/c/"):
		s.collection(response, request, view, strings.TrimPrefix(sub, "/c/"))
	case strings.HasPrefix(sub, "/a/"):
		s.article(response, request, view, strings.TrimPrefix(sub, "/a/"))
	case strings.HasPrefix(sub, "/images/"):
		s.image(response, request, view, strings.TrimPrefix(sub, "/images/"))
	case sub == "/robots.txt" && base == "":
		s.robots(response, request, view)
	default:
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
}

func (s *Site) newView(site domain.HelpCenterSite, base string) view {
	home := base
	if home == "" {
		home = "/"
	}
	// Everything a page shows besides the knowledge base itself: the build, the trial (noindex), the
	// title, language and name the header uses, and the address.
	sum := sha256.Sum256([]byte(strings.Join([]string{site.Slug, strconv.FormatInt(site.ContentVersion, 10), strconv.FormatBool(site.Trial),
		site.Title, site.Locale, site.Name, base, s.config.Version, s.pages.fingerprint}, "\x00")))
	return view{site: site, base: base, home: home, etag: `"` + hex.EncodeToString(sum[:12]) + `"`, address: URL(s.config.Origin, s.config.Domain, site.Slug)}
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

func (s *Site) writeStyle(response http.ResponseWriter, request *http.Request) {
	header := response.Header()
	header.Set("Content-Type", "text/css; charset=utf-8")
	header.Set("X-Content-Type-Options", "nosniff")
	// The name holds the content's hash, so it never changes under the same address.
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("Content-Length", strconv.Itoa(len(s.pages.style)))
	response.WriteHeader(http.StatusOK)
	if request.Method != http.MethodHead {
		_, _ = response.Write(s.pages.style)
	}
}

func (s *Site) robots(response http.ResponseWriter, request *http.Request, v view) {
	body := "User-agent: *\nDisallow:\n"
	if v.site.Trial {
		body = "User-agent: *\nDisallow: /\n"
	}
	if fresh(response, request, v.etag) {
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodHead {
		_, _ = io.WriteString(response, body)
	}
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
	body, err := s.config.Objects.Open(request.Context(), image.StorageKey)
	if err != nil {
		log.Printf("help center image %s: open stored bytes: %v", image.ID, err)
		s.writeSiteNotFound(response, v)
		return
	}
	defer body.Close()
	header := response.Header()
	header.Set("Content-Type", image.ContentType)
	header.Set("Content-Length", strconv.FormatInt(image.Size, 10))
	header.Set("X-Content-Type-Options", "nosniff")
	// Nothing the image holds may run or load, even if a browser opened it as a page.
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	response.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(response, body); err != nil {
		log.Printf("help center image %s: stream: %v", image.ID, err)
	}
}
