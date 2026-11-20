package helpcenter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// votePathSuffix follows an article's address for its votes: a form posts there.
const votePathSuffix = "/vote"

// voteBox is "Was this article helpful?" at the end of an article: plain forms, no script.
type voteBox struct {
	T voteText
	// Action is where the forms post; "" shows the question without forms (a preview).
	Action string
	// Count is the count sentence, "" before anyone voted.
	Count string
}

// voteBoxOf is the box of article in view v.
func voteBoxOf(v view, article domain.PublicKBArticle) *voteBox {
	box := &voteBox{T: voteTextFor(v.locale()), Action: v.root + ArticlePath(article.Number, article.Slug) + votePathSuffix}
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
