package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
	"golang.org/x/net/html"
)

// The old portal's public home page carries the team's Search Console and Bing Webmaster Tools tokens
// in its head. The import copies them into the help center's settings, so the new help center stays
// verified when the DNS switches (docs/internals/freshdesk-import.md, "Search Console tokens").

const (
	// portalHomePath is the old portal's home page, public and the same for every visitor.
	portalHomePath = "/support/home"
	// maxPortalBytes bounds what is read of the page; the head comes first.
	maxPortalBytes = 1 << 20
)

// What became of a token: it was set, the workspace had one, the old head had none, or it has a shape
// the help center would not serve.
const (
	TokenCarried    = "carried"
	TokenAlreadySet = "already_set"
	TokenNotFound   = "not_found"
	TokenInvalid    = "invalid"
)

// SearchConsoleReport is the report's section on the tokens of the old portal. Its status is its own
// (OK, or UNVERIFIED when the page could not be read); tokens are not data, so it never changes the
// import's status. The tokens themselves are not printed.
type SearchConsoleReport struct {
	Status string `json:"status"`
	// Source is the host whose home page was read.
	Source string   `json:"source"`
	Google string   `json:"google"`
	Bing   string   `json:"bing"`
	Detail []string `json:"detail"`
}

// portalTokens reads the verification tokens of the meta tags in the head of a page. A tag in the body,
// a script or a comment does not count. Only the first of each name is read.
func portalTokens(page io.Reader) (google, bing string) {
	tokenizer := html.NewTokenizer(io.LimitReader(page, maxPortalBytes))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return google, bing
		case html.EndTagToken:
			if name, _ := tokenizer.TagName(); string(name) == "head" {
				return google, bing
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			switch string(name) {
			case "body":
				return google, bing
			case "meta":
				var metaName, content string
				for hasAttr {
					var key, value []byte
					key, value, hasAttr = tokenizer.TagAttr()
					switch string(key) {
					case "name":
						metaName = strings.ToLower(strings.TrimSpace(string(value)))
					case "content":
						content = strings.TrimSpace(string(value))
					}
				}
				if metaName == "google-site-verification" && google == "" {
					google = content
				}
				if metaName == "msvalidate.01" && bing == "" {
					bing = content
				}
			}
		}
	}
}

// readPortal opens the old portal's home page without the API key and returns its tokens.
func (c *client) readPortal(ctx context.Context) (google, bing string, err error) {
	target := (&url.URL{Scheme: "https", Host: c.base.Host, Path: portalHomePath}).String()
	// Redirects may stay on the account host only: the page names nobody else's.
	ctx = withRedirectGuard(ctx, func(next *url.URL) bool { return strings.EqualFold(next.Host, c.base.Host) })
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "Colliedesk-Importer/0.2")
	response, err := c.files.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "", "", fmt.Errorf("the old help center's home page could not be reached (%s)", networkReason(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("the old help center's home page answered HTTP %d", response.StatusCode)
	}
	google, bing = portalTokens(response.Body)
	return google, bing, nil
}

// carryOverSearchConsole copies the old portal's tokens into the help center's settings where the
// workspace has none, and builds the report's section. A page that cannot be read is a section with
// the reason, never an error of the import.
func (r *run) carryOverSearchConsole(ctx context.Context) error {
	section := &SearchConsoleReport{Status: StatusOK, Source: r.client.base.Host, Google: TokenNotFound, Bing: TokenNotFound, Detail: []string{}}
	r.searchConsole = section
	google, bing, err := r.client.readPortal(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		section.Status = StatusUnverified
		section.Detail = append(section.Detail, err.Error()+", so its Search Console tokens were not read. Add them in Settings -> Help center -> Search Console.")
		return nil
	}
	// A token the help center would refuse to serve is reported and left out.
	for _, token := range []struct {
		value *string
		state *string
		name  string
	}{{&google, &section.Google, "Google"}, {&bing, &section.Bing, "Bing"}} {
		if *token.value != "" && !domain.ValidSearchConsoleToken(*token.value) {
			*token.value, *token.state = "", TokenInvalid
			section.Detail = append(section.Detail, fmt.Sprintf("The %s verification tag of the old help center is not a token the help center can serve (16 to 100 letters, digits, - or _), so it was left out.", token.name))
		}
	}
	var setGoogle, setBing bool
	err = r.db.InWorkspace(ctx, r.workspaceID, func(tx pgx.Tx) (err error) {
		setGoogle, setBing, err = store.CarryOverSearchConsole(ctx, tx, r.workspaceID, google, bing)
		return err
	})
	if err != nil {
		return fmt.Errorf("carry over search console tokens: %w", err)
	}
	for _, token := range []struct {
		value string
		set   bool
		state *string
	}{{google, setGoogle, &section.Google}, {bing, setBing, &section.Bing}} {
		switch {
		case token.set:
			*token.state = TokenCarried
		case token.value != "":
			*token.state = TokenAlreadySet
		}
	}
	return nil
}

func writeSearchConsole(b *strings.Builder, s *SearchConsoleReport) {
	if s == nil {
		return
	}
	fmt.Fprintf(b, "\n## Search Console: %s\n\n", s.Status)
	fmt.Fprintf(b, "Read from the old help center's home page on %s.\n", s.Source)
	fmt.Fprintf(b, "- Google: %s\n- Bing: %s\n", tokenSentence(s.Google), tokenSentence(s.Bing))
	for _, detail := range s.Detail {
		fmt.Fprintf(b, "- %s\n", detail)
	}
}

func tokenSentence(state string) string {
	switch state {
	case TokenCarried:
		return "carried over to the help center's settings"
	case TokenAlreadySet:
		return "the help center already has one, so it was kept"
	case TokenInvalid:
		return "found, but not a token the help center can serve"
	}
	return "no verification tag in the old help center's head"
}
