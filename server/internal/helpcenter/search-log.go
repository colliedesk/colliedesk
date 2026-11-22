package helpcenter

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/pageview"
)

const (
	// searchParam carries a kept search on its result links: <id>.<signature>. The canonical address
	// of an article never has it; an article opened with it moves there.
	searchParam = "s"
	// searchSignatureChars is how much of the signature a link carries: 96 bits, base64url.
	searchSignatureChars = 16
	// loggedSearchCache keeps a logged search page in the reader's own browser only, so Back shows the
	// same results with the same links instead of searching again; a shared cache never stores it.
	loggedSearchCache = "private, max-age=300"
)

// reader reports whether request comes from a reader (pageview.IsReader): not a bot, a prefetch, a
// HEAD or a team member previewing the help center.
func (s *Site) reader(request *http.Request) bool {
	team := s.config.Team != nil && s.config.Team(request)
	return pageview.IsReader(pageview.Visit{Method: request.Method, Status: http.StatusOK, Header: request.Header, Preview: team})
}

// logsSearch reports whether the search of request is kept: a reader's, with words, on a server that
// signs search links.
func (s *Site) logsSearch(request *http.Request, query string) bool {
	return query != "" && len(s.config.SearchKey) > 0 && s.reader(request)
}

// logSearch keeps a reader's search and returns what its result links carry, or "" when it was not
// kept (the workspace's searches of the minute are kept already, or the write failed): the page is the
// same, its links without the search.
func (s *Site) logSearch(request *http.Request, v view, query string, results int) string {
	id, err := s.config.Store.LogKBSearch(request.Context(), v.site.WorkspaceID, v.site.HelpCenterID, query, results)
	if err != nil {
		log.Printf("help center: keep a search of workspace %s: %v", v.site.WorkspaceID, err)
		return ""
	}
	if id == 0 {
		return ""
	}
	value := strconv.FormatInt(id, 36)
	return "?" + searchParam + "=" + value + "." + s.searchSignature(v.site.WorkspaceID, value)
}

// openedFromSearch records that a reader opened articleID from the search a result link carried. A
// value of another workspace, a forged one and a request that is not a reader's change nothing.
func (s *Site) openedFromSearch(request *http.Request, v view, value, articleID string) {
	if len(s.config.SearchKey) == 0 || !s.reader(request) {
		return
	}
	id, signature, ok := strings.Cut(value, ".")
	if !ok || !hmac.Equal([]byte(signature), []byte(s.searchSignature(v.site.WorkspaceID, id))) {
		return
	}
	searchID, err := strconv.ParseInt(id, 36, 64)
	if err != nil || searchID <= 0 {
		return
	}
	if err := s.config.Store.OpenKBSearch(request.Context(), v.site.WorkspaceID, searchID, articleID); err != nil {
		log.Printf("help center: record an article opened from a search of workspace %s: %v", v.site.WorkspaceID, err)
	}
}

// searchSignature signs a search id together with its workspace, so a link opens nothing elsewhere.
func (s *Site) searchSignature(workspaceID, id string) string {
	mac := hmac.New(sha256.New, s.config.SearchKey)
	mac.Write([]byte(workspaceID + "\x00" + id))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:searchSignatureChars]
}
