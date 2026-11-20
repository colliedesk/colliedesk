package helpcenter

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// votePathSuffix follows an article's address for its votes: a form posts there.
const votePathSuffix = "/vote"

// votePath matches the path below a help center's root a vote posts to, in any language.
var votePath = regexp.MustCompile(`^(?:/[^/]+)?/a/[^/]+` + votePathSuffix + `$`)

// maxVoteForm is the largest vote form read: the line is at most 500 characters, of up to four bytes,
// percent-encoded.
const maxVoteForm = 8 << 10

// readerKeptFor is how long a browser keeps the reader id its votes are counted by.
const readerKeptFor = 365 * 24 * time.Hour

// The states of the box after a vote, named in the article's ?voted= so the page needs no script.
const (
	votedYes  = "yes"
	votedNo   = "no"
	votedSent = "sent"
)

// voteBox is "Was this article helpful?" at the end of an article: plain forms, no script.
type voteBox struct {
	T voteText
	// Action is where the forms post; "" shows the question without forms (a preview).
	Action string
	// Count is the count sentence, "" before anyone voted.
	Count string
	// State is what the reader just did (votedYes, votedNo, votedSent), "" before a vote.
	State string
	// MaxMissing is the longest "What was missing?" line.
	MaxMissing int
}

// voteBoxOf is the box of article in view v.
func voteBoxOf(v view, article domain.PublicKBArticle) *voteBox {
	box := &voteBox{T: voteTextFor(v.locale()), Action: v.root + ArticlePath(article.Number, article.Slug) + votePathSuffix, MaxMissing: domain.MaxKBVoteMissingChars}
	if count := article.Votes; count.Total > 0 {
		box.Count = fmt.Sprintf(box.T.Count, count.Helpful, count.Total)
	}
	return box
}

// articleTag is the ETag of an article's page in view v: the view's and the votes' version, since the
// page shows the counts and a vote changes no content version.
func (v view) articleTag() string {
	sum := sha256.Sum256([]byte(v.etag + "\x00" + strconv.FormatInt(v.site.VotesVersion, 10)))
	return `"` + hex.EncodeToString(sum[:12]) + `"`
}

// voteState is the state an article's address names after a vote: its only query is ?voted=<state>.
func voteState(request *http.Request) string {
	query := request.URL.Query()
	if len(query) != 1 || len(query["voted"]) != 1 {
		return ""
	}
	switch state := query.Get("voted"); state {
	case votedYes, votedNo, votedSent:
		return state
	}
	return ""
}

// AddressOrigin is scheme://host[:port] of a help center address.
func AddressOrigin(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// vote records a reader's vote on the article at address (<number>[-<slug>]) and moves them back to
// the article with ?voted=, a 303 the browser follows with a GET. A form from any other origin is
// refused: the browser sends Origin with every POST.
func (s *Site) vote(response http.ResponseWriter, request *http.Request, v view, address string) {
	header := response.Header()
	securityHeaders(header)
	header.Set("Cache-Control", "no-store")
	if origin := request.Header.Get("Origin"); origin == "" || !strings.EqualFold(origin, AddressOrigin(v.address)) {
		http.Error(response, "Forbidden", http.StatusForbidden)
		return
	}
	match := articleAddress.FindStringSubmatch(address)
	if match == nil {
		s.writeSiteNotFound(response, v)
		return
	}
	number, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		s.writeSiteNotFound(response, v)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxVoteForm)
	if err := request.ParseForm(); err != nil {
		http.Error(response, "Bad request", http.StatusBadRequest)
		return
	}
	var helpful bool
	switch request.PostForm.Get("helpful") {
	case "yes":
		helpful = true
	case "no":
	default:
		http.Error(response, "Bad request", http.StatusBadRequest)
		return
	}
	missing := request.PostForm.Get("missing")
	cookie := s.readerCookie(request, v)
	voter := sha256.Sum256([]byte("reader\x00" + cookie.Value))
	err = s.config.Store.VoteKBArticle(request.Context(), v.site.WorkspaceID, number, voter[:], helpful, missing)
	if errors.Is(err, domain.ErrKBArticleNotFound) {
		s.writeSiteNotFound(response, v)
		return
	}
	if err != nil {
		s.writeError(response, err)
		return
	}
	http.SetCookie(response, cookie)
	state := votedNo
	switch {
	case helpful:
		state = votedYes
	case strings.TrimSpace(missing) != "":
		state = votedSent
	}
	// The slug as posted: the article's page moves an old one to the current address, keeping ?voted=.
	http.Redirect(response, request, v.root+ArticlePath(number, match[2])+"?voted="+state+"#helpful", http.StatusSeeOther)
}

// readerCookie is the first-party cookie a reader's votes are counted by, with the id the request
// carries or a new one. On a host of its own __Host- with Path=/, below /help/<slug> __Secure- with
// its path; a server on plain http (development) gets neither prefix nor Secure. Never Domain=.
func (s *Site) readerCookie(request *http.Request, v view) *http.Cookie {
	name, path := "cd_reader", "/"
	if v.base != "" {
		name, path = name+"_"+v.site.Slug, v.base
	}
	secure := !strings.HasPrefix(v.address, "http://")
	if secure {
		if v.base == "" {
			name = "__Host-" + name
		} else {
			name = "__Secure-" + name
		}
	}
	cookie := &http.Cookie{Name: name, Path: path, Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(readerKeptFor.Seconds())}
	if held, err := request.Cookie(name); err == nil {
		if id, err := base64.RawURLEncoding.DecodeString(held.Value); err == nil && len(id) == 32 {
			cookie.Value = held.Value
			return cookie
		}
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	cookie.Value = base64.RawURLEncoding.EncodeToString(id)
	return cookie
}
