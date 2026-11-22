package store_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/database/databasetest"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// New readers from one address who vote on an article at once count up to the address's limit of ten,
// never past it, and a reset counts every vote it closes.
func TestReadersFromOneAddressVotingAtOnceCountUpToTheLimit(t *testing.T) {
	pool := databasetest.Pool(t)
	ctx := context.Background()
	s := store.New(pool)
	workspace := testWorkspace(t, pool, "kb-votes")
	collection, err := s.CreateKBCollection(ctx, workspace, store.Actor{}, store.KBCollectionInput{Name: "Billing", Slug: "billing"})
	if err != nil {
		t.Fatal(err)
	}
	article, err := s.CreateKBArticle(ctx, workspace, store.Actor{}, store.KBArticleInput{
		CollectionID: collection.ID, Title: "Change your plan", Slug: "change-your-plan", BodyMD: "Open Billing.", Status: "published",
	})
	if err != nil {
		t.Fatal(err)
	}
	address := sha256.Sum256([]byte("203.0.113.7"))
	const readers = 15
	errs := make([]error, readers)
	atOnce(readers, func(i int) {
		voter := sha256.Sum256(fmt.Appendf(nil, "reader-%d", i))
		errs[i] = s.VoteKBArticle(ctx, workspace, "", article.Number, voter[:], address[:], true, "")
	})
	for i, err := range errs {
		if err != nil {
			t.Fatalf("reader %d: %v", i, err)
		}
	}
	votes, err := s.KBArticleVotes(ctx, workspace, article.ID)
	if err != nil || votes.Helpful != 10 || votes.NotHelpful != 0 {
		t.Fatalf("votes = %d helpful, %d not, %v; want the 10 the address may add", votes.Helpful, votes.NotHelpful, err)
	}
	reset, err := s.ResetKBArticleVotes(ctx, workspace, store.Actor{}, article.ID)
	if err != nil || reset.Helpful != 0 || len(reset.Resets) != 1 || reset.Resets[0].Helpful != 10 {
		t.Fatalf("reset = %+v %v, want the 10 votes kept as one reset and the count at 0", reset, err)
	}
}
