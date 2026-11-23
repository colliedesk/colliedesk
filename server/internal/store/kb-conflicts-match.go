package store

import (
	"context"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// A reply a team member sent is checked against the published article it is about, so a model is
// asked whether the two disagree only when one article matches closely (docs/internals/knowledge-base.md).

// KBConflictMatchThreshold is the score at which a reply is about an article. A reply that tells
// the returns article's rule with another number scores 0.87, the same rule word for word 1, a
// shipping reply that shares "delivery", "business days" and "payment" with it 0.3.
const KBConflictMatchThreshold = 0.5

// KBConflictMinWords is how many words of 4 or more letters a paragraph of the reply must share
// with the article before its score counts: "Return days?" says too little to disagree with anything.
const KBConflictMinWords = 4

const (
	// kbConflictWordRunes is the shortest word that counts; shorter ones are "to", "of", "is".
	kbConflictWordRunes = 3
	// kbConflictCandidates is how many of the best retrieval matches are scored.
	kbConflictCandidates = 3
)

var kbConflictParagraph = regexp.MustCompile(`\n[ \t]*\n`)

// KBConflictScore is how closely a reply is about an article: for each paragraph of the reply, the
// length of its distinct words found in the article over the length of all of them (words of 3 or
// more letters, folded, a word matching a longer one it starts as in knowledge gaps), and the best
// paragraph's score. A paragraph sharing fewer than KBConflictMinWords long words scores 0, so a
// greeting or a sign-off never decides.
func KBConflictScore(reply, article string) float64 {
	articleTerms := kbGapTerms(kbGapKey(article))
	best := 0.0
	for _, paragraph := range kbConflictParagraph.Split(reply, -1) {
		seen := map[string]bool{}
		matched, total, long := 0, 0, 0
		for _, term := range kbGapTerms(kbGapKey(paragraph)) {
			weight := utf8.RuneCountInString(term)
			if weight < kbConflictWordRunes || seen[term] {
				continue
			}
			seen[term] = true
			total += weight
			for _, candidate := range articleTerms {
				if kbGapTermsMatch(term, candidate) {
					matched += weight
					if weight >= kbGapPrefixRunes {
						long++
					}
					break
				}
			}
		}
		if total == 0 || long < KBConflictMinWords {
			continue
		}
		best = max(best, float64(matched)/float64(total))
	}
	return best
}

// KBConflictArticle is the published article a reply is about, with the part of its text that
// matches the reply.
type KBConflictArticle struct {
	ArticleID string
	Locale    string // "" is the article's own language
	Title     string
	Excerpt   string
	Score     float64
}

// KBConflictArticleFor returns the published article of the workspace that reply is most closely
// about, when one scores KBConflictMatchThreshold or more. It reads what AI retrieval may read
// (every published article not kept from AI, in every language) and scores the best few matches.
// Run it in the workspace's scoped transaction.
func KBConflictArticleFor(ctx context.Context, tx pgx.Tx, workspaceID, reply string) (KBConflictArticle, bool, error) {
	docs, err := AIRetrieve(ctx, tx, workspaceID, AIRetrievalQuery{Text: reply, KBLimit: kbConflictCandidates})
	if err != nil {
		return KBConflictArticle{}, false, fmt.Errorf("match a reply to the knowledge base: %w", err)
	}
	var found KBConflictArticle
	for _, doc := range docs {
		if doc.Kind != AIRetrievedKBArticle {
			continue
		}
		score := KBConflictScore(reply, doc.Title+"\n\n"+doc.Text)
		if score >= KBConflictMatchThreshold && score > found.Score {
			found = KBConflictArticle{ArticleID: doc.KBArticleID, Locale: doc.KBLocale, Title: doc.Title,
				Excerpt: aiExcerpt(doc.Text, kbGapTerms(kbGapKey(reply)), aiKBExcerptRunes), Score: score}
		}
	}
	return found, found.ArticleID != "", nil
}
